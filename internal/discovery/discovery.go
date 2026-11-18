package discovery

import "github.com/TylerPetri/p2p-networking-project/internal/p2p"

type Strategy interface {
	Discover(n *p2p.Node) []string // returns list of addresses to try
}
