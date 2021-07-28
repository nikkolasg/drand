package core

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/drand/drand/http"
	"github.com/drand/drand/net"
	"github.com/drand/drand/protobuf/drand"
)

// ID is ID of a running protocol - an unique identifier for each running
// protocol. Currently it is the group hash of the genesis group.
type ID = string

type Server struct {
	sync.RWMutex
	// list of active protocols mapped by their IDs. Each protocol has its own
	// folder.
	protocols map[ID]Protocol
	// explicit inclusion of the V1 ID to know how to fill the id on incoming
	// messages. We make the assumption that there is only one V1 protocol
	// running.
	v1ID ID
	// running setup protocol. It is nil when there is no setup in progress.
	// Note this is NOT for a resharing phase, where the protocol is already
	// running (regardless if it is for a new node or node).
	setup Protocol
	// all the network componenents. The server maintains them all and dispatch
	// the requests to the requested protocol.
	privGateway *net.PrivateGateway
	pubGateway  *net.PublicGateway
	control     net.ControlListener
}

// We make sure the Server implements all the required methods of what the
// network layer expects. That allows us to create the network client and
// listener even before creating the protocols.
var _ net.Service = (*Server)(nil)

func NewServer(c *Config) Server {
	server := new(Server)
	// instantiate the network components
	if !c.insecure && (c.certPath == "" || c.keyPath == "") {
		return nil, errors.New("config: need to set WithInsecure if no certificate and private key path given")
	}
	// Set the private API address to the command-line flag, if given.
	// Otherwise, set it to the address associated with stored private key.
	privAddr := c.PrivateListenAddress(c.priv.Public.Address())
	pubAddr := c.PublicListenAddress("")
	// ctx is used to create the gateway below.  Gateway constructors
	// (specifically, the generated gateway stubs that require it) do not
	// actually use it, so we are passing a background context to be safe.
	ctx := context.Background()
	var err error
	c.log.Info("network", "init", "insecure", c.insecure)
	if pubAddr != "" {
		handler, err := http.New(ctx, &drandProxy{server}, c.Version(), c.log.With("server", "http"))
		if err != nil {
			return err
		}
		if server.pubGateway, err = net.NewRESTPublicGateway(ctx, pubAddr, c.certPath, c.keyPath, c.certmanager, handler, c.insecure); err != nil {
			return err
		}
	}
	server.privGateway, err = net.NewGRPCPrivateGateway(ctx, privAddr, c.certPath, c.keyPath, c.certmanager, server, c.insecure, d.opts.grpcOpts...)
	if err != nil {
		return err
	}
	p := c.ControlPort()
	server.control = net.NewTCPGrpcControlListener(server, p)
	go control.Start()
	c.log.Info("private_listen", privAddr, "control_port", c.ControlPort(), "public_listen", pubAddr, "folder", d.opts.ConfigFolder())
	privGateway.StartAll()
	if pubGateway != nil {
		pubGateway.StartAll()
	}
	server.protocols = make(map[ID]Protocol)
	return server, nil
}

// LoadProtocols looks for already running protocols and run those if present
// It tries to run all protocols and return a combined error for each protocol
// that failed to run properly.
// NOTE For v1, it only loads the first one it finds and returns errors for the
// other.
func (s *Server) LoadProtocols() error {
	protoConfigs = s.c.SearchprotocolConfig()
	var errs []string
	var v1Found bool
	for _, c := range protoConfigs {
		factory := getProtocolFactory(c.Version)
		protocol, err := factory.Load(c)
		if err != nil {
			errs = append(errs, err.String())
			continue
		}
		s.protocols[protocol.Key()] = protocol
		if c.Version == VERSION_1 {
			if v1Found {
				errrs = append(errs, fmt.Errorf("V1 duplicate protocol found"))
				continue
			}
			// explicit saving of the v1 ID
			s.v1ID = protocol.Key()
			v1Found = true
		}
	}
	// XXX Later we could also save some information for a protocol that was in
	// the setup phase and restore it here
	return nil
}

// Descriptions returns the descriptions of all running protocols and the one in
// setup phase.
func (s *Server) Descriptions() []string {
	var d = make([]string, 0, len(s.protocols)+1)
	for id, p := range s.protocols {
		d = append(d, p.String())
	}
	if s.setup != nil {
		d = append(d, "Setup Phase: "+p.String())
	}
	return d
}

func (s *Server) Stop() {

}

// Example of a function to dispatch to correct protocol. It looks at the group
// hash field (the ID) and then dspatch. If it is not present, we have the
// exception case for V1 where we fill the field manually with the V1 protocol ID.
func (s *Server) PartialBeacon(c context.Context, in *drand.PartialBeaconPacket) (*drand.Empty, error) {
	if len(in.groupHash) == 0 {
		// We are in the v1 case so we fill via the ID we have
		if len(s.v1ID) == 0 {
			return nil, errors.New("No group hash mentionned and no v1 ID registered")
		}
		in.groupHash = []byte(s.v1ID)
	}
	id := string(in.groupHash)
	s.RLock()
	p, ok := s.protocols[id]
	s.RUnLock()
	if !ok {
		return nil, errors.New("No protocols associated with that ID found")
	}
	return p.PartialBeacon(c, in)
}
