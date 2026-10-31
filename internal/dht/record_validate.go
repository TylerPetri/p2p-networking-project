package dht

import (
	"crypto/ed25519"
	"crypto/sha256"
	"errors"
	"time"

	proto "p2p-park/internal/wire"
)

var ErrInvalidRecord = errors.New("dht: invalid record")

func (d *DHT) ValidateRecordAgainstKey(key [32]byte, rec *proto.DHTRecord) error {
	return validateRecord(key, rec, time.Now())
}

func validateRecord(key [32]byte, rec *proto.DHTRecord, now time.Time) error {
	if rec == nil {
		return ErrInvalidRecord
	}

	// Expiry sanity
	if rec.ExpiresUnix != 0 && now.Unix() > rec.ExpiresUnix {
		return ErrInvalidRecord
	}

	if len(rec.Name) > 256 || len(rec.PubKey) > ed25519.PublicKeySize || len(rec.Sig) > ed25519.SignatureSize {
		return ErrRecordTooLarge
	}
	if rec.ExpiresUnix > now.Add(30*24*time.Hour).Unix() {
		return ErrInvalidRecord
	}
	// Size caps
	const maxValue = 64 * 1024
	if len(rec.Value) > maxValue {
		return ErrRecordTooLarge
	}

	switch rec.Type {
	case RecordImmutable:
		sum := sha256.Sum256(rec.Value)
		if sum != key {
			return ErrKeyMismatch
		}
		return nil

	case RecordMutable:
		if len(rec.PubKey) != ed25519.PublicKeySize || len(rec.Sig) == 0 {
			return ErrBadRecord
		}
		pub := ed25519.PublicKey(rec.PubKey)
		// key must equal sha256(pub || name)
		expect := KeyFromMutable(pub, rec.Name)
		if expect != key {
			return ErrKeyMismatch
		}
		if !VerifyMutable(pub, key, rec.Seq, rec.ExpiresUnix, rec.Value, rec.Sig) {
			return ErrBadSignature
		}
		return nil

	default:
		return ErrBadRecord
	}
}
