package engine_test

import (
	"context"
	"encoding/hex"
	"errors"
	"io"
	"log"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"p2p-park/engine"
	"p2p-park/internal/netx"
	"p2p-park/internal/p2p"
	"p2p-park/internal/wire"
)

// A hostile peer needs raw control-message access in this adversarial fixture;
// production consumers and examples remain restricted to the public engine API.
type observedNetwork struct {
	netx.Network
	closed chan struct{}
	once   sync.Once
}
type observedConnection struct {
	netx.Conn
	owner *observedNetwork
	once  sync.Once
}

func (c *observedConnection) Close() error {
	c.once.Do(func() { c.owner.once.Do(func() { close(c.owner.closed) }) })
	return c.Conn.Close()
}
func (n *observedNetwork) Accept() (netx.Conn, error) {
	c, err := n.Network.Accept()
	if err != nil {
		return nil, err
	}
	return &observedConnection{Conn: c, owner: n}, nil
}
func rawInvitationNode(t *testing.T, id string, n netx.Network) *p2p.Node {
	t.Helper()
	node, err := p2p.NewNode(p2p.NodeConfig{Network: n, BindAddr: "127.0.0.1:0", Protocol: engine.Protocol, NetworkID: id, DisablePeerStore: true, Logger: log.New(io.Discard, "", 0)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = node.Stop() })
	if err = node.Start(); err != nil {
		t.Fatal(err)
	}
	return node
}
func TestInvitationDoesNotTrustMaliciousReferrals(t *testing.T) {
	id := network(t)
	malicious := rawInvitationNode(t, id, netx.NewTCPNetwork())
	observed := &observedNetwork{Network: netx.NewTCPNetwork(), closed: make(chan struct{})}
	trap := rawInvitationNode(t, id, observed)
	recipient := node(t, id, "malicious-referrals", nil)
	invitation := engine.Invitation{Version: 1, NetworkID: id, Bootstraps: []engine.BootstrapPeer{{Address: string(malicious.ListenAddr()), PeerID: malicious.ID(), SigningID: hex.EncodeToString(malicious.Identity().SignPub)}}}
	invitationJoin(t, recipient, invitation)
	waitFor(t, func() bool { return malicious.PeerCount() == 1 })
	// Validly authenticated bootstrap sends an attacker-selected address with a
	// false expected key. Discovery must prove that key, not trust the bootstrap.
	referral := wire.PeerList{Peers: []wire.PeerInfo{{ID: strings.Repeat("1", 64), Addr: string(trap.ListenAddr())}, {ID: trap.ID(), Addr: "169.254.169.254:80"}}}
	if err := malicious.SendToPeer(recipient.PeerID(), wire.Envelope{Type: wire.MsgPeerList, Payload: wire.MustMarshal(referral)}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-observed.closed:
	case <-time.After(3 * time.Second):
		t.Fatal("referral identity attempt did not fail closed")
	}
	if trap.PeerCount() != 0 {
		t.Fatal("false referral admitted")
	}
	for _, p := range recipient.Peers() {
		if p.ID != malicious.ID() {
			t.Fatal("referral became trusted", p)
		}
	}
}

func TestInvitationCloseCancelsBootstrapAndJoinsWorkers(t *testing.T) {
	id := network(t)
	n := node(t, id, "close-invite", nil)
	server := node(t, id, "close-invite", nil)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	accepted := make(chan net.Conn, 1)
	go func() {
		c, err := l.Accept()
		if err == nil {
			accepted <- c
		}
	}()
	invitation := invite(t, server)
	invitation.Bootstraps[0].Address = l.Addr().String()
	result := make(chan error, 1)
	go func() { _, err := n.Join(context.Background(), invitation, loopbackInvites); result <- err }()
	select {
	case c := <-accepted:
		defer c.Close()
	case <-time.After(time.Second):
		t.Fatal("no bootstrap attempt")
	}
	stopped := make(chan struct{})
	go func() { _ = n.Close(); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("close did not join cancelled bootstrap")
	}
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("join retained worker")
	}
}

func TestInvitationExpirationDuringBootstrap(t *testing.T) {
	id := network(t)
	n := node(t, id, "expiring-invite", nil)
	server := node(t, id, "expiring-invite", nil)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	accepted := make(chan net.Conn, 1)
	go func() {
		c, err := l.Accept()
		if err == nil {
			accepted <- c
		}
	}()
	invitation := invite(t, server)
	invitation.Bootstraps[0].Address = l.Addr().String()
	invitation.ExpiresUnix = time.Now().Add(2 * time.Second).Unix()
	if _, err := n.Join(context.Background(), invitation, loopbackInvites); !errors.Is(err, engine.ErrInvitationExpired) {
		t.Fatal(err)
	}
	select {
	case c := <-accepted:
		defer c.Close()
	case <-time.After(time.Second):
		t.Fatal("no expiring attempt")
	}
}
