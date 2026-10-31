package engine

import (
	"context"
	"time"
)

// Key is the existing DHT's 256-bit key. Immutable values are content-addressed;
// mutable names are prefixed with the application label before key derivation.
type Key [32]byte

// Record is a validated snapshot, not a newest-replica or authorization guarantee.
type Record struct {
	Type, Name                  string
	Value, PublicKey, Signature []byte
	Sequence                    uint64
	ExpiresUnix                 int64
}

func (e *Engine) PutImmutable(ctx context.Context, value []byte, ttl time.Duration) (Key, error) {
	finish, err := e.beginDHT(ctx)
	if err != nil {
		return Key{}, err
	}
	defer finish()
	k, err := e.node.PutImmutable(ctx, append([]byte(nil), value...), ttl)
	return Key(k), err
}
func (e *Engine) PutMutable(ctx context.Context, name string, value []byte, seq uint64, ttl time.Duration) (Key, error) {
	finish, err := e.beginDHT(ctx)
	if err != nil {
		return Key{}, err
	}
	defer finish()
	k, err := e.node.PutMutable(ctx, e.application+"/"+name, append([]byte(nil), value...), seq, ttl)
	return Key(k), err
}
func (e *Engine) Get(ctx context.Context, key Key) (Record, bool, error) {
	finish, err := e.beginDHT(ctx)
	if err != nil {
		return Record{}, false, err
	}
	defer finish()
	r, ok, err := e.node.GetValue(ctx, [32]byte(key))
	if !ok || err != nil {
		return Record{}, ok, err
	}
	return Record{r.Type, r.Name, append([]byte(nil), r.Value...), append([]byte(nil), r.PubKey...), append([]byte(nil), r.Sig...), r.Seq, r.ExpiresUnix}, true, nil
}

// Register calls under the same lock used to begin closure: no Add/Wait race.
func (e *Engine) beginDHT(ctx context.Context) (func(), error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.state == 2 {
		return nil, ErrClosed
	}
	if e.state == 0 {
		return nil, ErrNotStarted
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	e.wg.Add(1)
	return e.wg.Done, nil
}
