package proto

import (
	"github.com/TylerPetri/p2p-networking-project/internal/gameproto"
	"github.com/TylerPetri/p2p-networking-project/internal/wire"
)

// ApplicationProtocol is the legacy Park wire version retained for compatibility
// and adversarial tests. Current Park uses the public engine protocol.
const ApplicationProtocol = "park-p2p/0.2.0"

type MessageType = wire.MessageType

const (
	MsgHello       MessageType = "hello"
	MsgPeerList    MessageType = "peer_list"
	MsgGossip      MessageType = "gossip"
	MsgIdentify    MessageType = "identify"
	MsgNatRegister MessageType = "nat_register"
	MsgNatRelay    MessageType = "nat_relay"
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
type EncryptedMessage = gameproto.EncryptedMessage
