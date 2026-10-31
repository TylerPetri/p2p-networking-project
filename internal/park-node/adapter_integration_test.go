package parknode

import (
	"context"
	"crypto"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"p2p-park/engine"
	"p2p-park/internal/crypto/channel"
	proto "p2p-park/internal/gameproto"
)

type recordingPrinter struct {
	mu    sync.Mutex
	lines strings.Builder
}

func (p *recordingPrinter) Printf(f string, a ...any) {
	p.mu.Lock()
	defer p.mu.Unlock()
	fmt.Fprintf(&p.lines, f, a...)
}
func (p *recordingPrinter) Println(a ...any) { p.Printf("%s\n", fmt.Sprint(a...)) }
func (p *recordingPrinter) text() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return regexp.MustCompile(`\x1b\[[0-9;]*m`).ReplaceAllString(p.lines.String(), "")
}
func eventually(t *testing.T, f func() bool) {
	t.Helper()
	end := time.Now().Add(4 * time.Second)
	for time.Now().Before(end) {
		if f() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition did not become true")
}
func game(t *testing.T, edit func(*Config)) (*App, *recordingPrinter) {
	t.Helper()
	cfg := Config{Name: "player", Bind: "127.0.0.1:0", DataDir: t.TempDir(), DiscoveryStateDir: t.TempDir(), DisableLAN: true}
	if edit != nil {
		edit(&cfg)
	}
	a, err := New(cfg, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	p := &recordingPrinter{}
	a.ui = p
	t.Cleanup(a.StopAll)
	if err = a.Start(); err != nil {
		t.Fatal(err)
	}
	return a, p
}
func join(t *testing.T, a, b *App) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := a.Network.Connect(ctx, b.Network.Address()); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool {
		for _, p := range b.Network.Peers() {
			if p.ID == a.Network.PeerID() {
				return true
			}
		}
		return false
	})
}
func score(a *App, id string) int64 {
	for _, s := range a.Points.All() {
		if s.PlayerID == id {
			return s.Points
		}
	}
	return -1
}

