package p2p

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	proto "p2p-park/internal/wire"
)

const gossipLifetime = 5 * time.Minute
const gossipFutureSkew = 30 * time.Second

// Standard Ed25519 signs a domain-separated, fixed-field JSON representation.
// The identity binding is also verified so a relayer cannot choose another
// network ID for its own signing key. Names are never trust anchors.
func gossipSigningBytes(env proto.Envelope) ([]byte, error) {
	env.Signature = nil
	b, err := json.Marshal(env)
	return append([]byte("p2p-park/gossip/v1\x00"), b...), err
}
func (n *Node) signGossip(env proto.Envelope) (proto.Envelope, error) {
	if env.FromID != "" && env.FromID != n.ID() {
		return env, errors.New("cannot sign another origin")
	}
	env.FromID = n.ID()
	env.NetworkID = n.cfg.NetworkID
	env.OriginPub = append([]byte(nil), n.id.SignPub...)
	env.OriginBinding = ed25519.Sign(n.id.SignPriv, identityBinding(n.id.NoisePub[:]))
	env.SentUnix = time.Now().Unix()
	b, err := gossipSigningBytes(env)
	if err != nil {
		return env, err
	}
	env.Signature = ed25519.Sign(n.id.SignPriv, b)
	return env, nil
}
func verifyGossip(env proto.Envelope, now time.Time) bool {
	if env.Type != proto.MsgGossip || !validPeerID(env.FromID) || len(env.OriginPub) != ed25519.PublicKeySize || len(env.Signature) != ed25519.SignatureSize {
		return false
	}
	if env.SentUnix < now.Add(-gossipLifetime).Unix() || env.SentUnix > now.Add(gossipFutureSkew).Unix() {
		return false
	}
	static, _ := hex.DecodeString(env.FromID)
	if !ed25519.Verify(ed25519.PublicKey(env.OriginPub), identityBinding(static), env.OriginBinding) {
		return false
	}
	b, err := gossipSigningBytes(env)
	return err == nil && ed25519.Verify(ed25519.PublicKey(env.OriginPub), b, env.Signature)
}
