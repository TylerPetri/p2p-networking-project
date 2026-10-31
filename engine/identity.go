package engine

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/crypto/curve25519"
	"p2p-park/internal/p2p"
)

// Identity owns an immutable pair of transport/signing keys. Do not share its
// backing file with untrusted users. The API never returns private key material.
type Identity struct{ id *p2p.Identity }

func GenerateIdentity() (*Identity, error) {
	id, err := p2p.NewIdentity()
	if err != nil {
		return nil, err
	}
	return &Identity{id}, nil
}
func (i *Identity) PeerID() string {
	if i == nil || i.id == nil {
		return ""
	}
	return i.id.ID
}
func (i *Identity) SigningPublicKey() []byte {
	if i == nil || i.id == nil {
		return nil
	}
	return append([]byte(nil), i.id.SignPub...)
}

type diskIdentity struct {
	Version      int    `json:"version"`
	SigningSeed  []byte `json:"signing_seed"`
	NoisePrivate []byte `json:"noise_private"`
}

// LoadOrCreateIdentity atomically publishes a new 0600 file without overwriting
// an existing identity. Corrupt, oversized, symlink and permissive files fail;
// they are never silently regenerated. Hard-link publication requires a local
// filesystem supporting links. Parent paths must be controlled by the caller.
func LoadOrCreateIdentity(path string) (*Identity, error) {
	if path == "" {
		return nil, errors.New("engine: empty identity path")
	}
	id, err := loadIdentity(path)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	fresh, err := GenerateIdentity()
	if err != nil {
		return nil, err
	}
	b, err := json.Marshal(diskIdentity{1, fresh.id.SignPriv.Seed(), fresh.id.NoisePriv[:]})
	if err != nil {
		return nil, err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".identity-*")
	if err != nil {
		return nil, err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(b); err != nil {
		f.Close()
		return nil, err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return nil, err
	}
	if err = f.Close(); err != nil {
		return nil, err
	}
	if err = os.Link(f.Name(), path); err != nil && !errors.Is(err, os.ErrExist) {
		return nil, err
	}
	return loadIdentity(path)
}
func loadIdentity(path string) (*Identity, error) {
	st, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() || st.Mode().Perm()&0077 != 0 {
		return nil, errors.New("engine: identity must be a private regular file (0600)")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, 4097))
	if err != nil {
		return nil, err
	}
	if len(b) > 4096 {
		return nil, errors.New("engine: identity file too large")
	}
	var d diskIdentity
	if err = json.Unmarshal(b, &d); err != nil {
		return nil, fmt.Errorf("engine: invalid identity encoding")
	}
	if d.Version != 1 || len(d.SigningSeed) != 32 || len(d.NoisePrivate) != 32 {
		return nil, errors.New("engine: invalid identity version or key length")
	}
	priv := ed25519.NewKeyFromSeed(d.SigningSeed)
	pub := priv.Public().(ed25519.PublicKey)
	noisePub, err := curve25519.X25519(d.NoisePrivate, curve25519.Basepoint)
	if err != nil {
		return nil, err
	}
	id := &p2p.Identity{SignPriv: priv, SignPub: pub, ID: hex.EncodeToString(noisePub)}
	copy(id.NoisePriv[:], d.NoisePrivate)
	copy(id.NoisePub[:], noisePub)
	return &Identity{id}, nil
}
