package core

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/drand/drand/chain"
	"github.com/drand/drand/chain/beacon"
	"github.com/drand/drand/net"
	"github.com/drand/drand/protobuf/drand"
)

// BroadcastDKG is the public method to call during a DKG protocol.
func (d *v1Protocol) BroadcastDKG(c context.Context, in *drand.DKGPacket) (*drand.Empty, error) {
	d.state.Lock()
	defer d.state.Unlock()
	if d.dkgInfo == nil {
		return nil, errors.New("drand: no dkg running")
	}
	addr := net.RemoteAddress(c)
	if !d.dkgInfo.started {
		d.log.Info("init_dkg", "START DKG", "signal from leader", addr, "group", hex.EncodeToString(d.dkgInfo.target.Hash()))
		d.dkgInfo.started = true
		go d.dkgInfo.phaser.Start()
	}
	if _, err := d.dkgInfo.board.BroadcastDKG(c, in); err != nil {
		return nil, err
	}
	return new(drand.Empty), nil
}

// PartialBeacon receives a beacon generation request and answers
// with the partial signature from this drand node.
func (d *v1Protocol) PartialBeacon(c context.Context, in *drand.PartialBeaconPacket) (*drand.Empty, error) {
	d.state.Lock()
	if d.beacon == nil {
		d.state.Unlock()
		return nil, errors.New("drand: beacon not setup yet")
	}
	inst := d.beacon
	d.state.Unlock()
	return inst.ProcessPartialBeacon(c, in)
}

// PublicRand returns a public random beacon according to the request. If the Round
// field is 0, then it returns the last one generated.
func (d *v1Protocol) PublicRand(c context.Context, in *drand.PublicRandRequest) (*drand.PublicRandResponse, error) {
	var addr = net.RemoteAddress(c)
	d.state.Lock()
	defer d.state.Unlock()
	if d.beacon == nil {
		return nil, errors.New("drand: beacon generation not started yet")
	}
	var r *chain.Beacon
	var err error
	if in.GetRound() == 0 {
		r, err = d.beacon.Store().Last()
	} else {
		// fetch the correct entry or the next one if not found
		r, err = d.beacon.Store().Get(in.GetRound())
	}
	if err != nil || r == nil {
		d.log.Debug("public_rand", "unstored_beacon", "round", in.GetRound(), "from", addr)
		return nil, fmt.Errorf("can't retrieve beacon: %w %s", err, r)
	}
	d.log.Info("public_rand", addr, "round", r.Round, "reply", r.String())
	return beaconToProto(r), nil
}

// PublicRandStream exports a stream of new beacons as they are generated over gRPC
func (d *v1Protocol) PublicRandStream(req *drand.PublicRandRequest, stream drand.Public_PublicRandStreamServer) error {
	var b *beacon.Handler
	d.state.Lock()
	if d.beacon == nil {
		d.state.Unlock()
		return errors.New("beacon has not started on this node yet")
	}
	b = d.beacon
	d.state.Unlock()
	lastb, err := b.Store().Last()
	if err != nil {
		return err
	}
	addr := net.RemoteAddress(stream.Context())
	done := make(chan error, 1)
	d.log.Debug("request", "stream", "from", addr, "round", req.GetRound())
	if req.GetRound() != 0 && req.GetRound() <= lastb.Round {
		// we need to stream from store first
		var err error
		b.Store().Cursor(func(c chain.Cursor) {
			for bb := c.Seek(req.GetRound()); bb != nil; bb = c.Next() {
				if err = stream.Send(beaconToProto(bb)); err != nil {
					d.log.Debug("stream", err)
					return
				}
			}
		})
		if err != nil {
			return err
		}
	}
	// then we can stream from any new rounds
	// register a callback for the duration of this stream
	d.beacon.AddCallback(addr, func(b *chain.Beacon) {
		err := stream.Send(&drand.PublicRandResponse{
			Round:             b.Round,
			Signature:         b.Signature,
			PreviousSignature: b.PreviousSig,
			Randomness:        b.Randomness(),
		})
		// if connection has a problem, we drop the callback
		if err != nil {
			d.beacon.RemoveCallback(addr)
			done <- err
		}
	})
	return <-done
}

// Home provides the address the local node is listening
func (d *v1Protocol) Home(c context.Context, in *drand.HomeRequest) (*drand.HomeResponse, error) {
	d.log.With("module", "public").Info("home", net.RemoteAddress(c))
	return &drand.HomeResponse{
		Status: fmt.Sprintf("drand up and running on %s",
			d.priv.Public.Address()),
	}, nil
}

// ChainInfo replies with the chain information this node participates to
func (d *v1Protocol) ChainInfo(ctx context.Context, in *drand.ChainInfoRequest) (*drand.ChainInfoPacket, error) {
	d.state.Lock()
	defer d.state.Unlock()
	if d.group == nil {
		return nil, errors.New("drand: no dkg group setup yet")
	}
	return chain.NewChainInfo(d.group).ToProto(), nil
}

// SignalDKGParticipant receives a dkg signal packet from another member
func (d *v1Protocol) SignalDKGParticipant(ctx context.Context, p *drand.SignalDKGPacket) (*drand.Empty, error) {
	d.state.Lock()
	defer d.state.Unlock()
	if d.manager == nil {
		return nil, errors.New("no manager")
	}
	addr := net.RemoteAddress(ctx)
	// manager will verify if information are correct
	err := d.manager.ReceivedKey(addr, p)
	if err != nil {
		return nil, err
	}
	return new(drand.Empty), nil
}

// PushDKGInfo triggers sending DKG info to other members
func (d *v1Protocol) PushDKGInfo(ctx context.Context, in *drand.DKGInfoPacket) (*drand.Empty, error) {
	d.state.Lock()
	defer d.state.Unlock()
	if d.receiver == nil {
		return nil, errors.New("no receiver setup")
	}
	d.log.Info("push_group", "received_new")
	// the control routine will receive this info and start the dkg at the right
	// time - if that is the right secret.
	return new(drand.Empty), d.receiver.PushDKGInfo(in)
}

// SyncChain is a inter-node protocol that replies to a syncing request from a
// given round
func (d *v1Protocol) SyncChain(req *drand.SyncRequest, stream drand.Protocol_SyncChainServer) error {
	d.state.Lock()
	b := d.beacon
	d.state.Unlock()
	if b != nil {
		return b.SyncChain(req, stream)
	}
	return nil
}

// GetIdentity returns the identity of this drand node
func (d *v1Protocol) GetIdentity(ctx context.Context, req *drand.IdentityRequest) (*drand.Identity, error) {
	return d.priv.Public.ToProto(), nil
}
