package p2p

import (
	"fmt"
	"p2p-park/internal/proto"
)

type SendPolicy int

const (
	SendDrop       SendPolicy = iota // drop this message if buffer full
	SendDisconnect                   // drop peer if buffer full
)

func (n *Node) sendAsyncWithPolicy(p *peer, env proto.Envelope, policy SendPolicy) {
	if env.Type == proto.MsgGossip && len(env.Signature) == 0 {
		var err error
		env, err = n.signGossip(env)
		if err != nil {
			return
		}
	}
	if !validEnvelopeSize(env) {
		return
	}
	select {
	case <-p.ctx.Done():
		// peer is closing; just drop
		return
	default:
	}

	select {
	case p.sendCh <- env:
		return
	default:
		if policy == SendDisconnect {
			// No goroutine per dropped message: a slow peer cannot amplify workers.
			n.removePeer(p.id, p)
			n.Logf("dropping peer %s: send buffer full", p.id)
		}
		// else: drop the message
	}
}

func (n *Node) sendAsync(p *peer, env proto.Envelope) {
	n.sendAsyncWithPolicy(p, env, SendDisconnect)
}

func (n *Node) sendPeerList(p *peer) error {
	pl := proto.PeerList{Peers: n.snapshotPeersInfo()}
	env := proto.Envelope{
		Type:    proto.MsgPeerList,
		FromID:  n.id.ID,
		Payload: proto.MustMarshal(pl),
	}
	n.sendAsync(p, env)
	return nil
}

// SendToPeer sends an envelope to a peer by network ID.
// It returns an error if the peer is not known.
func (n *Node) SendToPeer(id string, env proto.Envelope) error {
	n.mu.RLock()
	p, ok := n.peers[id]
	n.mu.RUnlock()
	if !ok {
		return fmt.Errorf("unknown peer %q", id)
	}

	if env.Type == proto.MsgGossip && len(env.Signature) == 0 {
		var err error
		env, err = n.signGossip(env)
		if err != nil {
			return err
		}
	}
	if !validEnvelopeSize(env) {
		return ErrResourceLimit
	}
	n.sendAsync(p, env)
	return nil
}
