package p2p

import (
	"encoding/json"
	"p2p-park/internal/netx"
	proto "p2p-park/internal/wire"
	"time"
)

func (n *Node) handleEnvelope(p *peer, env proto.Envelope) {
	if env.Type != proto.MsgGossip && env.Type != proto.MsgNatRelay {
		env.FromID = p.id
	}
	if !n.allowPeerMessage(p) {
		_ = p.conn.Close()
		return
	}
	switch env.Type {
	case proto.MsgPeerList:
		var pl proto.PeerList
		if err := json.Unmarshal(env.Payload, &pl); err != nil {
			n.Logf("bad peer list from %s: %s", p.id, err)
			return
		}
		if len(pl.Peers) > maxDiscoveryBatch {
			pl.Peers = pl.Peers[:maxDiscoveryBatch]
		}
		for _, pi := range pl.Peers {
			if pi.ID == n.id.ID {
				continue
			}
			if n.hasPeer(pi.ID) {
				continue
			}
			n.Logf("discovery: dialing peer %s at %s", pi.ID, pi.Addr)
			n.queueReferral(netx.Addr(pi.Addr), pi.ID)
		}
	case proto.MsgGossip:
		if env.NetworkID != n.cfg.NetworkID || !n.admitted(env.FromID) || !verifyGossip(env, time.Now()) {
			return
		}
		if env.Recipient != "" && env.Recipient != n.ID() {
			return
		}
		var g proto.Gossip
		if err := json.Unmarshal(env.Payload, &g); err != nil {
			return
		}
		if g.ID == "" || len(g.ID) > 128 || len(g.Channel) > 128 {
			return
		}
		if n.seen.Seen(env.FromID + "/" + g.ID) {
			return
		}

		select {
		case n.incoming <- env:
		default:
		}
		if env.Recipient == "" {
			n.relay(p.id, env)
		}
	case proto.MsgIdentify:
		n.handleIdentify(p, env)
	case proto.MsgNatRegister:
		n.handleNatRegister(p, env)
	case proto.MsgNatRelay:
		if n.cfg.IsSeed {
			n.handleNatRelaySeed(p, env)
		} else {
			n.handleNatRelayClient(p, env)
		}
	case proto.MsgDHT:
		if n.dht != nil {
			n.mu.RLock()
			name := p.name
			n.mu.RUnlock()
			n.dht.HandleDHT(n, p.id, string(p.addr), name, env)
		}
		return

	default:
		select {
		case n.incoming <- env:
		default:
		}
	}
}
