package engine_test

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"p2p-park/engine"
)

func network(t *testing.T) string {
	t.Helper()
	id, err := engine.NewNetworkID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}
func node(t *testing.T, nid, app string, edit func(*engine.Config)) *engine.Engine {
	t.Helper()
	cfg := engine.Config{NetworkID: nid, Application: app}
	if edit != nil {
		edit(&cfg)
	}
	n, err := engine.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = n.Close() })
	if err := n.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	return n
}
func waitPeer(t *testing.T, n *engine.Engine, id string) {
	t.Helper()
	end := time.Now().Add(3 * time.Second)
	for time.Now().Before(end) {
		for _, p := range n.Peers() {
			if p.ID == id {
				return
			}
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("peer %s did not appear", id)
}
func connect(t *testing.T, from, to *engine.Engine) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := from.Connect(ctx, to.Address()); err != nil {
		t.Fatal(err)
	}
	waitPeer(t, to, from.PeerID())
}
func receive(t *testing.T, n *engine.Engine) engine.Message {
	t.Helper()
	select {
	case m := <-n.Messages():
		return m
	case <-time.After(3 * time.Second):
		t.Fatal("no message")
	}
	return engine.Message{}
}
func TestLifecycleAndCancellation(t *testing.T) {
	for i := 0; i < 5; i++ {
		n, err := engine.New(engine.Config{NetworkID: network(t), Application: "lifecycle"})
		if err != nil {
			t.Fatal(err)
		}
		if err := n.Publish(context.Background(), "x", nil); !errors.Is(err, engine.ErrNotStarted) {
			t.Fatal(err)
		}
		canceled, cancel := context.WithCancel(context.Background())
		cancel()
		if err := n.Start(canceled); !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		ctx, stop := context.WithCancel(context.Background())
		if err := n.Start(ctx); err != nil {
			t.Fatal(err)
		}
		if err := n.Start(ctx); !errors.Is(err, engine.ErrAlreadyStarted) {
			t.Fatal(err)
		}
		addr := n.Address()
		stop()
		select {
		case <-n.Done():
		case <-time.After(3 * time.Second):
			t.Fatal("cancellation did not close engine")
		}
		var wg sync.WaitGroup
		for j := 0; j < 5; j++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if err := n.Close(); err != nil {
					t.Error(err)
				}
			}()
		}
		wg.Wait()
		if err := n.Start(context.Background()); !errors.Is(err, engine.ErrClosed) {
			t.Fatal(err)
		}
		l, err := net.Listen("tcp", addr)
		if err != nil {
			t.Fatal("listener leaked", err)
		}
		_ = l.Close()
		if _, ok := <-n.Messages(); ok {
			t.Fatal("message stream not closed")
		}
	}
}
func TestDirectPublishAndApplicationIsolation(t *testing.T) {
	id := network(t)
	a := node(t, id, "alpha", nil)
	b := node(t, id, "alpha", nil)
	c := node(t, id, "alpha", nil)
	connect(t, b, a)
	connect(t, c, a)
	payload := []byte("direct")
	if err := a.Send(context.Background(), b.PeerID(), "identify", payload); err != nil {
		t.Fatal(err)
	}
	payload[0] = 'X'
	m := receive(t, b)
	if string(m.Data) != "direct" || m.FromPeer != a.PeerID() || len(m.SigningKey) != 32 {
		t.Fatal("bad origin or copy", m)
	}
	select {
	case m := <-c.Messages():
		t.Fatal("direct message leaked", m)
	case <-time.After(50 * time.Millisecond):
	}
	if err := b.Publish(context.Background(), "broadcast", []byte("gossip")); err != nil {
		t.Fatal(err)
	}
	if m := receive(t, c); m.FromPeer != b.PeerID() || string(m.Data) != "gossip" {
		t.Fatal(m)
	}
	other := node(t, id, "beta", nil)
	connect(t, other, a)
	if err := a.Publish(context.Background(), "only-alpha", []byte("not beta")); err != nil {
		t.Fatal(err)
	}
	select {
	case m := <-other.Messages():
		t.Fatal("application namespace leak", m)
	case <-time.After(50 * time.Millisecond):
	}
	if err := a.Send(context.Background(), b.PeerID(), "oversized", make([]byte, engine.MaxMessageSize+1)); !errors.Is(err, engine.ErrInvalidMessage) {
		t.Fatal(err)
	}
}
func TestDomainsMembershipAndFailures(t *testing.T) {
	id := network(t)
	a := node(t, id, "test", nil)
	different := node(t, network(t), "test", nil)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := different.Connect(ctx, a.Address()); err == nil {
		t.Fatal("cross-network admission")
	}
	denied := node(t, id, "test", func(c *engine.Config) { c.Admission = engine.Restricted })
	if err := a.Connect(ctx, denied.Address()); err == nil {
		t.Fatal("remote rejection reported success")
	}
	if len(denied.Peers()) != 0 {
		t.Fatal("unapproved peer admitted")
	}
	identity, err := engine.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	restricted := node(t, id, "test", func(c *engine.Config) { c.Admission = engine.Restricted; c.ApprovedPeers = []string{identity.PeerID()} })
	approved := node(t, id, "test", func(c *engine.Config) { c.Identity = identity })
	connect(t, approved, restricted)
	if err := approved.Send(ctx, restricted.PeerID(), "allowed", []byte("member")); err != nil {
		t.Fatal(err)
	}
	_ = receive(t, restricted)
	if err := a.Connect(ctx, "127.0.0.1:1"); err == nil {
		t.Fatal("connection failure hidden")
	}
	canceled, stop := context.WithCancel(context.Background())
	stop()
	if err := a.Connect(canceled, restricted.Address()); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := approved.Disconnect(restricted.PeerID()); err != nil {
		t.Fatal(err)
	}
}
func TestConcurrentActivityAndDHT(t *testing.T) {
	id := network(t)
	a := node(t, id, "parallel", nil)
	b := node(t, id, "parallel", nil)
	connect(t, b, a)
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := a.Send(context.Background(), b.PeerID(), "parallel", []byte("ok")); err != nil {
				t.Error(err)
			}
			_ = a.Peers()
		}()
	}
	wg.Wait()
	for i := 0; i < 10; i++ {
		_ = receive(t, b)
	}
	solo := node(t, network(t), "records", nil)
	key, err := solo.PutImmutable(context.Background(), []byte("immutable"), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	record, ok, err := solo.Get(context.Background(), key)
	if err != nil || !ok || string(record.Value) != "immutable" {
		t.Fatal(record, ok, err)
	}
	mk, err := solo.PutMutable(context.Background(), "status", []byte("signed"), 1, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	record, ok, err = solo.Get(context.Background(), mk)
	if err != nil || !ok || record.Type != "MUTABLE" || record.Name != "records/status" {
		t.Fatal(record, ok, err)
	}
}
func TestPersistentIdentityAndValidation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keys", "identity.json")
	var wg sync.WaitGroup
	ids := make(chan string, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id, err := engine.LoadOrCreateIdentity(path)
			if err != nil {
				t.Error(err)
				return
			}
			ids <- id.PeerID()
		}()
	}
	wg.Wait()
	close(ids)
	first := ""
	for id := range ids {
		if first == "" {
			first = id
		}
		if id != first {
			t.Fatal("identity changed during concurrent creation")
		}
	}
	a, err := engine.LoadOrCreateIdentity(path)
	if err != nil {
		t.Fatal(err)
	}
	k := a.SigningPublicKey()
	k[0] ^= 1
	if string(k) == string(a.SigningPublicKey()) {
		t.Fatal("public key alias")
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.LoadOrCreateIdentity(path); err == nil {
		t.Fatal("permissive identity accepted")
	}
	_ = os.Chmod(path, 0600)
	_ = os.WriteFile(path, []byte("invalid"), 0600)
	if _, err := engine.LoadOrCreateIdentity(path); err == nil {
		t.Fatal("corrupt identity silently regenerated")
	}
}

