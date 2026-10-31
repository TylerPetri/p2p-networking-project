package p2p

import (
	"context"
	"p2p-park/internal/dht"
	"p2p-park/internal/wire"
	"time"
)

// dhtAccessor returns the node's DHT engine.
// It is intentionally unexported; tests in package p2p may use it.
func (n *Node) dhtAccessor() *dht.DHT {
	return n.dht
}

func (n *Node) PutImmutable(ctx context.Context, value []byte, ttl time.Duration) ([32]byte, error) {
	sender, cancel := n.dhtSender(ctx)
	defer cancel()
	return n.dht.PutImmutable(ctx, sender, value, ttl)
}
func (n *Node) PutMutable(ctx context.Context, name string, value []byte, seq uint64, ttl time.Duration) ([32]byte, error) {
	sender, cancel := n.dhtSender(ctx)
	defer cancel()
	return n.dht.PutMutable(ctx, sender, n.id.SignPriv, name, value, seq, ttl)
}
func (n *Node) GetValue(ctx context.Context, key [32]byte) (*wire.DHTRecord, bool, error) {
	sender, cancel := n.dhtSender(ctx)
	defer cancel()
	return n.dht.GetValue(ctx, sender, key)
}

// The public engine's per-operation context participates in the existing RPC
// sender cancellation path, without changing legacy background lookups.
type contextualDHTSender struct {
	*Node
	ctx context.Context
}

func (s contextualDHTSender) Done() <-chan struct{} { return s.ctx.Done() }
func (s contextualDHTSender) SendToPeer(id string, env wire.Envelope) error {
	if err := s.ctx.Err(); err != nil {
		return err
	}
	return s.Node.SendToPeer(id, env)
}
func (n *Node) dhtSender(ctx context.Context) (contextualDHTSender, func()) {
	combined, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(n.ctx, cancel)
	return contextualDHTSender{n, combined}, func() { stop(); cancel() }
}
