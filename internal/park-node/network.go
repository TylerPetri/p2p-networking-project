package parknode

import (
	"context"
	"github.com/TylerPetri/p2p-networking-project/engine"
)

// parkNetwork is the application's only transport adapter. It handles application
// topic selection and display name lookup; it never constructs wire envelopes.
type parkNetwork struct {
	engine *engine.Engine
}

func (n *parkNetwork) publish(topic string, body []byte) error {
	return n.engine.Publish(context.Background(), topic, body)
}
func (n *parkNetwork) displayName(signer string) string {
	for _, p := range n.engine.Peers() {
		if p.SigningID == signer && p.Name != "" {
			return p.Name
		}
	}
	return shortID(signer)
}
