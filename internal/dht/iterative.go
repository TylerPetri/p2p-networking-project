package dht

import (
	"net"
	"sort"
	"time"

	"p2p-park/internal/proto"
)

type LookupConfig struct {
	Alpha      int
	K          int
	RPCTimeout time.Duration
	MaxRounds  int
}

func DefaultLookupConfig() LookupConfig {
	return LookupConfig{
		Alpha:      3,
		K:          20,
		RPCTimeout: 1200 * time.Millisecond,
		MaxRounds:  32,
	}
}

// IterativeFindNode performs a Kademlia-style iterative lookup.
func (d *DHT) IterativeFindNode(n Sender, targetHex string, cfg LookupConfig) ([]proto.DHTNode, error) {
	if cfg.Alpha > 3 {
		cfg.Alpha = 3
	}
	if cfg.K > 20 {
		cfg.K = 20
	}
	if cfg.MaxRounds > 32 {
		cfg.MaxRounds = 32
	}
	if cfg.Alpha <= 0 {
		cfg.Alpha = 3
	}
	if cfg.K <= 0 {
		cfg.K = 20
	}
	if cfg.RPCTimeout <= 0 {
		cfg.RPCTimeout = 1200 * time.Millisecond
	}
	if cfg.MaxRounds <= 0 {
		cfg.MaxRounds = 32
	}

	target, err := ParseNodeIDHex(targetHex)
	if err != nil {
		return nil, err
	}

	start := time.Now()
	queries := 0
	ok := false
	defer func() { d.metrics.ObserveLookup("FIND_NODE", queries, time.Since(start), ok) }()

	const (
		stUnqueried = iota
		stQuerying
		stDone
		stFailed
	)

	type cand struct {
		node  proto.DHTNode
		id    NodeID
		dist  NodeID
		state int
	}

	seen := map[string]*cand{} // keyed by NodeID hex

	seed := d.rt.Closest(target, cfg.K)
	for _, ni := range seed {
		c := &cand{
			node:  proto.DHTNode{NodeID: ni.NodeIDHex, PeerID: ni.PeerID, Addr: ni.Addr, Name: ni.Name},
			id:    ni.NodeID,
			dist:  Distance(ni.NodeID, target),
			state: stUnqueried,
		}
		seen[c.node.NodeID] = c
	}

	sorted := func() []*cand {
		out := make([]*cand, 0, len(seen))
		for _, c := range seen {
			out = append(out, c)
		}
		sort.Slice(out, func(i, j int) bool {
			return DistanceLess(out[i].dist, out[j].dist)
		})
		return out
	}

	for round := 0; round < cfg.MaxRounds; round++ {
		cands := sorted()
		if len(cands) == 0 {
			return nil, nil
		}

		// Pick up to alpha unqueried among closest window (K*2 prevents stalls).
		limit := len(cands)
		if limit > cfg.K*2 {
			limit = cfg.K * 2
		}
		toQuery := make([]*cand, 0, cfg.Alpha)
		for i := 0; i < limit && len(toQuery) < cfg.Alpha; i++ {
			if cands[i].state == stUnqueried {
				cands[i].state = stQuerying
				toQuery = append(toQuery, cands[i])
			}
		}

		if len(toQuery) == 0 {
			limit2 := len(cands)
			if limit2 > cfg.K {
				limit2 = cfg.K
			}
			left := false
			for i := 0; i < limit2; i++ {
				if cands[i].state == stUnqueried {
					left = true
					break
				}
			}
			if !left {
				break
			}
			continue
		}

		type result struct {
			c   *cand
			w   proto.DHTWire
			err error
		}
		queries += len(toQuery)
		resCh := make(chan result, len(toQuery))

		for _, c := range toQuery {
			peerID := c.node.PeerID
			go func(c *cand, pid string) {
				resp, err := d.QueryFindNode(n, pid, targetHex, cfg.RPCTimeout)
				resCh <- result{c: c, w: resp, err: err}
			}(c, peerID)
		}

		for i := 0; i < len(toQuery); i++ {
			r := <-resCh
			if r.err != nil || r.w.Kind != "NODES" {
				r.c.state = stFailed
				continue
			}
			r.c.state = stDone

			// Cap ingestion to mitigate poisoning.
			nodes := r.w.Nodes
			if len(nodes) > cfg.K*2 {
				nodes = nodes[:cfg.K*2]
			}

			for _, nd := range nodes {
				if len(nd.NodeID) != 64 || len(nd.PeerID) != 64 || len(nd.Addr) > 128 || len(nd.Name) > 256 || nd.Addr == "" {
					continue
				}
				if !NodeIDMatchesPeerID(nd.NodeID, nd.PeerID) {
					continue
				}
				if _, _, err := net.SplitHostPort(nd.Addr); err != nil {
					continue
				}
				if _, ok := seen[nd.NodeID]; ok {
					continue
				}

				id, err := ParseNodeIDHex(nd.NodeID)
				if err != nil {
					continue
				}

				d.rt.UpsertWithEviction(id, nd.PeerID, nd.Addr, nd.Name, func(tail NodeInfo) bool {
					resp, err := d.QueryPing(n, tail.PeerID, 800*time.Millisecond)
					return err == nil && resp.Kind == "PONG"
				})

				seen[nd.NodeID] = &cand{node: nd, id: id, dist: Distance(id, target), state: stUnqueried}
			}
		}

		// Bound candidate set.
		cands = sorted()
		if len(cands) > cfg.K*8 {
			cands = cands[:cfg.K*8]
			keep := map[string]*cand{}
			for _, c := range cands {
				keep[c.node.NodeID] = c
			}
			seen = keep
		}
	}

	cands := sorted()
	if len(cands) > cfg.K {
		cands = cands[:cfg.K]
	}
	out := make([]proto.DHTNode, 0, len(cands))
	for _, c := range cands {
		out = append(out, c.node)
	}
	ok = true
	return out, nil
}
