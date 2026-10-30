package p2p

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"io"
	"log"
	"net"
	"testing"
	"time"

	"p2p-park/internal/crypto/noiseconn"
	"p2p-park/internal/netx"
	"p2p-park/internal/proto"
)

type pipeConn struct{ net.Conn }

func (p pipeConn) RemoteAddr() netx.Addr { return "127.0.0.1:12345" }
func securityNode(t *testing.T) *Node {
	t.Helper()
	n, err := NewNode(NodeConfig{Network: netx.NewTCPNetwork(), Protocol: "security/1", Logger: log.New(io.Discard, "", 0), IsSeed: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = n.Stop() })
	return n
}

func TestSessionRejectsSpoofedHelloAndSigningIdentity(t *testing.T) {
	for _, which := range []string{"hello", "signature", "valid"} {
		t.Run(which, func(t *testing.T) {
			n := securityNode(t)
			id, err := NewIdentity()
			if err != nil {
				t.Fatal(err)
			}
			payload := proto.NoiseIdentityPayload{Name: "attacker", UserPub: id.SignPub, Signature: ed25519.Sign(id.SignPriv, identityBinding(id.NoisePub[:]))}
			if which == "signature" {
				payload.Signature[0] ^= 1
			}
			a, b := net.Pipe()
			defer a.Close()
			defer b.Close()
			_ = a.SetDeadline(time.Now().Add(3 * time.Second))
			_ = b.SetDeadline(time.Now().Add(3 * time.Second))
			type result struct {
				p   *peer
				err error
			}
			ch := make(chan result, 1)
			go func() { p, _, err := n.establishPeer(pipeConn{b}, true); ch <- result{p, err} }()
			hs, err := noiseconn.NewSecureClient(a, id.NoisePriv[:], id.NoisePub[:], proto.MustMarshal(payload))
			if err != nil {
				t.Fatal(err)
			}
			if which == "signature" {
				if r := <-ch; r.err == nil {
					t.Fatal("accepted false signing identity")
				}
				return
			}
			// Drain responder Hello so the pipe writer can progress.
			var serverHello proto.Envelope
			if err := json.NewDecoder(hs.Conn).Decode(&serverHello); err != nil {
				t.Fatal(err)
			}
			peerID := id.ID
			if which == "hello" {
				peerID = n.ID()
			}
			hello := proto.Envelope{Type: proto.MsgHello, FromID: peerID, Payload: proto.MustMarshal(proto.Hello{Protocol: n.cfg.Protocol, Listen: "127.0.0.1:12345"})}
			var wire bytes.Buffer
			enc := json.NewEncoder(&wire)
			_ = enc.Encode(hello)
			// Coalesce the next envelope with Hello: decoder read-ahead must survive setup.
			next := proto.Envelope{Type: proto.MsgGossip, FromID: id.ID}
			_ = enc.Encode(next)
			if _, err := hs.Conn.Write(wire.Bytes()); err != nil {
				t.Fatal(err)
			}
			r := <-ch
			if which == "hello" {
				if r.err == nil {
					t.Fatal("accepted spoofed network ID")
				}
				return
			}
			if r.err != nil || r.p == nil {
				t.Fatalf("valid peer rejected: %v", r.err)
			}
			var got proto.Envelope
			if err := r.p.reader.Decode(&got); err != nil || got.Type != next.Type {
				t.Fatalf("lost buffered envelope: %v %+v", err, got)
			}
		})
	}
}

func TestIdentifyAndNATCannotReplaceHandshakeIdentity(t *testing.T) {
	n := securityNode(t)
	id, err := NewIdentity()
	if err != nil {
		t.Fatal(err)
	}
	p := &peer{id: id.ID, userID: PlayerIDFromPub(id.SignPub), userPub: id.SignPub, name: "original"}
	n.handleIdentify(p, proto.Envelope{Payload: proto.MustMarshal(proto.Identify{Name: "spoof", UserPub: n.id.SignPub})})
	n.handleNatRegister(p, proto.Envelope{Payload: proto.MustMarshal(proto.NatRegister{UserID: PlayerIDFromPub(n.id.SignPub)})})
	if p.name != "original" || p.userID != PlayerIDFromPub(id.SignPub) || len(n.natByUserID) != 0 {
		t.Fatal("unverified identity replaced handshake identity")
	}
}

func TestStopClosesPendingHandshake(t *testing.T) {
	n := securityNode(t)
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	done := make(chan struct{})
	go func() { n.handleConn(pipeConn{b}, true); close(done) }()
	_ = n.Stop()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("shutdown left handshake blocked")
	}
	if err := n.ConnectTo("127.0.0.1:1"); err != context.Canceled {
		t.Fatalf("dial after stop: %v", err)
	}
}

func TestHandshakeSetupTimeout(t *testing.T) {
	n := securityNode(t)
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	done := make(chan struct{})
	go func() { n.handleConn(pipeConn{b}, true); close(done) }()
	select {
	case <-done:
	case <-time.After(7 * time.Second):
		t.Fatal("stalled handshake has no setup timeout")
	}
}

func TestConcurrentIdentityMetadata(t *testing.T) {
	n := securityNode(t)
	id, err := NewIdentity()
	if err != nil {
		t.Fatal(err)
	}
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	p := &peer{id: id.ID, userID: PlayerIDFromPub(id.SignPub), userPub: id.SignPub, name: "original", conn: a}
	if !n.addPeer(p) {
		t.Fatal("add peer")
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 100; i++ {
			n.handleIdentify(p, proto.Envelope{Payload: proto.MustMarshal(proto.Identify{Name: "updated", UserPub: id.SignPub})})
			n.handleNatRegister(p, proto.Envelope{Payload: proto.MustMarshal(proto.NatRegister{UserID: PlayerIDFromPub(id.SignPub)})})
		}
	}()
	for i := 0; i < 100; i++ {
		n.SnapshotPeers()
		n.snapshotPeersInfo()
		n.PeerDisplayName(p.id)
	}
	<-done
}
