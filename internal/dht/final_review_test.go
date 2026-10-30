package dht

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"testing"
	"time"

	"p2p-park/internal/proto"
)

func TestRetainedRecordTypeCannotChange(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	const name = "profile"
	key := KeyFromMutable(pub, name)
	mutable := &proto.DHTRecord{Type: RecordMutable, PubKey: pub, Name: name, Seq: 7, Value: []byte("signed value")}
	mutable.Sig = SignMutable(priv, key, mutable.Seq, 0, mutable.Value)
	// Existing derivations share a keyspace: this is a valid immutable record at
	// the mutable key, without breaking SHA-256 or possessing the signing key.
	immutable := &proto.DHTRecord{Type: RecordImmutable, Value: append(append([]byte(nil), pub...), []byte(name)...)}
	if KeyFromImmutable(immutable.Value) != key {
		t.Fatal("fixture must share a key")
	}
	for _, pair := range [][2]*proto.DHTRecord{{mutable, immutable}, {immutable, mutable}} {
		t.Run(pair[0].Type, func(t *testing.T) {
			store := NewMemRecordStore()
			now := time.Now()
			if err := store.Put(key, pair[0], now); err != nil {
				t.Fatal(err)
			}
			before := store.bytes
			if err := store.Put(key, pair[1], now); err != ErrRecordTypeConflict {
				t.Fatalf("expected record type conflict, got %v", err)
			}
			got, ok := store.Get(key, now)
			if !ok || got.Type != pair[0].Type || got.Seq != pair[0].Seq || store.bytes != before {
				t.Fatal("rejected type change altered retained state")
			}
			d, err := New(hex.EncodeToString(pub), WithRecordStore(store))
			if err != nil {
				t.Fatal(err)
			}
			sender := &fakeSender{selfID: d.selfIDHex}
			if err := d.PublishRecord(context.Background(), sender, key, pair[1], DefaultPublishConfig()); err != ErrRecordTypeConflict {
				t.Fatalf("conflicting type publish: %v", err)
			}
			if sender.sentTo != "" || len(d.owned) != 0 {
				t.Fatal("conflicting type propagated or marked owned")
			}
		})
	}
}
