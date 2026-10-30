package parknode

import (
	"p2p-park/internal/crypto/channel"
	"testing"
)

func TestEncryptedCiphertextReplayCache(t *testing.T) {
	a := &App{}
	key := channel.ChannelKey{1}
	nonce := make([]byte, 24)
	ct := []byte("authenticated ciphertext")
	if !a.acceptCiphertext(key, nonce, ct) || a.acceptCiphertext(key, nonce, ct) {
		t.Fatal("accepted ciphertext replay")
	}
	for i := 0; i < 5000; i++ {
		a.acceptCiphertext(key, nonce, []byte{byte(i), byte(i >> 8)})
	}
	if len(a.encryptedReplay) != 4096 {
		t.Fatalf("cache size %d", len(a.encryptedReplay))
	}
	if a.acceptCiphertext(key, nonce, []byte("new")) {
		t.Fatal("saturation allowed replay eviction")
	}
}
