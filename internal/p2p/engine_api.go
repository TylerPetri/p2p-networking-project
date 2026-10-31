package p2p

import (
	"context"
	"errors"

	"p2p-park/internal/netx"
	proto "p2p-park/internal/wire"
)

var ErrPeerClosed = errors.New("p2p: peer not connected or rejected")

// ConnectContext waits for Noise, identity, domain and admission checks. Unlike
// the legacy ConnectTo this is not merely acknowledgement of a TCP dial.
func (n *Node) ConnectContext(ctx context.Context, addr netx.Addr) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	n.mu.RLock()
	for _, p := range n.peers {
		if p.addr == addr && (n.cfg.PeerPins[addr] == "" || p.id == n.cfg.PeerPins[addr]) {
			n.mu.RUnlock()
			return nil
		}
	}
	n.mu.RUnlock()
	if !n.reserveConnection() {
		if err := n.ctx.Err(); err != nil {
			return err
		}
		return ErrResourceLimit
	}
	dialCtx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(n.ctx, cancel)
	defer func() { stop(); cancel() }()
	d, ok := n.cfg.Network.(interface {
		DialContext(context.Context, netx.Addr) (netx.Conn, error)
	})
	if !ok {
		n.releaseConnection()
		return errors.New("p2p: contextual dialing unsupported")
	}
	conn, err := d.DialContext(dialCtx, addr)
	if err != nil {
		n.releaseConnection()
		return err
	}
	ready := make(chan error, 1)
	abort := context.AfterFunc(dialCtx, func() { _ = conn.Close() })
	defer abort()
	go func() { defer n.releaseConnection(); n.runConn(conn, false, n.cfg.PeerPins[addr], ready) }()
	select {
	case <-dialCtx.Done():
		return dialCtx.Err()
	case err := <-ready:
		return err
	}
}
func (n *Node) DisconnectPeer(id string) error {
	n.mu.RLock()
	p := n.peers[id]
	n.mu.RUnlock()
	if p == nil {
		return ErrPeerClosed
	}
	n.removePeer(id, p)
	return nil
}

func (n *Node) PublishGossip(g proto.Gossip) error {
	if g.ID == "" || len(g.ID) > 128 || len(g.Channel) > 128 {
		return ErrResourceLimit
	}
	env, err := n.signGossip(proto.Envelope{Type: proto.MsgGossip, FromID: n.ID(), Payload: proto.MustMarshal(g)})
	if err != nil {
		return err
	}
	if !validEnvelopeSize(env) {
		return ErrResourceLimit
	}
	if n.seen.Seen(n.ID() + "/" + g.ID) {
		return ErrResourceLimit
	}
	n.mu.RLock()
	peers := make([]*peer, 0, len(n.peers))
	for _, p := range n.peers {
		peers = append(peers, p)
	}
	n.mu.RUnlock()
	var first error
	for _, p := range peers {
		if err := n.enqueue(p, env, SendDisconnect); err != nil && first == nil {
			first = err
		}
	}
	return first
}
func (n *Node) SendGossip(id string, g proto.Gossip) error {
	if !validPeerID(id) || g.ID == "" || len(g.ID) > 128 || len(g.Channel) > 128 {
		return ErrResourceLimit
	}
	return n.SendToPeer(id, proto.Envelope{Type: proto.MsgGossip, FromID: n.ID(), Recipient: id, Payload: proto.MustMarshal(g)})
}
