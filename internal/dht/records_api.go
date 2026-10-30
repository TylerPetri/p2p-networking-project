package dht

import (
	"context"
	"crypto/ed25519"
	"reflect"
	"time"

	"p2p-park/internal/proto"
)

type PublishConfig struct {
	K          int
	Alpha      int
	RPCTimeout time.Duration
}

func DefaultPublishConfig() PublishConfig {
	return PublishConfig{
		K:          20,
		Alpha:      3,
		RPCTimeout: 1200 * time.Millisecond,
	}
}

func (d *DHT) PutImmutable(ctx context.Context, n Sender, value []byte, ttl time.Duration) ([32]byte, error) {
	key := KeyFromImmutable(value)
	now := time.Now()

	rec := &proto.DHTRecord{
		Type:        RecordImmutable,
		Value:       value,
		CreatedUnix: now.Unix(),
	}
	if ttl > 0 {
		rec.ExpiresUnix = now.Add(ttl).Unix()
	}

	return key, d.PublishRecord(ctx, n, key, rec, DefaultPublishConfig())
}

func (d *DHT) PutMutable(ctx context.Context, n Sender, priv ed25519.PrivateKey, name string, value []byte, seq uint64, ttl time.Duration) ([32]byte, error) {
	pub := priv.Public().(ed25519.PublicKey)
	key := KeyFromMutable(pub, name)
	now := time.Now()

	rec := &proto.DHTRecord{
		Type:        RecordMutable,
		Name:        name,
		Value:       value,
		PubKey:      pub,
		Seq:         seq,
		CreatedUnix: now.Unix(),
	}
	if ttl > 0 {
		rec.ExpiresUnix = now.Add(ttl).Unix()
	}
	rec.Sig = SignMutable(priv, key, rec.Seq, rec.ExpiresUnix, rec.Value)

	return key, d.PublishRecord(ctx, n, key, rec, DefaultPublishConfig())
}

func (d *DHT) GetValue(ctx context.Context, n Sender, key [32]byte) (*proto.DHTRecord, bool, error) {
	return d.IterativeFindValue(ctx, n, key, DefaultValueLookupConfig())
}

func (d *DHT) PublishRecord(ctx context.Context, n Sender, key [32]byte, rec *proto.DHTRecord, cfg PublishConfig) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if cfg.K > 20 {
		cfg.K = 20
	}
	if cfg.Alpha > 3 {
		cfg.Alpha = 3
	}
	if cfg.K <= 0 {
		cfg.K = 20
	}
	if cfg.Alpha <= 0 {
		cfg.Alpha = 3
	}
	if cfg.RPCTimeout <= 0 {
		cfg.RPCTimeout = 1200 * time.Millisecond
	}

	// Validate before polluting local store
	if err := d.ValidateRecordAgainstKey(key, rec); err != nil {
		return err
	}

	now := time.Now()
	if err := d.rs.Put(key, rec, now); err != nil {
		// Identical retained records may be republished; stale/conflicting data cannot.
		old, ok := d.rs.Get(key, now)
		if !ok || !reflect.DeepEqual(old, rec) {
			return err
		}
	}

	// Mark as “owned” so maintenance can republish
	d.ownedMu.Lock()
	if len(d.owned) >= MaxRecords {
		for k := range d.owned {
			if _, ok := d.rs.Get(k, now); !ok {
				delete(d.owned, k)
			}
		}
	}
	if _, ok := d.owned[key]; ok || len(d.owned) < MaxRecords {
		d.owned[key] = ownedRec{nextRepublish: now.Add(30 * time.Minute)}
	}
	d.ownedMu.Unlock()

	// Find k-closest peers to the key by doing iterative FIND_NODE on the key target
	lookupCfg := DefaultLookupConfig()
	lookupCfg.K = cfg.K
	lookupCfg.Alpha = cfg.Alpha
	lookupCfg.RPCTimeout = cfg.RPCTimeout

	targetHex := NodeID(key).Hex()
	nodes, err := d.IterativeFindNode(n, targetHex, lookupCfg)
	if err != nil {
		return err
	}
	if len(nodes) > cfg.K {
		nodes = nodes[:cfg.K]
	}

	// STORE to those nodes with bounded concurrency = Alpha
	sem := make(chan struct{}, cfg.Alpha)
	errCh := make(chan error, len(nodes))

	for _, nd := range nodes {
		nd := nd
		select {
		case <-ctx.Done():
			return ctx.Err()
		case sem <- struct{}{}:
		}
		go func() {
			defer func() { <-sem }()
			w, e := d.QueryStore(n, nd.PeerID, KeyHex(key), rec, cfg.RPCTimeout)
			if e != nil {
				errCh <- e
				return
			}
			if w.Kind == "STORE_RESULT" && !w.OK {
				errCh <- ErrBadRecord
				return
			}
			errCh <- nil
		}()
	}

	var firstErr error
	for i := 0; i < len(nodes); i++ {
		if e := <-errCh; e != nil && firstErr == nil {
			firstErr = e
		}
	}
	return firstErr
}
