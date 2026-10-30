package dht

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"p2p-park/internal/proto"
)

func TestStoreQuotasAndReplacementAccounting(t *testing.T) {
	m := NewMemRecordStore()
	now := time.Now()
	for i := 0; i < MaxRecords; i++ {
		value := []byte(strings.Repeat("x", i+1))
		key := KeyFromImmutable(value)
		if err := m.Put(key, &proto.DHTRecord{Type: RecordImmutable, Value: value}, now); err != nil {
			t.Fatal(err)
		}
	}
	rec := &proto.DHTRecord{Type: RecordImmutable, Value: []byte("new")}
	key := KeyFromImmutable(rec.Value)
	if err := m.Put(key, rec, now); !errors.Is(err, ErrStoreFull) {
		t.Fatalf("quota: %v", err)
	}
	for k := range m.data {
		_ = m.Delete(k)
		break
	}
	if err := m.Put(key, rec, now); err != nil {
		t.Fatal(err)
	}
	expected := 0
	for _, r := range m.data {
		expected += recordBytes(r)
	}
	if expected != m.bytes || m.bytes > MaxRecordBytes {
		t.Fatal("inconsistent accounting")
	}
	invalid := *rec
	invalid.Value = []byte("different")
	if m.Put(key, &invalid, now) == nil {
		t.Fatal("store bypassed key validation")
	}
}
func TestRejectedPublishDoesNotPropagate(t *testing.T) {
	d, _ := New(strings.Repeat("01", 32))
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	key := KeyFromMutable(pub, "name")
	now := time.Now()
	rec := &proto.DHTRecord{Type: RecordMutable, PubKey: pub, Name: "name", Value: []byte("new"), Seq: 2}
	rec.Sig = SignMutable(priv, key, rec.Seq, 0, rec.Value)
	if err := d.rs.Put(key, rec, now); err != nil {
		t.Fatal(err)
	}
	stale := copyRecord(rec)
	stale.Seq = 1
	stale.Sig = SignMutable(priv, key, 1, 0, stale.Value)
	n := &fakeSender{selfID: d.selfIDHex}
	if err := d.PublishRecord(context.Background(), n, key, stale, DefaultPublishConfig()); err != ErrSeqTooLow {
		t.Fatalf("publish stale: %v", err)
	}
	if n.sentTo != "" || len(d.owned) != 0 {
		t.Fatal("rejected record propagated/marked owned")
	}
}
func TestRecordMetadataBounds(t *testing.T) {
	d, _ := New(strings.Repeat("01", 32))
	r := &proto.DHTRecord{Type: RecordImmutable, Value: []byte("v")}
	key := KeyFromImmutable(r.Value)
	r.Name = strings.Repeat("n", 257)
	if d.ValidateRecordAgainstKey(key, r) == nil {
		t.Fatal("oversize name")
	}
	r.Name = ""
	r.ExpiresUnix = time.Now().Add(31 * 24 * time.Hour).Unix()
	if d.ValidateRecordAgainstKey(key, r) == nil {
		t.Fatal("excessive TTL")
	}
}
func FuzzRecordValidation(f *testing.F) {
	f.Add([]byte(`{"type":"IMMUTABLE","value":"aGk="}`))
	f.Add([]byte(`{"type":"MUTABLE","pub_key":"AA=="}`))
	f.Fuzz(func(t *testing.T, b []byte) {
		if len(b) > 128*1024 {
			return
		}
		var r proto.DHTRecord
		if json.Unmarshal(b, &r) != nil {
			return
		}
		key := KeyFromImmutable(r.Value)
		_ = validateRecord(key, &r, time.Now())
	})
}

func TestRecordByteQuota(t *testing.T) {
	m := NewMemRecordStore()
	now := time.Now()
	accepted := 0
	for i := 0; i < MaxRecords; i++ {
		value := make([]byte, 65536)
		value[0] = byte(i)
		value[1] = byte(i >> 8)
		rec := &proto.DHTRecord{Type: RecordImmutable, Value: value}
		err := m.Put(KeyFromImmutable(value), rec, now)
		if err == ErrStoreFull {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		accepted++
	}
	if accepted == 0 || accepted >= MaxRecords || m.bytes > MaxRecordBytes {
		t.Fatal("byte quota not enforced", accepted, m.bytes)
	}
}
