package discovery

import (
	"fmt"
	"time"

	"p2p-park/internal/netx"
	"p2p-park/internal/p2p"
)

type Manager struct {
	Store       *PeerStore
	LANConfig   LANConfig
	Seeds       []string
	Interval    time.Duration
	MaxFailures int
}

// NewManager constructs a DiscoveryManager with sane defaults.
func NewManager(store *PeerStore, lanCfg LANConfig) *Manager {
	return &Manager{
		Store:       store,
		LANConfig:   lanCfg,
		Seeds:       DefaultSeeds,
		Interval:    30 * time.Second,
		MaxFailures: 5,
	}
}

// Run starts the periodic discovery loop.
func (m *Manager) Run(n *p2p.Node) { _ = m.RunManaged(n) }

// RunManaged returns completion for owners that must join discovery on shutdown.
func (m *Manager) RunManaged(n *p2p.Node) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		select {
		case <-n.Done():
			return
		case <-time.After(500 * time.Millisecond):
		}

		for {
			// 1) LAN
			listenAddr := n.ListenAddr()
			name := n.Name()
			addrs, err := DiscoverLANPeers(m.LANConfig, string(listenAddr), name)
			if err != nil {
				fmt.Printf("[DISCOVERY] LAN error: %v\n", err)
			}

			if len(addrs) == 0 {
				fmt.Println("[DISCOVERY] no peers found on LAN")
			} else {
				fmt.Printf("[DISCOVERY] found %d peers: %v\n", len(addrs), addrs)
			}
			// 2) PeerStore
			if m.Store != nil {
				addrs = append(addrs, m.Store.Candidates(m.MaxFailures)...)
			}
			// 3) Seeds
			addrs = append(addrs, m.Seeds...)

			seen := make(map[string]struct{})
			for i, a := range addrs {
				select {
				case <-n.Done():
					return
				default:
				}
				if i >= 16 {
					break
				}
				if a == "" || a == string(listenAddr) {
					continue
				}
				if _, ok := seen[a]; ok {
					continue
				}
				seen[a] = struct{}{}

				func(addr string) {
					if err := n.ConnectDiscovered(netx.Addr(addr), ""); err != nil {
						if n.Debug() {
							fmt.Printf("[DISCOVERY] connect to %s failed: %v\n", addr, err)
						}
						if m.Store != nil {
							m.Store.NoteFailure(addr)
						}
						return
					}
					if m.Store != nil {
						m.Store.NoteSuccess(addr)
					}
				}(a)
			}

			select {
			case <-n.Done():
				return
			case <-time.After(m.Interval):
			}
		}
	}()
	return done
}
