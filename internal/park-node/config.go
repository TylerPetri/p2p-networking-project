package parknode

import "p2p-park/internal/netx"

type Config struct {
	DisableLAN bool
	DataDir    string
	Name       string
	Bind       string
	IsSeed     bool
	Bootstraps []netx.Addr
	Debug      bool
}
