package parknode

import "github.com/TylerPetri/p2p-networking-project/engine"

type Config struct {
	NetworkID     string
	Identity      *engine.Identity
	Admission     engine.Admission
	ApprovedPeers map[string]string

	PeerPins       map[string]string
	DiscoveryCIDRs []string
	DisableLAN     bool

	DiscoveryStateDir string
	Name              string
	Bind              string
	IsSeed            bool
	Bootstraps        []string
	Debug             bool
}
