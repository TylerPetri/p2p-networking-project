package gameproto

import (
	"crypto/rand"
	"encoding/hex"
)

// NewID uses the standard random source for application record identifiers.
func NewID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}
