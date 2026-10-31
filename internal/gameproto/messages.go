package gameproto

// ChatMessage is our payload that is stuffed into Gossip.Body for the global channel
type ChatMessage struct {
	Text      string `json:"text"`
	From      string `json:"from"`
	Timestamp int64  `json:"timestamp"`
}

// PointsSnapshot represents "here is my current score".
// Each identity controls its own score: last higher Version wins.
type PointsSnapshot struct {
	PlayerID string `json:"player_id"`
	Name     string `json:"name"`
	Points   int64  `json:"points"`
	Version  uint64 `json:"version"`
}

// SignedPointsSnapshot wraps a PointsSnapshot with an ed25519 signature.
// PubKey and Signature are []byte; JSON will base64-encode them.
type SignedPointsSnapshot struct {
	Snapshot  PointsSnapshot `json:"snapshot"`
	PubKey    []byte         `json:"pub_key"`
	Signature []byte         `json:"sig"`
}

// EncryptedMessage wraps AEAD nonce + ciphertext for encrypted channels.
type EncryptedMessage struct {
	Channel    string `json:"channel,omitempty"`
	Nonce      []byte `json:"nonce"`
	Ciphertext []byte `json:"ciphertext"`
}

type GrantSyncSummary struct {
	MaxTimestamp   int64    `json:"max_ts"`
	RecentGrantIDs []string `json:"recent_ids,omitempty"`
}

type GrantSyncRequest struct {
	SinceTimestamp int64 `json:"since_ts"`
	Limit          int   `json:"limit"`
}

type GrantSyncResponse struct {
	Grants []QuizGrant `json:"grants"`
}
