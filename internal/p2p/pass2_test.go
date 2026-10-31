package p2p

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"runtime"
	"strings"
	"testing"
	"time"

	"p2p-park/internal/crypto/noiseconn"
	"p2p-park/internal/netx"
	"p2p-park/internal/proto"
)

type countedReader struct {
	r io.Reader
	n int
}

func (c *countedReader) Read(p []byte) (int, error) { n, e := c.r.Read(p); c.n += n; return n, e }
func TestEnvelopeBoundAcrossFrames(t *testing.T) {
	input := `{"payload":"` + strings.Repeat("a", MaxEnvelopeSize*4) + `"}` + "\n"
	c := &countedReader{r: strings.NewReader(input)}
	var env proto.Envelope
	if err := newEnvelopeDecoder(c).Decode(&env); err != ErrResourceLimit {
		t.Fatalf("oversize: %v", err)
	}
	if c.n > MaxEnvelopeSize+4096 {
		t.Fatalf("read %d bytes before rejection", c.n)
	}
	dec := newEnvelopeDecoder(strings.NewReader("{\"type\":\"hello\"}\n{\"type\":\"identify\"}\n"))
	if dec.Decode(&env) != nil || env.Type != proto.MsgHello {
		t.Fatal("hello")
	}
	if dec.Decode(&env) != nil || env.Type != proto.MsgIdentify {
		t.Fatal("lost read-ahead")
	}
}
func FuzzEnvelopeDecoder(f *testing.F) {
	for _, s := range []string{"{}\n", "{\"type\":\"gossip\",\"payload\":{}}\n", "[\n", "{}", "\n"} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		var env proto.Envelope
		_ = newEnvelopeDecoder(bytes.NewReader(b)).Decode(&env)
	})
}
func TestSignedGossipTamperReplayAndOriginNamespace(t *testing.T) {
	sender, receiver := securityNode(t), securityNode(t)
	p := &peer{id: sender.ID()}
	body := proto.MustMarshal(proto.Gossip{ID: "same-id", Channel: "global", Body: json.RawMessage(`{"text":"hello"}`)})
	env, err := sender.signGossip(proto.Envelope{Type: proto.MsgGossip, Payload: body})
	if err != nil {
		t.Fatal(err)
	}
	if !verifyGossip(env, time.Now()) {
		t.Fatal("valid signature rejected")
	}
	for _, mutate := range []func(*proto.Envelope){
		func(e *proto.Envelope) { e.FromID = receiver.ID() },
		func(e *proto.Envelope) { e.Payload = proto.MustMarshal(proto.Gossip{ID: "new-id", Channel: "global"}) },
		func(e *proto.Envelope) { e.SentUnix++ },
		func(e *proto.Envelope) { e.OriginBinding = nil },
		func(e *proto.Envelope) { e.Signature = nil },
	} {
		b, _ := json.Marshal(env)
		var bad proto.Envelope
		_ = json.Unmarshal(b, &bad)
		mutate(&bad)
		if verifyGossip(bad, time.Now()) {
			t.Fatal("accepted tampering")
		}
	}
	if verifyGossip(env, time.Now().Add(gossipLifetime+time.Second)) {
		t.Fatal("accepted stale signed gossip")
	}
	receiver.handleEnvelope(p, env)
	receiver.handleEnvelope(p, env)
	if len(receiver.incoming) != 1 {
		t.Fatalf("replay delivered %d times", len(receiver.incoming))
	}
	other := securityNode(t)
	second, _ := other.signGossip(proto.Envelope{Type: proto.MsgGossip, Payload: body})
	receiver.handleEnvelope(p, second)
	if len(receiver.incoming) != 2 {
		t.Fatal("one signer can suppress another's message ID")
	}
}
func TestReplayCacheSaturationFailsClosed(t *testing.T) {
	s := newSeenCache(time.Hour)
	for i := 0; i < 10000; i++ {
		s.Seen(string(rune(i)) + "id")
	}
	if len(s.items) != 8192 {
		t.Fatalf("cache size %d", len(s.items))
	}
	if !s.Seen("another") {
		t.Fatal("saturation evicted live replay entries")
	}
}
func TestDiscoveryDestinationPolicyAndPins(t *testing.T) {
	n := securityNode(t)
	for _, addr := range []netx.Addr{"169.254.169.254:80", "example.com:443", "10.1.2.3:4000", "[fe80::1]:1", "224.0.0.1:2", "0.0.0.0:1"} {
		if n.ConnectDiscovered(addr, "") == nil {
			t.Fatalf("accepted referral %s", addr)
		}
	}
	n.cfg.BindAddr = "127.0.0.1:0"
	if n.ConnectDiscovered("127.0.0.1:1", "bad-key") == nil {
		t.Fatal("accepted malformed expected key")
	}
	// The mismatch is checked immediately after XX, before Hello/admission.
	id, _ := NewIdentity()
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	_ = a.SetDeadline(time.Now().Add(time.Second))
	_ = b.SetDeadline(time.Now().Add(time.Second))
	done := make(chan error, 1)
	go func() { _, _, err := n.establishPeer(pipeConn{b}, true, n.ID()); done <- err }()
	// A genuine client for a different key must still be rejected by a pin.
	_, err := noiseClient(a, id)
	if err != nil {
		t.Fatal(err)
	}
	if err := <-done; err == nil {
		t.Fatal("accepted mismatched pin")
	}
	if n.PeerCount() != 0 {
		t.Fatal("pinned mismatch admitted")
	}
}

