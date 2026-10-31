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
	return n.connectPinnedContext(ctx, addr, n.cfg.PeerPins[addr], "")
}

// ConnectPinnedContext applies per-attempt pins without changing node trust or
// discovery configuration. Signing pins are checked before peer admission.
func (n *Node) ConnectPinnedContext(ctx context.Context, addr netx.Addr, peerID, signingID string) error {
	if !validPeerID(peerID) || !validPeerID(signingID) {
		return errors.New("invalid bootstrap identity")
	}
	if pin := n.cfg.PeerPins[addr]; pin != "" && pin != peerID {
		return errors.New("bootstrap conflicts with configured peer pin")
	}
	return n.connectPinnedContext(ctx, addr, peerID, signingID)
}
func (n *Node) matchesPinnedPeer(addr netx.Addr, peerID, signingID string) bool {
	n.mu.RLock()
	defer n.mu.RUnlock()
	for _, p := range n.peers {
		if p.addr == addr && (peerID == "" || p.id == peerID) && (signingID == "" || p.userID == signingID) {
			return true
		}
	}
	return false
}
func (n *Node) connectPinnedContext(ctx context.Context, addr netx.Addr, peerID, signingID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if n.matchesPinnedPeer(addr, peerID, signingID) {
		return nil
	}
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
	go func() { defer n.releaseConnection(); n.runPinnedConn(conn, false, peerID, signingID, ready) }()
	select {
	case <-dialCtx.Done():
	case err = <-ready:
	}
	// Cancellation may make setup report EOF before this waiter runs. Normalize
	// that outcome and close explicitly: stopping AfterFunc must not suppress cleanup.
	if contextErr := dialCtx.Err(); contextErr != nil {
		_ = conn.Close()
		return contextErr
	}
	if errors.Is(err, ErrPeerClosed) && n.matchesPinnedPeer(addr, peerID, signingID) {
		return nil
	}
	return err
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