func TestStartupFailureAndCloseBeforeStart(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	n, err := engine.New(engine.Config{NetworkID: network(t), Application: "failure", Bind: l.Addr().String()})
	if err != nil {
		t.Fatal(err)
	}
	if err := n.Start(context.Background()); err == nil {
		t.Fatal("occupied port accepted")
	}
	if err := n.Close(); err != nil {
		t.Fatal(err)
	}
	fresh, err := engine.New(engine.Config{NetworkID: network(t), Application: "unstarted"})
	if err != nil {
		t.Fatal(err)
	}
	if err := fresh.Close(); err != nil {
		t.Fatal(err)
	}
	if err := fresh.Start(context.Background()); !errors.Is(err, engine.ErrClosed) {
		t.Fatal(err)
	}
}

func TestStalledConnectCancellationReleasesBudget(t *testing.T) {
	id := network(t)
	a := node(t, id, "cancel", func(c *engine.Config) { c.MaxConnections = 1 })
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
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := a.Connect(ctx, l.Addr().String()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	select {
	case c := <-accepted:
		defer c.Close()
	case <-time.After(time.Second):
		t.Fatal("no accepted socket")
	}
	b := node(t, id, "cancel", nil)
	deadline := time.Now().Add(3 * time.Second)
	for {
		err = a.Connect(context.Background(), b.Address())
		if err == nil {
			break
		}
		if !errors.Is(err, engine.ErrResourceLimit) || time.Now().After(deadline) {
			t.Fatal("connection budget leaked", err)
		}
		time.Sleep(time.Millisecond)
	}
	if err := a.Send(context.Background(), b.PeerID(), "alive", []byte("session survives attempt context")); err != nil {
		t.Fatal(err)
	}
	_ = receive(t, b)
}

func TestIdentitySurvivesEngineRecreation(t *testing.T) {
	identity, err := engine.LoadOrCreateIdentity(filepath.Join(t.TempDir(), "identity.json"))
	if err != nil {
		t.Fatal(err)
	}
	id := network(t)
	remote := node(t, id, "reconnect", nil)
	for i := 0; i < 2; i++ {
		local := node(t, id, "reconnect", func(c *engine.Config) { c.Identity = identity })
		if local.PeerID() != identity.PeerID() {
			t.Fatal("identity changed")
		}
		// Remote may still be removing the prior socket. Wait for observed removal.
		deadline := time.Now().Add(3 * time.Second)
		for len(remote.Peers()) != 0 {
			if time.Now().After(deadline) {
				t.Fatal("stale session")
			}
			time.Sleep(time.Millisecond)
		}
		connect(t, local, remote)
		if err := local.Send(context.Background(), remote.PeerID(), "reconnected", []byte("ok")); err != nil {
			t.Fatal(err)
		}
		if m := receive(t, remote); m.FromPeer != identity.PeerID() {
			t.Fatal("wrong identity")
		}
		if err := local.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
