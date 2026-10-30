package channel

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"

	"golang.org/x/crypto/chacha20poly1305"
)

// ChannelKey is a 32-byte symmetric key for a channel.
type ChannelKey [32]byte

// NewRandomKey generates a new random channel key.
func NewRandomKey() (ChannelKey, error) {
	var k ChannelKey
	if _, err := io.ReadFull(rand.Reader, k[:]); err != nil {
		return ChannelKey{}, err
	}
	return k, nil
}

// KeyToHex encodes a key as a hex string for sharing.
func KeyToHex(k ChannelKey) string {
	return hex.EncodeToString(k[:])
}

// ParseKeyHex parses a hex string into a ChannelKey.
func ParseKeyHex(s string) (ChannelKey, error) {
	b, err := hex.DecodeString(s)
	if err != nil {
		return ChannelKey{}, err
	}
	if len(b) != 32 {
		return ChannelKey{}, fmt.Errorf("expected 32-byte key, got %d", len(b))
	}
	var k ChannelKey
	copy(k[:], b)
	return k, nil
}

// Encrypt encrypts plaintext using XChaCha20-Poly1305 with the given key.
func Encrypt(key ChannelKey, plaintext []byte) ([]byte, []byte, error) {
	return encrypt(key, plaintext, nil)
}
func EncryptForChannel(key ChannelKey, name string, plaintext []byte) ([]byte, []byte, error) {
	return encrypt(key, plaintext, []byte("p2p-park/channel/v1:"+name))
}
func encrypt(key ChannelKey, plaintext, ad []byte) ([]byte, []byte, error) {
	aead, err := chacha20poly1305.NewX(key[:])
	if err != nil {
		return nil, nil, err
	}
	nonce := make([]byte, chacha20poly1305.NonceSizeX)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, nil, err
	}
	ct := aead.Seal(nil, nonce, plaintext, ad)
	return nonce, ct, nil
}

// Decrypt decrypts ciphertext using XChaCha20-Poly1305 with the given key.
func Decrypt(key ChannelKey, nonce, ciphertext []byte) ([]byte, error) {
	return decrypt(key, nonce, ciphertext, nil)
}
func DecryptForChannel(key ChannelKey, name string, nonce, ciphertext []byte) ([]byte, error) {
	return decrypt(key, nonce, ciphertext, []byte("p2p-park/channel/v1:"+name))
}
func decrypt(key ChannelKey, nonce, ciphertext, ad []byte) ([]byte, error) {
	aead, err := chacha20poly1305.NewX(key[:])
	if err != nil {
		return nil, err
	}
	if len(nonce) != aead.NonceSize() {
		return nil, fmt.Errorf("invalid nonce length: %d", len(nonce))
	}
	pt, err := aead.Open(nil, nonce, ciphertext, ad)
	if err != nil {
		return nil, err
	}
	return pt, nil
}
