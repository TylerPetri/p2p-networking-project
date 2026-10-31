package p2p

import "errors"

var ErrAdmissionDenied = errors.New("p2p: identity is not approved")

func (n *Node) admitted(id string) bool { return !n.cfg.Restricted || id == n.ID() || n.approved[id] }
