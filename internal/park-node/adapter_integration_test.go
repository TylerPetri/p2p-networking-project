package parknode

import (
	"context"
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

	"github.com/TylerPetri/p2p-networking-project/engine"
	"github.com/TylerPetri/p2p-networking-project/internal/crypto/channel"
	proto "github.com/TylerPetri/p2p-networking-project/internal/gameproto"
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
	cfg := Config{Name: "player", Bind: "127.0.0.1:0", DiscoveryStateDir: t.TempDir(), DisableLAN: true}
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
func TestParkEngineChat(t *testing.T) {
	a, _ := game(t, func(c *Config) { c.Name = "alice" })
	b, bui := game(t, func(c *Config) { c.Name = "bob" })
	join(t, b, a)
	a.handleCommand("/say authenticated-chat")
	eventually(t, func() bool { return strings.Contains(bui.text(), "alice: authenticated-chat") })
}

func TestParkChatUsesVerifiedSigner(t *testing.T) {
	victim, _ := game(t, func(c *Config) { c.Name = "victim" })
	attacker, _ := game(t, func(c *Config) { c.Name = "attacker" })
	receiver, ui := game(t, nil)
	join(t, victim, receiver)
	join(t, attacker, receiver)
	// The payload's display label cannot substitute the authenticated signer.
	body, err := json.Marshal(proto.ChatMessage{From: "victim", Text: "spoof-name", Timestamp: time.Now().Unix()})
	if err != nil {
		t.Fatal(err)
	}
	if err := attacker.net.publish("global", body); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return strings.Contains(ui.text(), "attacker: spoof-name") })
	if strings.Contains(ui.text(), "victim: spoof-name") {
		t.Fatal("trusted payload name")
	}
}

func TestParkMembershipIsolationAndConcurrentLifecycle(t *testing.T) {
	approved, err := engine.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	restricted, ui := game(t, func(c *Config) {
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
		go func() { defer wg.Done(); member.handleCommand("/say concurrent-chat"); _ = member.Network.Peers() }()
	}
	wg.Wait()
	eventually(t, func() bool { return strings.Contains(ui.text(), "concurrent-chat") })
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
	body, err := json.Marshal(proto.EncryptedMessage{Channel: "other", Nonce: nonce, Ciphertext: ct})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.net.publish("encrypted", body); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	if strings.Contains(ui.text(), "cross-channel-forgery") {
		t.Fatal("channel label bypassed AEAD associated data")
	}
}
