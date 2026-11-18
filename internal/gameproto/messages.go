package gameproto

// ChatMessage is our payload that is stuffed into Gossip.Body for the global channel
type ChatMessage struct {
	Text      string `json:"text"`
	From      string `json:"from"`
	Timestamp int64  `json:"timestamp"`
}

// EncryptedMessage wraps AEAD nonce + ciphertext for encrypted channels.
type EncryptedMessage struct {
	Channel    string `json:"channel,omitempty"`
	Nonce      []byte `json:"nonce"`
	Ciphertext []byte `json:"ciphertext"`
}
