package bootstrap

import (
	"context"
	"github.com/TylerPetri/p2p-networking-project/internal/netx"
)

type PeerSource interface {
	// Discover returns candidate peers to connect to.
	Discover(ctx context.Context) ([]netx.Addr, error)
	Name() string
}