func TestParkEngineChatPointsQuizAndGrantSync(t *testing.T) {
	a, _ := game(t, func(c *Config) { c.Name = "alice" })
	b, bui := game(t, func(c *Config) { c.Name = "bob" })
	join(t, b, a)
	a.handleCommand("/say authenticated-chat")
	eventually(t, func() bool { return strings.Contains(bui.text(), "alice: authenticated-chat") })
	a.handleCommand("/add 17")
	eventually(t, func() bool { return score(b, a.userIDHex()) == 17 })
	a.handleCommand("/quizask 5 60 What is two plus two? | four")
	eventually(t, func() bool { return len(b.Quiz.ListOpen()) == 1 })
	qid := b.Quiz.ListOpen()[0].QuizID
	b.handleCommand("/quizanswer " + qid + " four")
	eventually(t, func() bool { return b.Ledger.Total(b.userIDHex()) == 5 && a.Ledger.Total(b.userIDHex()) == 5 })
	// Late node obtains grants through the signed, recipient-bound sync topics.
	c, _ := game(t, nil)
	join(t, c, a)
	eventually(t, func() bool { return c.Ledger.Total(b.userIDHex()) == 5 })
	// Repeated application messages do not award again.
	b.handleCommand("/quizanswer " + qid + " four")
	time.Sleep(30 * time.Millisecond)
	if b.Ledger.Total(b.userIDHex()) != 5 {
		t.Fatal("duplicate award")
	}
	// Stop the late node and reopen its unchanged BoltDB with the same identity.
	c.StopAll()
	cfg := c.cfg
	cfg.Identity = c.identity
	reloaded, err := New(cfg, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	defer reloaded.StopAll()
	if reloaded.Ledger.Total(b.userIDHex()) != 5 {
		t.Fatal("persisted grants lost")
	}
	if err := reloaded.Start(); err != nil {
		t.Fatal(err)
	}
}

func TestParkIdentitySubstitutionAndAuthorization(t *testing.T) {
	victim, _ := game(t, func(c *Config) { c.Name = "victim" })
	attacker, _ := game(t, func(c *Config) { c.Name = "attacker" })
	receiver, ui := game(t, nil)
	join(t, victim, receiver)
	join(t, attacker, receiver)
	// The payload's display label cannot substitute the authenticated signer.
	attacker.net.broadcast("global", proto.ChatMessage{From: "victim", Text: "spoof-name", Timestamp: time.Now().Unix()})
	eventually(t, func() bool { return strings.Contains(ui.text(), "attacker: spoof-name") })
	if strings.Contains(ui.text(), "victim: spoof-name") {
		t.Fatal("trusted payload name")
	}
	victim.handleCommand("/quizask 9 60 Question | answer")
	eventually(t, func() bool { return len(receiver.Quiz.ListOpen()) == 1 })
	qid := receiver.Quiz.ListOpen()[0].QuizID
	// A different engine signer cannot author a creator-only result.
	attacker.net.broadcast("quiz", proto.QuizWire{Kind: "result", Result: &proto.QuizResult{QuizID: qid, PlayerID: receiver.userIDHex(), Correct: true, Delta: 999}})
	forged := proto.QuizGrant{GrantID: proto.NewID(), QuizID: qid, GrantorID: victim.userIDHex(), RecipientID: receiver.userIDHex(), Points: 999, Timestamp: time.Now().Unix()}
	bytes, _ := proto.EncodeQuizGrantCanonical(forged)
	forged.Signature, _ = attacker.identity.Sign(rand.Reader, bytes, crypto.Hash(0))
	attacker.net.broadcast("grants.response", proto.GrantSyncResponse{Grants: []proto.QuizGrant{forged}})
	time.Sleep(100 * time.Millisecond)
	if strings.Contains(ui.text(), "correct! +999") || receiver.Ledger.Total(receiver.userIDHex()) != 0 {
		t.Fatal("accepted substituted identity")
	}
	spoof := proto.SignedPointsSnapshot{Snapshot: proto.PointsSnapshot{PlayerID: victim.userIDHex(), Name: "victim", Points: 123456, Version: 999}, PubKey: attacker.identity.SigningPublicKey()}
	snapshotBytes, _ := proto.EncodeSnapshotCanonical(spoof.Snapshot)
	spoof.Signature, _ = attacker.identity.Sign(rand.Reader, snapshotBytes, crypto.Hash(0))
	attacker.net.broadcast("points", spoof)
	time.Sleep(50 * time.Millisecond)
	if score(receiver, victim.userIDHex()) == 123456 {
		t.Fatal("accepted substituted points identity")
	}
	// Valid independent grant records can be forwarded, but forged signatures cannot.
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	forged.GrantorID = hex.EncodeToString(pub)
	bytes, _ = proto.EncodeQuizGrantCanonical(forged)
	forged.Signature = ed25519.Sign(priv, bytes)
	attacker.net.broadcast("grants.response", proto.GrantSyncResponse{Grants: []proto.QuizGrant{forged}})
	eventually(t, func() bool { return receiver.Ledger.Total(receiver.userIDHex()) == 999 })
}

func TestParkMembershipIsolationAndConcurrentLifecycle(t *testing.T) {
	approved, err := engine.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	restricted, _ := game(t, func(c *Config) {
		c.Admission = engine.Restricted
		c.ApprovedPeers = map[string]string{approved.PeerID(): approved.SigningID()}
	})
	denied, _ := game(t, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := denied.Network.Connect(ctx, restricted.Network.Address()); err == nil {
		t.Fatal("unauthorized peer admitted")
	}
	otherID, _ := engine.NewNetworkID()
	isolated, _ := game(t, func(c *Config) { c.NetworkID = otherID })
	if err := isolated.Network.Connect(ctx, denied.Network.Address()); err == nil {
		t.Fatal("overlay isolation failed")
	}
	member, _ := game(t, func(c *Config) { c.Identity = approved })
	join(t, member, restricted)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); member.handleCommand("/add 1"); _ = member.Network.Peers() }()
	}
	wg.Wait()
	eventually(t, func() bool { return score(restricted, member.userIDHex()) == 8 })
	if err := member.Network.Disconnect(restricted.Network.PeerID()); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return len(restricted.Network.Peers()) == 0 })
	join(t, member, restricted)
	address := member.Network.Address()
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); member.StopAll() }()
	}
	wg.Wait()
	select {
	case <-member.Network.Done():
	default:
		t.Fatal("network still active")
	}
	l, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatal("listener leaked", err)
	}
	l.Close()
	if err := member.Start(); err != engine.ErrClosed {
		t.Fatal(err)
	}
}

