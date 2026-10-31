package proto

import (
	"p2p-park/internal/wire"
)

// ApplicationProtocol is enforced by the park-node application Hello exchange.
const ApplicationProtocol = "park-p2p/0.2.0"

type MessageType = wire.MessageType

const (
	MsgHello             MessageType = "hello"
	MsgPeerList          MessageType = "peer_list"
	MsgGossip            MessageType = "gossip"
	MsgIdentify          MessageType = "identify"
	MsgNatRegister       MessageType = "nat_register"
	MsgNatRelay          MessageType = "nat_relay"
	MsgGrantSyncSummary  MessageType = "grant_sync_summary"
	MsgGrantSyncRequest  MessageType = "grant_sync_request"
	MsgGrantSyncResponse MessageType = "grant_sync_response"
)

type Envelope = wire.Envelope

// Hello is exchanged on connection setup.
type Hello = wire.Hello

// PeerInfo describes another peer we know about.
type PeerInfo = wire.PeerInfo

// PeerList is exchanged through gossip to populate other peers' Peerlist.
type PeerList = wire.PeerList

// Gossip is our generics "app-level broadcast" payload.
type Gossip = wire.Gossip

// ChatMessage is our payload that is stuffed into Gossip.Body for the global channel
type ChatMessage struct {
	Text      string `json:"text"`
	From      string `json:"from"`
	Timestamp int64  `json:"timestamp"`
}

// Identify is sent by each peer after the transport is secured.
// It tells the remote side "who I am" at the app level.
type Identify = wire.Identify

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
	Nonce      []byte `json:"nonce"`
	Ciphertext []byte `json:"ciphertext"`
}

// NatRegister is sent from a client to a SeedNode to say:
// "I'm online as this user; please be able to route to me."
type NatRegister = wire.NatRegister

// NatRelay is sent from a client to a SeedNode, and the SeedNode forwards it
// to the target user if they're registered and connected.
type NatRelay = wire.NatRelay

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