func TestAdmissionAndShutdownResourceCleanup(t *testing.T) {
	before := runtime.NumGoroutine()
	n, err := NewNode(NodeConfig{Network: netx.NewTCPNetwork(), BindAddr: "127.0.0.1:0", MaxConnections: 2, DisablePeerStore: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := n.Start(); err != nil {
		t.Fatal(err)
	}
	defer n.Stop()
	var clients []net.Conn
	for i := 0; i < 2; i++ {
		c, err := net.Dial("tcp", string(n.ListenAddr()))
		if err != nil {
			t.Fatal(err)
		}
		clients = append(clients, c)
		defer c.Close()
	}
	deadline := time.Now().Add(time.Second)
	for len(n.slots) != 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if len(n.slots) != 2 {
		t.Fatal("two pending inbound clients did not occupy the budget")
	}
	extra, err := net.Dial("tcp", string(n.ListenAddr()))
	if err != nil {
		t.Fatal(err)
	}
	defer extra.Close()
	_ = extra.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := extra.Read(make([]byte, 1)); err != io.EOF {
		t.Fatalf("excess connection not closed: %v", err)
	}
	if err := n.Stop(); err != nil {
		t.Fatal(err)
	}
	if len(n.slots) != 0 || n.PeerCount() != 0 {
		t.Fatal("shutdown retained connections")
	}
	deadline = time.Now().Add(time.Second)
	for runtime.NumGoroutine() > before+2 && time.Now().Before(deadline) {
		runtime.Gosched()
		time.Sleep(time.Millisecond)
	}
	if runtime.NumGoroutine() > before+2 {
		t.Fatalf("goroutines before=%d after=%d", before, runtime.NumGoroutine())
	}
}
func TestSlowEnvelopeTimeout(t *testing.T) {
	n := securityNode(t)
	n.cfg.MessageTimeout = 30 * time.Millisecond
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	p := &peer{conn: b, reader: newEnvelopeDecoder(b)}
	done := make(chan error, 1)
	go func() { var e proto.Envelope; done <- n.decodePeer(p, &e) }()
	_, _ = a.Write([]byte(`{"payload":`))
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("accepted partial JSON")
		}
	case <-time.After(time.Second):
		t.Fatal("partial JSON held reader indefinitely")
	}
}

func noiseClient(c io.ReadWriteCloser, id *Identity) (*noiseconn.HandshakeResult, error) {
	return noiseconn.NewSecureClient(c, id.NoisePriv[:], id.NoisePub[:], nil)
}

func TestOldSessionCannotRemoveReplacement(t *testing.T) {
	n := securityNode(t)
	old := &peer{id: "same"}
	replacement := &peer{id: "same"}
	n.peers["same"] = replacement
	n.removePeer("same", old)
	if n.peers["same"] != replacement {
		t.Fatal("stale session removed its replacement")
	}
	// Remove the synthetic peer before the cleanup hook tries to close it.
	delete(n.peers, "same")
}

func TestCorrectOutboundPinConnects(t *testing.T) {
	server := newTestNode(t, "pinned-server")
	client := newTestNode(t, "pinned-client", func(c *NodeConfig) { c.PeerPins = map[netx.Addr]string{server.ListenAddr(): server.ID()} })
	if err := client.ConnectTo(server.ListenAddr()); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for !client.hasPeer(server.ID()) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !client.hasPeer(server.ID()) {
		t.Fatal("expected pinned peer did not connect")
	}
}

func TestUnpinnedExplicitDialCoalescesWithPinnedDiscovery(t *testing.T) {
	n := securityNode(t)
	addr := netx.Addr("127.0.0.1:12345")
	n.dialing[addr] = n.ID() // an existing attempt already enforces a stronger key check
	if err := n.ConnectTo(addr); err != nil {
		t.Fatalf("duplicate explicit dial: %v", err)
	}
	if n.dialing[addr] != n.ID() {
		t.Fatal("coalescing weakened the existing pin")
	}
	if err := n.connectTo(addr, strings.Repeat("01", 32)); err == nil {
		t.Fatal("coalesced incompatible expected keys")
	}
	delete(n.dialing, addr)
}