func TestGrantSyncBatchFitsEngineLimit(t *testing.T) {
	gs := make([]proto.QuizGrant, grantSyncLimit)
	for i := range gs {
		gs[i] = proto.QuizGrant{GrantID: strings.Repeat("f", 32), QuizID: strings.Repeat("f", 32), GrantorID: strings.Repeat("f", 64), RecipientID: strings.Repeat("f", 64), Points: 1_000_000_000, Timestamp: time.Now().Unix(), Signature: make([]byte, 64)}
	}
	b, err := json.Marshal(proto.GrantSyncResponse{Grants: gs})
	if err != nil || len(b) > engine.MaxMessageSize {
		t.Fatal("bounded batch exceeds API cap", len(b), err)
	}
}

func TestParkBootstrapDiscoveryAndEchoIsolation(t *testing.T) {
	a, _ := game(t, func(c *Config) { c.IsSeed = true })
	b, bui := game(t, func(c *Config) { c.Bootstraps = []string{a.Network.Address()} })
	eventually(t, func() bool { return len(b.Network.Peers()) == 1 })
	c, _ := game(t, func(cfg *Config) { cfg.Bootstraps = []string{b.Network.Address()} })
	eventually(t, func() bool { return len(c.Network.Peers()) == 2 })
	c.handleCommand("/say discovered-chat")
	eventually(t, func() bool { return strings.Contains(bui.text(), "discovered-chat") })
	echo, err := engine.New(engine.Config{NetworkID: DefaultNetworkID, Application: "echo"})
	if err != nil {
		t.Fatal(err)
	}
	defer echo.Close()
	if err = echo.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err = echo.Connect(context.Background(), b.Network.Address()); err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(proto.ChatMessage{Text: "foreign-application", Timestamp: time.Now().Unix()})
	if err = echo.Publish(context.Background(), "global", payload); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	if strings.Contains(bui.text(), "foreign-application") {
		t.Fatal("application namespace isolation failed")
	}
}

func TestParkEncryptedChannelIsolationAndLongNames(t *testing.T) {
	a, _ := game(t, nil)
	b, ui := game(t, nil)
	join(t, a, b)
	key, err := channel.NewRandomKey()
	if err != nil {
		t.Fatal(err)
	}
	name := strings.Repeat("n", 124)
	a.encMu.Lock()
	a.encChannels[name] = key
	a.encMu.Unlock()
	b.encMu.Lock()
	b.encChannels[name] = key
	b.encChannels["other"] = key
	b.encMu.Unlock()
	a.sendEncrypted(name, "long-channel-message")
	eventually(t, func() bool { return strings.Contains(ui.text(), "long-channel-message") })
	nonce, ct, err := channel.EncryptForChannel(key, name, []byte(`{"text":"cross-channel-forgery"}`))
	if err != nil {
		t.Fatal(err)
	}
	a.net.broadcast("encrypted", proto.EncryptedMessage{Channel: "other", Nonce: nonce, Ciphertext: ct})
	time.Sleep(50 * time.Millisecond)
	if strings.Contains(ui.text(), "cross-channel-forgery") {
		t.Fatal("channel label bypassed AEAD associated data")
	}
}
