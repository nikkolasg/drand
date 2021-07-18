package drand

import (
	"context"
	"sync"

	"github.com/drand/drand/key"
	"github.com/drand/drand/log"
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
	c *ProtocolConfig
	// The rest of the fields comes from the original drand struct - the goal is
	// to keep the exact same logic for v1.
	group *key.Group
	index int
	store key.Store
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

}

func loadV1Protocol(c *ProtocolConfig) (Protocol, error) {

}
