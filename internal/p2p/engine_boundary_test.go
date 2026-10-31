package p2p

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
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
	target.approved[relay.id] = hex.EncodeToString(target.id.SignPub)
	target.handleEnvelope(relay, env)
	if len(target.incoming) != 0 {
		t.Fatal("unapproved origin bypassed restricted admission through relay")
	}
	target.approved[sender.ID()] = hex.EncodeToString(sender.id.SignPub)
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

func TestRelayedTransportClaimCannotBypassSigningApproval(t *testing.T) {
	victim, attacker, target := securityNode(t), securityNode(t), securityNode(t)
	attacker.cfg.NetworkID = target.ID()
	target.cfg.NetworkID = target.ID()
	target.cfg.Restricted = true
	target.approved[victim.ID()] = hex.EncodeToString(victim.id.SignPub)
	env, err := attacker.signGossip(wire.Envelope{Type: wire.MsgGossip, Payload: wire.MustMarshal(wire.Gossip{ID: "forged-transport", Channel: "engine/test", Body: json.RawMessage(`{}`)})})
	if err != nil {
		t.Fatal(err)
	}
	// Any signing key can sign a public Noise key. This is a valid Ed25519
	// signature, but no evidence that the signer owns the victim's Noise key.
	env.FromID = victim.ID()
	env.OriginBinding = ed25519.Sign(attacker.id.SignPriv, identityBinding(victim.id.NoisePub[:]))
	b, err := gossipSigningBytes(env)
	if err != nil {
		t.Fatal(err)
	}
	env.Signature = ed25519.Sign(attacker.id.SignPriv, b)
	if !verifyGossip(env, time.Now()) {
		t.Fatal("adversarial signature fixture is invalid")
	}
	relay := &peer{id: attacker.ID()}
	target.handleEnvelope(relay, env)
	if len(target.incoming) != 0 {
		t.Fatal("transport-only claim bypassed pinned signing approval")
	}
	target.cfg.Restricted = false
	target.peers[victim.ID()] = &peer{userID: hex.EncodeToString(victim.id.SignPub)}
	target.handleEnvelope(relay, env)
	delete(target.peers, victim.ID())
	if len(target.incoming) != 0 {
		t.Fatal("relay replaced a known authenticated signing binding")
	}
}
