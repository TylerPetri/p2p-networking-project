package engine_test

import (
	"context"
	"crypto"
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"

	"p2p-park/engine"
)

func TestApplicationSignerAndManagedLANShutdown(t *testing.T) {
	identity, err := engine.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	var signer crypto.Signer = identity
	message := []byte("canonical application record")
	signature, err := signer.Sign(rand.Reader, message, crypto.Hash(0))
	if err != nil {
		t.Fatal(err)
	}
	if !ed25519.Verify(signer.Public().(ed25519.PublicKey), message, signature) {
		t.Fatal("bad application signature")
	}
	if _, err = signer.Sign(rand.Reader, message, crypto.SHA256); err == nil {
		t.Fatal("accepted incompatible signer hash")
	}
	public := signer.Public().(ed25519.PublicKey)
	public[0] ^= 1
	if ed25519.Verify(public, message, signature) || !ed25519.Verify(identity.SigningPublicKey(), message, signature) {
		t.Fatal("public key aliases identity storage")
	}
	// Uses the existing UDP implementation, but no external peers are required.
	n, err := engine.New(engine.Config{NetworkID: network(t), Application: "lan-lifecycle", LANDiscovery: true})
	if err != nil {
		t.Fatal(err)
	}
	if err = n.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	time.Sleep(550 * time.Millisecond) // exercise discovery collection during closure
	done := make(chan struct{})
	go func() { _ = n.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("LAN workers did not join")
	}
}
