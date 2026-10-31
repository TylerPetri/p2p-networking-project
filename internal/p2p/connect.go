package p2p

import (
	"context"
	"time"

	"p2p-park/internal/netx"
)

// ConnectTo allows manual dialing (used by discovery/bootstraps).
func (n *Node) ConnectTo(addr netx.Addr) error { return n.connectTo(addr, n.cfg.PeerPins[addr]) }

func (n *Node) connectTo(addr netx.Addr, expected string) error {
	if err := n.ctx.Err(); err != nil {
		return err
	}
	n.mu.Lock()
	if pin, exists := n.dialing[addr]; exists {
		n.mu.Unlock()
		if expected != "" && pin != expected {
			return ErrResourceLimit
		}
		return nil
	}
	for _, p := range n.peers {
		if p.addr == addr && (expected == "" || p.id == expected) {
			n.mu.Unlock()
			return nil
		}
	}
	n.dialing[addr] = expected
	n.mu.Unlock()
	cleanup := func() { n.mu.Lock(); delete(n.dialing, addr); n.mu.Unlock() }
	if !n.reserveConnection() {
		cleanup()
		return ErrResourceLimit
	}
	var conn netx.Conn
	var err error
	if dialer, ok := n.cfg.Network.(interface {
		DialContext(context.Context, netx.Addr) (netx.Conn, error)
	}); ok {
		conn, err = dialer.DialContext(n.ctx, addr)
	} else {
		conn, err = n.cfg.Network.Dial(addr)
	}
	if err != nil {
		cleanup()
		n.releaseConnection()
		return err
	}
	go func() { defer cleanup(); defer n.releaseConnection(); n.runConn(conn, false, expected) }()
	return nil
}

// handleConn also admits direct/custom-transport callers through the same budget.
func (n *Node) handleConn(rawConn netx.Conn, inbound bool) {
	if !n.reserveConnection() {
		_ = rawConn.Close()
		return
	}
	defer n.releaseConnection()
	n.runConn(rawConn, inbound, "")
}

func (n *Node) runConn(rawConn netx.Conn, inbound bool, expected string, ready ...chan error) {
	notify := func(err error) {
		if len(ready) > 0 {
			ready[0] <- err
		}
	}
	stopCancel := context.AfterFunc(n.ctx, func() { _ = rawConn.Close() })
	defer stopCancel()
	// Include Noise and both Hello reads/writes, even for transports without deadlines.
	setupTimer := time.AfterFunc(5*time.Second, func() { _ = rawConn.Close() })
	p, secureCloser, err := n.establishPeer(rawConn, inbound, expected)
	setupTimer.Stop()
	if err != nil {
		notify(err)
		n.Logf("conn setup failed (inbound=%v): %v", inbound, err)
		_ = rawConn.Close()
		return
	}
	if p == nil {
		notify(ErrPeerClosed)
		_ = rawConn.Close()
		return
	}
	notify(nil)
	defer func() {
		n.removePeer(p.id, p)
		if p.writerDone != nil {
			<-p.writerDone
		}
		if secureCloser != nil {
			_ = secureCloser.Close()
		}
	}()

	if !n.cfg.IsSeed {
		if err := n.sendNatRegister(p); err != nil {
			n.Logf("send NAT register to %s failed: %v", p.id, err)
		}
	}

	if err := n.sendIdentify(p); err != nil {
		n.Logf("send identify to %s failed: %v", p.id, err)
	}

	n.mu.RLock()
	name := p.name
	n.mu.RUnlock()
	n.Logf("connected to peer id=%s name=%s addr=%s inbound=%v", p.id, name, p.addr, inbound)

	if err := n.sendPeerList(p); err != nil {
		n.Logf("send peer list to %s failed: %v", p.id, err)
	}

	n.runPeerReadLoop(p)
}
