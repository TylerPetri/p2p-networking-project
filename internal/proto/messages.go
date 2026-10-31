package proto

import (
	"p2p-park/internal/gameproto"
	"p2p-park/internal/wire"
)

// ApplicationProtocol is the legacy Park wire version retained for compatibility
// and adversarial tests. Current Park uses the public engine protocol.
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

type Identify = wire.Identify
type NatRegister = wire.NatRegister
type NatRelay = wire.NatRelay
type ChatMessage = gameproto.ChatMessage
type PointsSnapshot = gameproto.PointsSnapshot
type SignedPointsSnapshot = gameproto.SignedPointsSnapshot
type EncryptedMessage = gameproto.EncryptedMessage
type GrantSyncSummary = gameproto.GrantSyncSummary
type GrantSyncRequest = gameproto.GrantSyncRequest
type GrantSyncResponse = gameproto.GrantSyncResponse
type QuizWire = gameproto.QuizWire
type QuizOpen = gameproto.QuizOpen
type QuizOpenSigned = gameproto.QuizOpenSigned
type QuizAnswer = gameproto.QuizAnswer
type QuizGrant = gameproto.QuizGrant
type QuizResult = gameproto.QuizResult
