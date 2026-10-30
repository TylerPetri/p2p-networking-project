package parknode

import "p2p-park/internal/netx"

type Config struct {
	PeerPins       map[netx.Addr]string
	DiscoveryCIDRs []string
	DisableLAN     bool
	DataDir        string
	Name           string
	Bind           string
	IsSeed         bool
	Bootstraps     []netx.Addr
	Debug          bool
}
