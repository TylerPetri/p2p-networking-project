package grants

import (
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"time"

	proto "p2p-park/internal/gameproto"
)

var (
	ErrInvalidGrant = errors.New("invalid grant")
	ErrBadSignature = errors.New("bad grant signature")
)

// VerifyGrant performs deterministic, local validation of a QuizGrant.
// It does NOT check whether the grant was already seen (dedup is the caller's job).
func VerifyGrant(g proto.QuizGrant) error {
	if g.GrantID == "" || g.QuizID == "" || g.GrantorID == "" || g.RecipientID == "" {
		return ErrInvalidGrant
	}
	if !canonicalHex(g.GrantID, 16) || !canonicalHex(g.QuizID, 16) || !canonicalHex(g.RecipientID, 32) || !canonicalHex(g.GrantorID, 32) || len(g.Signature) != ed25519.SignatureSize {
		return ErrInvalidGrant
	}
	if g.Points <= 0 || g.Points > 1_000_000_000 {
		return ErrInvalidGrant
	}

	// expiry window: ignore absurd timestamps (avoids unbounded bbolt growth from garbage).
	now := time.Now().Unix()
	if g.Timestamp > now+60 || g.Timestamp < now-60*60*24*365 {
		return ErrInvalidGrant
	}

	pubBytes, err := hex.DecodeString(g.GrantorID)
	if err != nil || len(pubBytes) != ed25519.PublicKeySize {
		return ErrInvalidGrant
	}
	msg, _ := proto.EncodeQuizGrantCanonical(g)
	if !ed25519.Verify(ed25519.PublicKey(pubBytes), msg, g.Signature) {
		return ErrBadSignature
	}
	return nil
}

// Reject alternate/NUL-delimited encodings before the legacy canonical signer.
func canonicalHex(s string, n int) bool {
	b, err := hex.DecodeString(s)
	return err == nil && len(b) == n && hex.EncodeToString(b) == s
}

const MaxGrants = 10000
