package p2p

import (
	"encoding/hex"
	"errors"
)

var ErrAdmissionDenied = errors.New("p2p: identity is not approved")

// Restricted trust pins both key identities: an Ed25519 signature alone does
// not prove possession of the Noise private key named in a relayed envelope.
func (n *Node) admitted(id, signer string) bool {
	if !n.cfg.Restricted {
		return true
	}
	if id == n.ID() {
		return signer == hex.EncodeToString(n.id.SignPub)
	}
	return signer != "" && n.approved[id] == signer
}
