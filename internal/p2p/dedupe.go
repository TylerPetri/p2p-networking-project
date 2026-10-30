package p2p

import (
	"sync"
	"time"
)

type seenCache struct {
	mu        sync.Mutex
	ttl       time.Duration
	items     map[string]time.Time
	nextSweep time.Time
}

func newSeenCache(ttl time.Duration) *seenCache {
	return &seenCache{
		ttl:   ttl,
		items: make(map[string]time.Time),
	}
}

// Seen returns true if id was seen recently. If not, it records it and returns false.
func (s *seenCache) Seen(id string) bool {
	if id == "" || len(id) > 256 {
		return true
	}

	now := time.Now()

	s.mu.Lock()
	defer s.mu.Unlock()

	if now.After(s.nextSweep) {
		for k, t := range s.items {
			if now.Sub(t) > s.ttl {
				delete(s.items, k)
			}
		}
		s.nextSweep = now.Add(time.Second)
	}
	if t, ok := s.items[id]; ok && now.Sub(t) <= s.ttl {
		return true
	}
	// Fail closed when saturated rather than evicting live replay entries.
	if len(s.items) >= 8192 {
		return true
	}
	s.items[id] = now
	return false
}
