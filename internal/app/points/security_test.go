package points

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"

	"p2p-park/internal/p2p"
	"p2p-park/internal/proto"
)

func TestSignedSnapshotBindsPlayerIDToSigner(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	e := NewEngine("signer", priv, pub)
	valid, err := e.SnapshotSelf()
	if err != nil {
		t.Fatal(err)
	}
	if !verifySigned(valid) {
		t.Fatal("rejected correctly bound identity")
	}
	victim, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	forged, err := e.signSnapshot(proto.PointsSnapshot{PlayerID: p2p.PlayerIDFromPub(victim), Points: 999, Version: 1})
	if err != nil {
		t.Fatal(err)
	}
	if verifySigned(forged) {
		t.Fatal("accepted signature for someone else's player ID")
	}
}
