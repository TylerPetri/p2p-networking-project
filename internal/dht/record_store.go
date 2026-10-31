package dht

import (
	"errors"
	"sync"
	"time"

	proto "p2p-park/internal/wire"
)

// RecordStore retains a key's record type until deletion/expiry; Get is untyped.
type RecordStore interface {
	Get(key [32]byte, now time.Time) (*proto.DHTRecord, bool)
	Put(key [32]byte, rec *proto.DHTRecord, now time.Time) error
	Delete(key [32]byte) error
	SweepExpired(now time.Time) int
	ForEach(fn func(key [32]byte, rec *proto.DHTRecord) bool)
	Len() int
}

const MaxRecords = 256
const MaxRecordBytes = 8 * 1024 * 1024

var ErrStoreFull = errors.New("dht: record store full")

func recordBytes(r *proto.DHTRecord) int {
	if r == nil {
		return 0
	}
	return len(r.Value) + len(r.PubKey) + len(r.Sig) + len(r.Name) + 128
}

type MemRecordStore struct {
	bytes int
	mu    sync.RWMutex
	data  map[[32]byte]*proto.DHTRecord
}

func NewMemRecordStore() *MemRecordStore {
	return &MemRecordStore{data: make(map[[32]byte]*proto.DHTRecord)}
}

func (m *MemRecordStore) Len() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.data)
}

func copyRecord(in *proto.DHTRecord) *proto.DHTRecord {
	if in == nil {
		return nil
	}
	out := *in
	if in.Value != nil {
		out.Value = append([]byte(nil), in.Value...)
	}
	if in.PubKey != nil {
		out.PubKey = append([]byte(nil), in.PubKey...)
	}
	if in.Sig != nil {
		out.Sig = append([]byte(nil), in.Sig...)
	}
	return &out
}

func (m *MemRecordStore) Get(key [32]byte, now time.Time) (*proto.DHTRecord, bool) {
	m.mu.RLock()
	rec := m.data[key]
	m.mu.RUnlock()
	if rec == nil {
		return nil, false
	}
	if rec.ExpiresUnix != 0 && now.Unix() > rec.ExpiresUnix {
		return nil, false
	}
	return copyRecord(rec), true
}

func (m *MemRecordStore) Put(key [32]byte, rec *proto.DHTRecord, now time.Time) error {
	if err := validateRecord(key, rec, now); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Reclaim expired entries before admission; no extra maintenance worker needed.
	for k, r := range m.data {
		if r.ExpiresUnix != 0 && now.Unix() > r.ExpiresUnix {
			m.bytes -= recordBytes(r)
			delete(m.data, k)
		}
	}
	oldRecord := m.data[key]
	// The existing key derivations share a namespace. Never let a different
	// record type bypass the retained mutable sequence or immutable value rules.
	if oldRecord != nil && oldRecord.Type != rec.Type {
		return ErrRecordTypeConflict
	}
	needed := m.bytes - recordBytes(oldRecord) + recordBytes(rec)
	if needed > MaxRecordBytes || (oldRecord == nil && len(m.data) >= MaxRecords) {
		return ErrStoreFull
	}
	// Enforce mutable seq rule (if same key exists)
	if rec.Type == RecordMutable {
		old := m.data[key]
		if old != nil && old.Type == RecordMutable {
			if rec.Seq <= old.Seq {
				return ErrSeqTooLow
			}
		}
	}

	m.bytes = needed
	m.data[key] = copyRecord(rec)
	return nil
}

func (m *MemRecordStore) Delete(key [32]byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.bytes -= recordBytes(m.data[key])
	delete(m.data, key)
	return nil
}

func (m *MemRecordStore) SweepExpired(now time.Time) int {
	if now.IsZero() {
		now = time.Now()
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	n := 0
	for k, rec := range m.data {
		if rec != nil && rec.ExpiresUnix != 0 && now.Unix() > rec.ExpiresUnix {
			m.bytes -= recordBytes(rec)
			delete(m.data, k)
			n++
		}
	}
	return n
}

func (m *MemRecordStore) ForEach(fn func(key [32]byte, rec *proto.DHTRecord) bool) {
	m.mu.RLock()
	// Snapshot keys so fn can be slow without holding lock too long.
	keys := make([][32]byte, 0, len(m.data))
	for k := range m.data {
		keys = append(keys, k)
	}
	m.mu.RUnlock()

	for _, k := range keys {
		m.mu.RLock()
		rec := m.data[k]
		m.mu.RUnlock()
		if rec == nil {
			continue
		}
		if !fn(k, copyRecord(rec)) {
			return
		}
	}
}
