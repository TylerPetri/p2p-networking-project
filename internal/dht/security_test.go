package dht

import (
	"crypto/ed25519"
	"crypto/rand"
	"strings"
	"sync"
	"testing"
	"time"

	"p2p-park/internal/proto"
)

func TestRPCRejectsWrongPeerAndKind(t *testing.T) {
	d, err := New(strings.Repeat("01", 32))
	if err != nil {
		t.Fatal(err)
	}
	expected := strings.Repeat("02", 32)
	other := strings.Repeat("03", 32)
	ch := make(chan proto.DHTWire, 1)
	if err := d.beginRPC(expected, "known-rpc", ch, "VALUE"); err != nil {
		t.Fatal(err)
	}
	defer d.endRPC(expected, "known-rpc")
	n := &fakeSender{selfID: d.selfIDHex}
	for _, bad := range []struct{ peer, kind string }{{other, "VALUE"}, {expected, "PONG"}} {
		d.HandleDHT(n, bad.peer, "127.0.0.1:1", "", proto.Envelope{Payload: proto.MustMarshal(proto.DHTWire{RPCID: "known-rpc", Kind: bad.kind})})
		select {
		case <-ch:
			t.Fatal("accepted mismatched RPC response")
		default:
		}
	}
	d.HandleDHT(n, expected, "127.0.0.1:1", "", proto.Envelope{Payload: proto.MustMarshal(proto.DHTWire{RPCID: "known-rpc", Kind: "VALUE"})})
	select {
	case <-ch:
	default:
		t.Fatal("valid response lost after spoof")
	}
	if d.Routing().Size() != 0 {
		t.Fatal("unverified response modified routing")
	}
}

func TestAdversarialRecordValidation(t *testing.T) {
	d, err := New(strings.Repeat("01", 32))
	if err != nil {
		t.Fatal(err)
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key := KeyFromMutable(pub, "profile")
	rec := &proto.DHTRecord{Type: RecordMutable, PubKey: pub, Name: "profile", Value: []byte("valid"), Seq: 2, ExpiresUnix: time.Now().Add(time.Hour).Unix()}
	rec.Sig = SignMutable(priv, key, rec.Seq, rec.ExpiresUnix, rec.Value)
	if err := d.ValidateRecordAgainstKey(key, rec); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*proto.DHTRecord){
		func(r *proto.DHTRecord) { r.Value = []byte("forged") },
		func(r *proto.DHTRecord) { r.Seq++ },
		func(r *proto.DHTRecord) { r.ExpiresUnix++ },
		func(r *proto.DHTRecord) { r.ExpiresUnix = time.Now().Add(-time.Hour).Unix() },
		func(r *proto.DHTRecord) { r.Name = "wrong-key" },
		func(r *proto.DHTRecord) { r.PubKey = []byte{1} },
		func(r *proto.DHTRecord) { r.Sig = []byte{1} },
		func(r *proto.DHTRecord) { r.Type = "unknown" },
		func(r *proto.DHTRecord) { r.Value = make([]byte, 65537) },
	} {
		bad := copyRecord(rec)
		mutate(bad)
		if err := d.ValidateRecordAgainstKey(key, bad); err == nil {
			t.Fatal("accepted invalid record")
		}
	}
	immutable := &proto.DHTRecord{Type: RecordImmutable, Value: []byte("immutable")}
	ik := KeyFromImmutable(immutable.Value)
	if err := d.ValidateRecordAgainstKey(ik, immutable); err != nil {
		t.Fatal(err)
	}
	immutable.Value[0] ^= 1
	if err := d.ValidateRecordAgainstKey(ik, immutable); err == nil {
		t.Fatal("accepted hash mismatch")
	}
	// Concurrent replay of a valid mutable record must allow exactly one insertion.
	var wg sync.WaitGroup
	var mu sync.Mutex
	success := 0
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if d.rs.Put(key, rec, time.Now()) == nil {
				mu.Lock()
				success++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if success != 1 {
		t.Fatalf("accepted %d duplicate sequence writes", success)
	}
	old := copyRecord(rec)
	old.Seq = 1
	old.Sig = SignMutable(priv, key, old.Seq, old.ExpiresUnix, old.Value)
	if err := d.rs.Put(key, old, time.Now()); err != ErrSeqTooLow {
		t.Fatalf("rollback: %v", err)
	}
}
