package main

import (
	"context"
	"encoding/json"
	"github.com/TylerPetri/p2p-networking-project/engine"
	"io"
	"net"
	"os"
	"strings"
	"testing"
	"time"
)

func TestNoImplicitNonLoopbackExposure(t *testing.T) {
	for _, bind := range []string{"0.0.0.0:44001", "[::]:44001", "192.168.1.10:44001", "example.org:44001"} {
		err := run(context.Background(), config{state: t.TempDir(), bind: bind, duration: time.Second, probeTimeout: time.Second}, strings.NewReader("quit\n"), io.Discard)
		if err == nil {
			t.Fatal("accepted non-explicit local bind", bind)
		}
	}
}
func TestMalformedInviteBeforeNodeStartup(t *testing.T) {
	path := t.TempDir() + "/missing-invitation"
	err := run(context.Background(), config{state: t.TempDir(), bind: "127.0.0.1:0", invite: path, duration: time.Second, probeTimeout: time.Second}, strings.NewReader("quit\n"), io.Discard)
	if err == nil {
		t.Fatal("missing invitation accepted")
	}
}

func TestFaultAndJoinBoundsBeforeStartup(t *testing.T) {
	for _, edit := range []func(*config){
		func(c *config) { c.dropEvery = -1 }, func(c *config) { c.dropEvery = 10001 },
		func(c *config) { c.responseDelay = 3 * time.Second }, func(c *config) { c.responseDelay = -time.Second },
		func(c *config) { c.joinTimeout = time.Minute }, func(c *config) { c.joinTimeout = time.Millisecond },
		func(c *config) { c.link = "p2p://invite/malformed"; c.invite = "also-present" },
	} {
		cfg := config{state: t.TempDir(), bind: "127.0.0.1:0", duration: time.Second, probeTimeout: time.Second}
		edit(&cfg)
		if err := run(context.Background(), cfg, strings.NewReader("quit\n"), io.Discard); err == nil {
			t.Fatal("accepted invalid bounds")
		}
	}
}
func TestDirectMalformedLinkRejectedBeforeStateCreation(t *testing.T) {
	state := t.TempDir() + "/not-created"
	err := run(context.Background(), config{state: state, bind: "127.0.0.1:0", link: "p2p://invite/bad", duration: time.Second, probeTimeout: time.Second}, strings.NewReader("quit\n"), io.Discard)
	if err == nil {
		t.Fatal("malformed URI accepted")
	}
	if _, err := os.Stat(state); !os.IsNotExist(err) {
		t.Fatal("state was created before invitation validation", err)
	}
}

func TestQuitCancelsOutstandingDiagnosticJoin(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	accepted := make(chan net.Conn, 1)
	acceptDone := make(chan struct{})
	defer func() {
		_ = listener.Close()
		<-acceptDone
		select {
		case conn := <-accepted:
			_ = conn.Close()
		default:
		}
	}()
	go func() {
		defer close(acceptDone)
		conn, err := listener.Accept()
		if err == nil {
			accepted <- conn
		}
	}()
	input, commands := io.Pipe()
	output, events := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer input.Close()
	defer commands.Close()
	defer output.Close()
	defer events.Close()
	returned := make(chan error, 1)
	state := t.TempDir()
	go func() {
		returned <- run(ctx, config{state: state, bind: "127.0.0.1:0", allow: "127.0.0.1/32", duration: time.Minute, probeTimeout: time.Second, joinTimeout: 10 * time.Second}, input, events)
		_ = events.Close()
	}()
	ready := make(chan map[string]any, 1)
	go func() {
		var message map[string]any
		if json.NewDecoder(output).Decode(&message) == nil {
			ready <- message
		}
		_, _ = io.Copy(io.Discard, output)
	}()
	var message map[string]any
	select {
	case message = <-ready:
	case <-time.After(3 * time.Second):
		t.Fatal("no ready event")
	}
	invitation := engine.Invitation{Version: engine.InvitationVersion, NetworkID: message["network_id"].(string), Bootstraps: []engine.BootstrapPeer{{Address: listener.Addr().String(), PeerID: strings.Repeat("1", 64), SigningID: strings.Repeat("2", 64)}}}
	uri, err := invitation.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = io.WriteString(commands, "join "+uri+"\n"); err != nil {
		t.Fatal(err)
	}
	var stalled net.Conn
	select {
	case stalled = <-accepted:
		defer stalled.Close()
	case <-time.After(3 * time.Second):
		t.Fatal("join never dialed")
	}
	if _, err = io.WriteString(commands, "quit\n"); err != nil {
		t.Fatal(err)
	}
	select {
	case err = <-returned:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("quit waited for join timeout instead of cancellation")
	}
	_ = stalled.SetReadDeadline(time.Now().Add(time.Second))
	if _, err = io.Copy(io.Discard, stalled); err != nil {
		t.Fatal("setup socket not released", err)
	}
	rebound, err := net.Listen("tcp", message["address"].(string))
	if err != nil {
		t.Fatal("listener leaked", err)
	}
	_ = rebound.Close()
}
