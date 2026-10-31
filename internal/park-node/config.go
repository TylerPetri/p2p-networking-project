package parknode

import "p2p-park/engine"

type Config struct {
	NetworkID     string
	Identity      *engine.Identity
	Admission     engine.Admission
	ApprovedPeers map[string]string

	PeerPins          map[string]string
	DiscoveryCIDRs    []string
	DisableLAN        bool
	DataDir           string
	DiscoveryStateDir string
	Name              string
	Bind              string
	IsSeed            bool
	Bootstraps        []string
	Debug             bool
}
