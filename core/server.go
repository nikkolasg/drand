package drand

import (
	"context"
	"errors"
	"path"
	"sync"

	"github.com/drand/drand/log"
	"github.com/drand/drand/net"
	"github.com/drand/drand/protobuf/drand"
)

type ID = string

type Server struct {
	sync.RWMutex
	// list of active protocols
	protocols map[ID]Protocol
	// explicit inclusion of the V1 ID to know how to fill the id on incoming
	// messages. We make the assumption that there is only one V1 protocol
	// running.
	v1ID ID
	// running setup protocol. It is nil when there is no setup in progress.
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
	// instantiate the network components
	return &Server{}
}

// LoadProtocols looks for already running protocols and run those if present
// It tries to run all protocols and return a combined error for each protocol
// that failed to run properly.
func (s *Server) LoadProtocols() error {
	protoConfigs = s.c.SearchprotocolConfig()
	var errs []string
	for _, c := range protoConfigs {
		factory := getProtocolFactory(c.Version)
		protocol, err := factory.Load(c)
		if err != nil {
			errs = append(errs, err.String())
			continue
		}
		s.protocols[protocol.Key()] = protocol
		if c.Version == VERSION_1 {
			// explicit saving of the v1 ID
			s.v1ID = protocol.Key()
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

// SearchProtocolConfig looks in the host folders for folders of running
// protocols and return them if it finds any. These returned config are needed
// to instantiate and run the different protocols this server manages.
// The way folders are organized is by group hash:
// baseFolder/<group_hash>/{VERSION,...}
// The protocol can write anything inside his folder but it is required to write
// a VERSION file containing the version string of the protocol (required to
// load the protocol)..
// Note that for the v1 protocol, since it did not use this configuration, this
// function looks for
// 		baseFolder/{db/,groups/,key/}
// If it finds those folders, it instantiates the protocol using those, and
// under the V1ID so it is backward compatible..
func (c *Config) SearchProtocolConfig() []*ProtocolConfig {
	// TODO
	return nil
}

// ProtocolConfig is the configuration used by one instance of a protocol - its
// information are contained to this protocol and isolated from others in a
// different folder.
type ProtocolConfig struct {
	// The version of the protocol
	Version Version
	// The base folder where the protocol can write anything
	Folder string
	// The client that allows this protocol to speak to other nodes
	Client net.ProtocolClient
	// The logger to use for this protocol. The protocol is expected to
	// customize the logger.
	Log log.Logger
}

// DBFolder returns the folder which can be used by the database engine of the
// protocol
func (p *ProtocolConfig) DBFolder() string {
	return path.Join(p.Folder, DefaultDBFolder)
}
