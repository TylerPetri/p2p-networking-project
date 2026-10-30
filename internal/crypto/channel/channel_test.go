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
