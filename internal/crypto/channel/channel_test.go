package channel

import (
	"bytes"
	"testing"
)

func TestDecryptRejectsMalformedNonceAndTamper(t *testing.T) {
	key, err := NewRandomKey()
	if err != nil {
		t.Fatal(err)
	}
	nonce, ct, err := Encrypt(key, []byte("hello"))
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range [][]byte{nil, {1}, make([]byte, 23), make([]byte, 25)} {
		if _, err := Decrypt(key, n, ct); err == nil {
			t.Fatal("accepted malformed nonce")
		}
	}
	got, err := Decrypt(key, nonce, ct)
	if err != nil || !bytes.Equal(got, []byte("hello")) {
		t.Fatal(err)
	}
	ct[0] ^= 1
	if _, err := Decrypt(key, nonce, ct); err == nil {
		t.Fatal("accepted tamper")
	}
}

func TestChannelAssociatedData(t *testing.T) {
	key, _ := NewRandomKey()
	nonce, ct, err := EncryptForChannel(key, "friends", []byte("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecryptForChannel(key, "other", nonce, ct); err == nil {
		t.Fatal("ciphertext moved between channels")
	}
	if _, err := DecryptForChannel(key, "friends", nonce, ct); err != nil {
		t.Fatal(err)
	}
}
func FuzzChannelDecrypt(f *testing.F) {
	f.Add([]byte{1}, []byte{2})
	f.Add(make([]byte, 24), make([]byte, 16))
	f.Fuzz(func(t *testing.T, nonce, ct []byte) {
		if len(ct) > 128*1024 {
			return
		}
		_, _ = DecryptForChannel(ChannelKey{}, "test", nonce, ct)
	})
}
