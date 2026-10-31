package parknode

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"p2p-park/engine"
)

// parkNetwork is the application's only transport adapter. It handles game
// topic selection and safe reply/name lookup; it never constructs wire envelopes.
type parkNetwork struct {
	engine *engine.Engine
}

func (n *parkNetwork) publish(topic string, body []byte) error {
	return n.engine.Publish(context.Background(), topic, body)
}
func (n *parkNetwork) send(peer, topic string, body []byte) error {
	return n.engine.Send(context.Background(), peer, topic, body)
}
func (n *parkNetwork) reply(m engine.Message, topic string, body []byte) error {
	signer := hex.EncodeToString(m.SigningKey)
	for _, p := range n.engine.Peers() {
		if p.SigningID == signer {
			return n.send(p.ID, topic, body)
		}
	}
	return engine.ErrPeerClosed
}
func (n *parkNetwork) displayName(signer string) string {
	for _, p := range n.engine.Peers() {
		if p.SigningID == signer && p.Name != "" {
			return p.Name
		}
	}
	return shortID(signer)
}
func (n *parkNetwork) broadcast(topic string, value any) {
	b, err := json.Marshal(value)
	if err == nil {
		_ = n.publish(topic, b)
	}
}
