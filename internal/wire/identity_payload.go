package wire

// NoiseIdentityPayload is sent inside the Noise handshake payload.
// It binds a user-facing identity to the Noise static key.
type NoiseIdentityPayload struct {
	Name      string `json:"name"`
	UserPub   []byte `json:"user_pub"`
	Signature []byte `json:"signature"` // Ed25519 signature over a domain and Noise static key
}
