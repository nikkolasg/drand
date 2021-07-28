package core

import (
	"fmt"

	"github.com/drand/drand/net"
)

// Protocol is an interface that expects messages related to any running
// protocol on the server. There can be multiple Protocols running on the
// server, each identified by their group hash.
// PB: how to identify a protocol when the setup phase is not finished yet ?
// indeed there is no group hash to use as key.
// Sol 1: identify by version: unfortunately that means now we need to issue a
// new version for a new different frequency only ! -> No.
// Sol 2: Have a "Setup Protocol" interface that process setup related messages.
// The Server will only keep one Setup interface "alive" at most at any time (if
// there is no setup in progress there is no setup protocol running). Even
// though it allows us to decouple the setup of the group from the protocol
// that the group runs, either we enforce an unique "output" from all setup
// possible that we give to a Protocol, or we make the setup protocol creates
// the Protocol itself. The former case is undesirable because different setups
// may require different informations and that also may create incompatibilities
// between setup and protocol.
// Sol 3: We choose to have one "unidentified" protocol running in setup mode at
// most at a time and this protocol decides and runs the setup on its own. Once
// the setup is finished the protocol starts at its convenience the randomness
// mode without having to report to the server.
type Protocol interface {
	// Each protocol must be able to answers all networks queries described in
	// the protobuf files. A protocol can ignore certain irrelevant requests for
	// it. One can use "DefaultService" to automatically implement the Service
	// with error returning requests, allowing one to only implement the
	// relevant methods.
	net.Service
	// Key returns the unique key to distinct this protocol. It is required to
	// use the group hash as key as this is the key present in network messages
	// used to dispatch to the right protocol. Once a protocol is instantiated
	// via the factory, it is registered using the output of this method.
	Key() ID
	// Terminate kills the protocol and deletes every information related to it:
	// shares, keys, database.
	Terminate() error
	// Stringer must returns a succinct but complete description of the protocol
	// to be shown to the CLI.
	fmt.Stringer
}

// ProtocolFactory can instantiate a new fresh protocol or load one from the
// config. Loading means the protocol is already running in the network and this
// node has been restarted for example so it needs to laod the parameters and
// re-join the network.
type ProtocolFactory struct {
	// New instantiates a fresh protocol. New must start all go-routines already
	// needed by the protocol to function properly. After New is called, network
	// messages can be dispatched to the protocol..
	New func(*ProtocolConfig) (Protocol, error)
	// Loads all parameters and re-start the protocol from the last point. Load
	// must run all goroutines to function properly. After Load is called,
	// network mesages can be dispatched to the protocol.
	Load func(*ProtocolConfig) (Protocol, error)
}

// Version is an alias to represent the version of a protocol. Protocols are
// registered by versions.
type Version = string

var protocols = make(map[Version]ProtocolFactory)

// blacklist is a hardcoded list of protocol versions drand does not support
// anymore
var blacklist = []Version{}

// registerProtocol maps the version to a protocol factory which is used then to
// create or load the protocol. Note that it is possible to run multiple
// networks of the same version. The unique key to distinct them is the group
// hash.
func registerProtocol(v Version, f *ProtocolFactory) {
	protocols[v] = f
}

// getProtocolFactory returns the corresponding factory given the version is not
// blacklisted.
func getProtocolFactory(v Version) *ProtocolFactory {
	for _, b := range blacklist {
		if v == b {
			panic("blacklisted protocol")
		}
	}
	if f, ok := protocols[v]; !ok {
		panic("no registered protocols for this version")
	} else {
		return f
	}
}
