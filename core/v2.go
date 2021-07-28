package core

// BLS12381 - SHA256(r)
const VERSION_2 = "V2"

const factoryv2 = ProtocolFactory{
	New:  newV2Protocol,
	Load: loadV2Protocol,
}

func init() {
	registerProtocol(VERSION_2, V2Factory)
}

type V2Protocol struct {
	c *ProtocolConfig
}

func newV2Protocol(c *ProtocolConfig) (Protocol, error) {

}

func loadV2Protocol(c *ProtocolConfig) (Protocol, error) {

}
