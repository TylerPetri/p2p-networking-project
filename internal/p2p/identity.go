package p2p

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"

	"golang.org/x/crypto/curve25519"
)

type Identity struct {
	SignPriv ed25519.PrivateKey
	SignPub  ed25519.PublicKey

	NoisePriv [32]byte
	NoisePub  [32]byte

	ID string // hex-encoded public key
}

// PlayerIDFromPub derives the canonical player ID from a public key.
func PlayerIDFromPub(pub ed25519.PublicKey) string {
	return hex.EncodeToString(pub)
}

// noiseKeypair generates an X25519 keypair.
func noiseKeypair() (priv, pub [32]byte, err error) {
	if _, err = io.ReadFull(rand.Reader, priv[:]); err != nil {
		return
	}
	// Clamp as per X25519 spec.
	priv[0] &= 248
	priv[31] &= 127
	priv[31] |= 64
	curve25519.ScalarBaseMult(&pub, &priv)
	return
}

func NewIdentity() (*Identity, error) {
	signPub, signPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}

	nPriv, nPub, err := noiseKeypair()
	if err != nil {
		return nil, err
	}

	id := hex.EncodeToString(nPub[:])

	return &Identity{
		SignPriv:  signPriv,
		SignPub:   signPub,
		NoisePriv: nPriv,
		NoisePub:  nPub,
		ID:        id,
	}, nil
}

// CloneIdentity validates and owns independent key slices before any networking.
func CloneIdentity(in *Identity) (*Identity, error) {
	if in == nil || len(in.SignPriv) != ed25519.PrivateKeySize || len(in.SignPub) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("invalid identity")
	}
	priv := ed25519.NewKeyFromSeed(in.SignPriv.Seed())
	pub := priv.Public().(ed25519.PublicKey)
	noisePub, err := curve25519.X25519(in.NoisePriv[:], curve25519.Basepoint)
	if err != nil || !bytes.Equal(priv, in.SignPriv) || !bytes.Equal(pub, in.SignPub) || !bytes.Equal(noisePub, in.NoisePub[:]) || hex.EncodeToString(noisePub) != in.ID {
		return nil, fmt.Errorf("inconsistent identity keys")
	}
	out := *in
	out.SignPriv = append(ed25519.PrivateKey(nil), priv...)
	out.SignPub = append(ed25519.PublicKey(nil), pub...)
	return &out, nil
}
