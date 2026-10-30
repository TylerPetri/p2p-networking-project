package netx

import (
	"net"
	"sync"
	"time"
)

type tcpNetwork struct {
	mu       sync.Mutex
	listener net.Listener
}

func NewTCPNetwork() Network {
	return &tcpNetwork{}
}

func (t *tcpNetwork) Listen(bindAddr string) (Addr, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	l, err := net.Listen("tcp", bindAddr)
	if err != nil {
		return "", err
	}
	t.listener = l
	return Addr(l.Addr().String()), nil
}

func (t *tcpNetwork) Accept() (Conn, error) {
	t.mu.Lock()
	l := t.listener
	t.mu.Unlock()

	if l == nil {
		return nil, net.ErrClosed
	}
	c, err := l.Accept()
	if err != nil {
		return nil, err
	}
	return &tcpConn{Conn: c}, nil
}

func (t *tcpNetwork) Dial(addr Addr) (Conn, error) {
	c, err := net.DialTimeout("tcp", string(addr), 5*time.Second)
	if err != nil {
		return nil, err
	}
	return &tcpConn{Conn: c}, nil
}

func (t *tcpNetwork) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.listener != nil {
		err := t.listener.Close()
		t.listener = nil
		return err
	}
	return nil
}

type tcpConn struct {
	net.Conn
}

func (c *tcpConn) RemoteAddr() Addr {
	return Addr(c.Conn.RemoteAddr().String())
}

// Bound blocked frame writes without imposing an idle-read timeout on quiet peers.
func (c *tcpConn) Write(p []byte) (int, error) {
	if err := c.Conn.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return 0, err
	}
	return c.Conn.Write(p)
}
