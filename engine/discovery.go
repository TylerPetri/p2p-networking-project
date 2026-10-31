package engine

import "p2p-park/internal/discovery"

// Optional LAN workers reuse the existing discovery implementation. Untrusted
// advertisements still enter the core's scoped dialing and admission path.
func (e *Engine) startLAN() {
	if !e.lan {
		return
	}
	cfg := discovery.DefaultLANConfig()
	done, err := discovery.StartLANResponderManaged(e.node.Done(), cfg, string(e.node.ListenAddr()), e.node.Name())
	if err != nil {
		e.events <- Event{Type: EventDiscoveryError, Error: err.Error()}
	} else {
		e.wg.Add(1)
		go func() { defer e.wg.Done(); <-done }()
	}
	var store *discovery.PeerStore
	if e.lanStorePath != "" {
		store = discovery.NewPeerStore(e.lanStorePath)
	}
	manager := discovery.NewManager(store, cfg)
	finished := manager.RunManaged(e.node)
	e.wg.Add(1)
	go func() { defer e.wg.Done(); <-finished }()
}
