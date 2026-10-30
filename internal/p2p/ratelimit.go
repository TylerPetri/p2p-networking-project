package p2p

import "time"

// Read-loop owned: rate and burst apply across all message types for this session.
func (n *Node) allowPeerMessage(p *peer) bool {
	p.rateMu.Lock()
	defer p.rateMu.Unlock()
	now := time.Now()
	if p.rateLast.IsZero() {
		p.rateTokens = 4000
		p.rateLast = now
	}
	p.rateTokens += now.Sub(p.rateLast).Seconds() * 200
	if p.rateTokens > 4000 {
		p.rateTokens = 4000
	}
	p.rateLast = now
	if p.rateTokens < 1 {
		return false
	}
	p.rateTokens--
	return true
}
