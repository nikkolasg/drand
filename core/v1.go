package core

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/drand/drand/key"
	"github.com/drand/drand/log"
	"github.com/drand/drand/net"
)

// VERSION_1 is automatically inserted on incoming beacon related messages who
// do NOT specify any ID. Indeed, v1 did not support multiple protocols so in
// order to be backward compatible we are artificially injecting this ID on
// incoming AND outgoing message.
// BLS12381 - SHA256(r||prev_sig)
const VERSION_1 = "V1"

const factoryv1 = ProtocolFactory{
	New:  newV1Protocol,
	Load: loadV1Protocol,
}

func init() {
	registerProtocol(VERSION_1, factoryv1)
}

type v1Protocol struct {
	*net.DefaultService
	c     *ProtocolConfig
	store key.Store
	// The rest of the fields comes from the original drand struct - the goal is
	// to keep the exact same logic for v1.
	group *key.Group
	index int
	// dkg private share. can be nil if dkg not finished yet.
	share   *key.Share
	dkgDone bool
	// manager is created and destroyed during a setup phase
	manager  *setupManager
	receiver *setupReceiver

	// dkgInfo contains all the information related to an upcoming or in
	// progress dkg protocol. It is nil for the rest of the time.
	dkgInfo *dkgInfo
	// general logger
	log log.Logger

	// global state lock
	state  sync.Mutex
	exitCh chan bool

	// that cancel function is set when the drand process is following a chain
	// but not participating. Drand calls the cancel func when the node
	// participates to a resharing.
	syncerCancel context.CancelFunc

	// only used for testing currently
	// XXX need boundaries between gRPC and control plane such that we can give
	// a list of paramteres at each DKG (inluding this callback)
	setupCB func(*key.Group)
}

func newV1Protocol(c *ProtocolConfig) (Protocol, error) {
	return initV1(c)
}

func loadV1Protocol(c *ProtocolConfig) (Protocol, error) {
	v1, err := initV1(c)
	if err != nil {
		return nil, fmt.Errorf("Err loading V1: ", err)
	}
	v1.group, err = v1.store.LoadGroup()
	if err != nil {
		return nil, err
	}
	checkGroup(v1.log, v1.group)
	// Compared to previous version,we avoid loading the share in memory where
	// we don't need to
	v1.log.Debug("serving", v1.priv.Public.Address())
	v1.dkgDone = true
	return v1, nil
}

// initV1 loads the store and looks up if the private pblic key pair is valid.
// it returns a V1 protocol with these informations loaded. This function is
// only to be used within newV1Protocol or loadV1Protocol.
func initV1(c *ProtocolConfig) (Protocol, error) {
	store := key.NewFileStore(c.BaseFolder)
	priv, err := store.LoadKeyPair()
	if err != nil {
		return nil, err
	}
	if err := priv.Public.ValidSignature(); err != nil {
		logger.Error("INVALID SELF SIGNATURE", err, "action", "run `drand util self-sign`")
	}
	v1 := new(v1Protocol)
	v1.c = c
	v1.store = store
	v1.log = c.Log
	return v1
}

// WaitDKG waits on the running dkg protocol. In case of an error, it returns
// it. In case of a finished DKG protocol, it saves the dist. public  key and
// private share. These should be loadable by the store.
func (v1 *v1Protocol) WaitDKG() (*key.Group, error) {
	v1.state.Lock()
	if v1.dkgInfo == nil {
		v1.state.Unlock()
		return nil, errors.New("no dkg info set")
	}
	waitCh := v1.dkgInfo.proto.WaitEnd()
	v1.state.Unlock()

	v1.log.Debug("waiting_dkg_end", time.Now())
	res := <-waitCh
	if res.Error != nil {
		return nil, fmt.Errorf("drand: error from dkg: %v", res.Error)
	}

	v1.state.Lock()
	defer v1.state.Unlock()
	// filter the nodes that are not present in the target group
	var qualNodes []*key.Node
	for _, node := range v1.dkgInfo.target.Nodes {
		for _, qualNode := range res.Result.QUAL {
			if qualNode.Index == node.Index {
				qualNodes = append(qualNodes, node)
			}
		}
	}

	s := key.Share(*res.Result.Key)
	v1.share = &s
	if err := v1.store.SaveShare(v1.share); err != nil {
		return nil, err
	}
	targetGroup := v1.dkgInfo.target
	// only keep the qualified ones
	targetGroup.Nodes = qualNodes
	// setup the dist. public key
	targetGroup.PublicKey = v1.share.Public()
	v1.group = targetGroup
	var output []string
	for _, node := range qualNodes {
		output = append(output, fmt.Sprintf("{addr: %s, idx: %d, pub: %s}", node.Address(), node.Index, node.Key))
	}
	v1.log.Debug("dkg_end", time.Now(), "certified", v1.group.Len(), "list", "["+strings.Join(output, ",")+"]")
	if err := v1.store.SaveGroup(v1.group); err != nil {
		return nil, err
	}
	v1.opts.applyDkgCallback(d.share)
	v1.dkgInfo.board.Stop()
	v1.dkgInfo = nil
	return v1.group, nil
}

// StartBeacon initializes the beacon if needed and launch a go
// routine that runs the generation loop.
func (v1 *v1Protocol) StartBeacon(catchup bool) {
	b, err := v1.newBeacon()
	if err != nil {
		v1.log.Error("init_beacon", err)
		return
	}

	v1.log.Info("beacon_start", time.Now(), "catchup", catchup)
	if catchup {
		go b.Catchup()
	} else if err := b.Start(); err != nil {
		v1.log.Error("beacon_start", err)
	}
}

// transition between an "old" group and a new group. This method is called
// *after* a resharing dkg has proceed.
// the new beacon syncs before the new network starts
// and will start once the new network time kicks in. The old beacon will stop
// just before the time of the new network.
// TODO: due to current WaitDKG behavior, the old group is overwritten, so an
// old node that fails during the time the resharing is done and the new network
// comes up have to wait for the new network to comes in - that is to be fixed
func (v1 *v1Protocol) transition(oldGroup *key.Group, oldPresent, newPresent bool) {
	// the node should stop a bit before the new round to avoid starting it at
	// the same time as the new node
	// NOTE: this limits the round time of drand - for now it is not a use
	// case to go that fast
	timeToStop := v1.group.TransitionTime - 1
	if !newPresent {
		// an old node is leaving the network
		if err := v1.beacon.StopAt(timeToStop); err != nil {
			v1.log.Error("leaving_group", err)
		} else {
			v1.log.Info("leaving_group", "done", "time", v1.c.clock.Now())
		}
		return
	}

	v1.state.Lock()
	newGroup := v1.group
	newShare := v1.share
	v1.state.Unlock()

	// tell the current beacon to stop just before the new network starts
	if oldPresent {
		v1.beacon.TransitionNewGroup(newShare, newGroup)
	} else {
		b, err := d.newBeacon()
		if err != nil {
			v1.log.Fatal("transition", "new_node", "err", err)
		}
		if err := b.Transition(oldGroup); err != nil {
			v1.log.Error("sync_before", err)
		}
		v1.log.Info("transition_new", "done")
	}
}

// StopBeacon stops the beacon generation process and resets it.
func (v1 *v1Protocol) StopBeacon() {
	v1.state.Lock()
	defer v1.state.Unlock()
	if v1.beacon == nil {
		return
	}
	v1.beacon.Stop()
	v1.beacon = nil
}
