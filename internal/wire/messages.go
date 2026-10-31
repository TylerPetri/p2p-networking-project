package wire

import "encoding/json"

type MessageType string

const (
	MsgHello       MessageType = "hello"
	MsgPeerList    MessageType = "peer_list"
	MsgGossip      MessageType = "gossip"
	MsgIdentify    MessageType = "identify"
	MsgNatRegister MessageType = "nat_register"
	MsgNatRelay    MessageType = "nat_relay"
)

type Envelope struct {
	NetworkID     string          `json:"network_id,omitempty"`
	Recipient     string          `json:"recipient,omitempty"`
	OriginPub     []byte          `json:"origin_pub,omitempty"`
	OriginBinding []byte          `json:"origin_binding,omitempty"`
	Signature     []byte          `json:"signature,omitempty"`
	SentUnix      int64           `json:"sent_unix,omitempty"`
	Type          MessageType     `json:"type"`
	FromID        string          `json:"from_id"`
	Payload       json.RawMessage `json:"payload"`
}

type Hello struct {
	NetworkID string `json:"network_id,omitempty"`
	Name      string `json:"name"`
	Listen    string `json:"listen"`
	Protocol  string `json:"procol"`
}

type PeerInfo struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Addr       string `json:"addr"`
	PublicAddr string `json:"public_addr"`
}

type PeerList struct {
	Peers []PeerInfo `json:"peers"`
}

type Gossip struct {
	ID      string          `json:"id"`
	Channel string          `json:"channel"`
	Body    json.RawMessage `json:"body"`
}

type Identify struct {
	Name    string `json:"name"`     // display name
	UserPub []byte `json:"user_pub"` // ed25519 public key bytes
}

type NatRegister struct {
	UserID string `json:"user_id"` // hex(ed25519 pub) or your userID
	Name   string `json:"name"`    // display name (optional, for debugging)
}

type NatRelay struct {
	ToUserID string          `json:"to_user_id"` // target user
	Payload  json.RawMessage `json:"payload"`    // opaque; app defines
}

// MsgReady confirms engine-domain admission after Hello; legacy Park omits it.
const MsgReady MessageType = "ready"
