package parknode

import (
	"crypto/sha256"
	"p2p-park/internal/crypto/channel"
	"time"
)

// Checked only after successful AEAD authentication. Saturation drops new
// ciphertexts rather than evicting live replay entries. This state is not durable.
func (a *App) acceptCiphertext(key channel.ChannelKey, nonce, ct []byte) bool {
	h := sha256.New()
	h.Write(key[:])
	h.Write(nonce)
	h.Write(ct)
	var id [32]byte
	copy(id[:], h.Sum(nil))
	now := time.Now()
	a.replayMu.Lock()
	defer a.replayMu.Unlock()
	if a.encryptedReplay == nil {
		a.encryptedReplay = make(map[[32]byte]time.Time)
	}
	if now.After(a.replayNextSweep) {
		for k, t := range a.encryptedReplay {
			if now.Sub(t) > 24*time.Hour {
				delete(a.encryptedReplay, k)
			}
		}
		a.replayNextSweep = now.Add(time.Minute)
	}
	if _, ok := a.encryptedReplay[id]; ok {
		return false
	}
	if len(a.encryptedReplay) >= 4096 {
		return false
	}
	a.encryptedReplay[id] = now
	return true
}
