package grants

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"p2p-park/internal/proto"
)

func TestGrantCanonicalIDsAndCreatorScopedReplay(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	g := proto.QuizGrant{GrantID: strings.Repeat("01", 16), QuizID: strings.Repeat("02", 16), GrantorID: hex.EncodeToString(pub), RecipientID: hex.EncodeToString(pub), Points: 1, Timestamp: time.Now().Unix()}
	b, _ := proto.EncodeQuizGrantCanonical(g)
	g.Signature = ed25519.Sign(priv, b)
	ledger := NewLedger()
	if !ledger.ApplyGrant(g) || ledger.ApplyGrant(g) {
		t.Fatal("grant replay accounting")
	}
	bad := g
	bad.GrantID = "a\x00b"
	bad.QuizID = "c"
	b, _ = proto.EncodeQuizGrantCanonical(bad)
	bad.Signature = ed25519.Sign(priv, b)
	other := bad
	other.GrantID = "a"
	other.QuizID = "b\x00c"
	x, _ := proto.EncodeQuizGrantCanonical(other)
	if string(b) != string(x) {
		t.Fatal("test must demonstrate legacy delimiter ambiguity")
	}
	if VerifyGrant(bad) == nil || VerifyGrant(other) == nil {
		t.Fatal("ambiguous IDs accepted")
	}
	pub2, priv2, _ := ed25519.GenerateKey(rand.Reader)
	second := g
	second.GrantorID = hex.EncodeToString(pub2)
	b, _ = proto.EncodeQuizGrantCanonical(second)
	second.Signature = ed25519.Sign(priv2, b)
	if !ledger.ApplyGrant(second) {
		t.Fatal("another creator's ID suppressed a grant")
	}
	if ledger.Total(g.RecipientID) != 2 {
		t.Fatal("wrong total")
	}
}
