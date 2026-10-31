package p2p

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"p2p-park/internal/proto"
	"p2p-park/internal/wire"
)

func TestEngineNetworkAndRelayedAdmissionBoundary(t *testing.T) {
	sender, target := securityNode(t), securityNode(t)
	sender.cfg.NetworkID = sender.ID()
	target.cfg.NetworkID = target.ID()
	env, err := sender.signGossip(proto.Envelope{Type: proto.MsgGossip, Payload: proto.MustMarshal(proto.Gossip{ID: "network-bound", Channel: "engine/test", Body: json.RawMessage(`{}`)})})
	if err != nil {
		t.Fatal(err)
	}
	relay := &peer{id: target.ID()}
	target.handleEnvelope(relay, env)
	if len(target.incoming) != 0 {
		t.Fatal("cross-network signed replay accepted")
	}
	target.cfg.NetworkID = sender.cfg.NetworkID
	target.cfg.Restricted = true
	target.approved[relay.id] = true
	target.handleEnvelope(relay, env)
	if len(target.incoming) != 0 {
		t.Fatal("unapproved origin bypassed restricted admission through relay")
	}
	target.approved[sender.ID()] = true
	target.handleEnvelope(relay, env)
	if len(target.incoming) != 1 {
		t.Fatal("approved relayed origin rejected")
	}
}
func TestParkWireCompatibilityWithOmittedEngineFields(t *testing.T) {
	// Preserve field order and names of the merged 0.2.0 signed representation.
	type oldEnvelope struct {
		OriginPub     []byte           `json:"origin_pub,omitempty"`
		OriginBinding []byte           `json:"origin_binding,omitempty"`
		Signature     []byte           `json:"signature,omitempty"`
		SentUnix      int64            `json:"sent_unix,omitempty"`
		Type          wire.MessageType `json:"type"`
		FromID        string           `json:"from_id"`
		Payload       json.RawMessage  `json:"payload"`
	}
	old := oldEnvelope{SentUnix: 42, Type: wire.MsgGossip, FromID: "peer", Payload: json.RawMessage(`{"id":"same"}`)}
	current := wire.Envelope{SentUnix: old.SentUnix, Type: old.Type, FromID: old.FromID, Payload: old.Payload}
	a, _ := json.Marshal(old)
	b, _ := json.Marshal(current)
	if string(a) != string(b) {
		t.Fatalf("Park signed bytes changed: %s != %s", a, b)
	}
}

func TestEngineDHTContextCancelsPendingRPC(t *testing.T) {
	n := securityNode(t)
	remote := securityNode(t)
	ctx, cancel := context.WithCancel(context.Background())
	pctx, pcancel := context.WithCancel(n.ctx)
	defer pcancel()
	n.peers[remote.ID()] = &peer{id: remote.ID(), ctx: pctx, sendCh: make(chan wire.Envelope, 1)}
	// No writer/response: cancel only after the RPC has actually been enqueued.
	sender, stop := n.dhtSender(ctx)
	defer stop()
	done := make(chan error, 1)
	go func() { _, err := n.dht.QueryPing(sender, remote.ID(), 5*time.Second); done <- err }()
	select {
	case <-n.peers[remote.ID()].sendCh:
	case <-time.After(time.Second):
		t.Fatal("RPC not queued")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("RPC ignored cancellation")
	}
	// This synthetic peer has no socket; remove before node cleanup.
	delete(n.peers, remote.ID())
}
