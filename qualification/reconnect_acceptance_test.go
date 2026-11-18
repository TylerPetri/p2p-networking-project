package qualification_test

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/TylerPetri/p2p-networking-project/engine"
)

// This is an opt-in acceptance requirement, not an ordinary current-behavior
// assertion. Discovery and a manual invitation may race; the existing API does
// not promise seamless duplicate-dial settlement. Record any actual failure,
// without retrying it into success or putting a scheduling-sensitive gate in CI.
func TestQualificationConcurrentDiscoveryJoinStability(t *testing.T) {
	if os.Getenv("P2P_QUALIFY") != "1" {
		t.Skip("explicit gate: python3 scripts/qualify.py --gate reconnect --soak-rounds 100")
	}
	trials := 3
	if value := os.Getenv("P2P_SOAK_ROUNDS"); value != "" {
		n, err := strconv.Atoi(value)
		if err != nil || n < 1 || n > 1000 {
			t.Fatal("P2P_SOAK_ROUNDS must be 1..1000")
		}
		trials = n
	}
	failed := 0
	for round := 0; round < trials; round++ {
		if !t.Run(strconv.Itoa(round), func(t *testing.T) {
			network := networkID(t)
			authority := newNode(t, network, nil)
			seed := newNode(t, network, nil)
			startRoom(t, authority, filepath.Join(t.TempDir(), "room.json"), authority.SigningID(), nil, func(checkpoint) {})
			join(t, authority, seed)
			waitUntil(t, func() bool { return !noPeer(seed, authority.PeerID()) })
			peer := newNode(t, network, nil)
			join(t, peer, seed)
			// Deliberately overlap manual joining with peer-list discovery; unlike the
			// capacity setup there is no settled direct-session/application barrier.
			invitation, err := authority.Invitation(nil, time.Time{})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if _, err = peer.Join(ctx, invitation, engine.JoinOptions{AllowedCIDRs: []string{"127.0.0.1/32"}}); err != nil {
				t.Fatal("concurrent Join outcome", err)
			}
			_ = seed.Close()
			c := client(t, peer, authority.SigningID(), authority.PeerID(), filepath.Join(t.TempDir(), "client.json"))
			if err = c.send(ctx, request{Kind: "sync"}); err != nil {
				t.Fatal("post-seed-loss enqueue", err)
			}
			r, err := c.receive(ctx)
			if err != nil || r.Kind != "delta" || r.Seq != 0 {
				t.Fatal("post-seed-loss authenticated recovery", r, err)
			}
		}) {
			failed++
		}
	}
	t.Logf("RECONNECT concurrent_discovery_manual_join_trials=%d failed_trials=%d threshold=zero_failed_trials retry_masking=false; this bounded schedule is not a universal guarantee", trials, failed)
}
