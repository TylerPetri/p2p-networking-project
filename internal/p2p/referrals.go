package p2p

import (
	"encoding/hex"
	"errors"
	"net"
	"net/netip"
	"strconv"

	"p2p-park/internal/netx"
)

type referral struct {
	addr netx.Addr
	id   string
}

func validPeerID(id string) bool {
	b, err := hex.DecodeString(id)
	return err == nil && len(b) == 32 && hex.EncodeToString(b) == id
}
func numericEndpoint(addr netx.Addr) (netip.Addr, bool) {
	host, port, err := net.SplitHostPort(string(addr))
	if err != nil {
		return netip.Addr{}, false
	}
	p, err := strconv.Atoi(port)
	if err != nil || p < 1 || p > 65535 {
		return netip.Addr{}, false
	}
	ip, err := netip.ParseAddr(host)
	if err != nil || ip.Zone() != "" {
		return netip.Addr{}, false
	}
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() && !ip.IsLoopback() {
		return netip.Addr{}, false
	}
	if ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
		return netip.Addr{}, false
	}
	return ip, true
}

// ConnectDiscovered admits untrusted hints only in explicitly configured numeric
// ranges, on a connected peer's own host, or loopback when we bind loopback.
// DNS is reserved for explicit ConnectTo/bootstrap calls to avoid rebinding.
func (n *Node) ConnectDiscovered(addr netx.Addr, expected string) error {
	ip, ok := numericEndpoint(addr)
	if !ok {
		return errors.New("invalid discovery endpoint")
	}
	if expected != "" && !validPeerID(expected) {
		return errors.New("invalid discovery peer ID")
	}
	if pin := n.cfg.PeerPins[addr]; pin != "" {
		if expected != "" && expected != pin {
			return errors.New("referral conflicts with pin")
		}
		expected = pin
	}
	allowed := false
	for _, p := range n.discoveryPrefixes {
		if p.Contains(ip) {
			allowed = true
		}
	}
	host, _, _ := net.SplitHostPort(n.cfg.BindAddr)
	if bind, err := netip.ParseAddr(host); err == nil && bind.IsLoopback() && ip.IsLoopback() {
		allowed = true
	}
	n.mu.RLock()
	for _, p := range n.peers {
		remote, ok := numericEndpoint(p.observedAddr)
		if ok && remote == ip {
			allowed = true
		}
	}
	n.mu.RUnlock()
	if !allowed {
		return errors.New("discovery destination outside approved scope")
	}
	return n.connectTo(addr, expected)
}

func (n *Node) queueReferral(addr netx.Addr, id string) {
	if len(addr) > 128 {
		return
	}
	if id == n.ID() || n.hasPeer(id) || !validPeerID(id) {
		return
	}
	select {
	case n.referrals <- referral{addr, id}:
	default:
	}
}
func (n *Node) referralLoop() {
	for {
		select {
		case <-n.ctx.Done():
			return
		case r := <-n.referrals:
			_ = n.ConnectDiscovered(r.addr, r.id)
		}
	}
}
