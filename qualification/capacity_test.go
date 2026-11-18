package qualification_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/TylerPetri/p2p-networking-project/engine"
)

type resources struct {
	Goroutines int
	HeapBytes  uint64
	RSSKiB     *uint64
}

func sampleResources() resources {
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	result := resources{Goroutines: runtime.NumGoroutine(), HeapBytes: memory.HeapAlloc}
	// Linux-only current RSS; other platforms explicitly have no RSS sample.
	if raw, err := os.ReadFile("/proc/self/status"); err == nil {
		for _, line := range strings.Split(string(raw), "\n") {
			if fields := strings.Fields(line); len(fields) == 3 && fields[0] == "VmRSS:" {
				if rss, err := strconv.ParseUint(fields[1], 10, 64); err == nil {
					result.RSSKiB = &rss
				}
			}
		}
	}
	return result
}
func waitUntil(t *testing.T, condition func() bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for !condition() {
		select {
		case <-tick.C:
		case <-ctx.Done():
			t.Fatal("condition deadline", ctx.Err())
		}
	}
}
func noPeer(n *engine.Engine, id string) bool {
	for _, p := range n.Peers() {
		if p.ID == id {
			return false
		}
	}
	return true
}

func TestBoundedChurnRecovery(t *testing.T) {
	rounds := 3
	if value := os.Getenv("P2P_SOAK_ROUNDS"); value != "" {
		n, err := strconv.Atoi(value)
		if err != nil || n < 1 || n > 1000 {
			t.Fatal("P2P_SOAK_ROUNDS must be 1..1000")
		}
		rounds = n
	}
	suite := t
	before := sampleResources()
	network := networkID(t)
	dir := t.TempDir()
	identity, err := engine.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	authorityIdentity, err := engine.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	// Reserve two localhost ports and assign canonical first to an EOF service,
	// second to a real engine. First service is TCP reachable but bootstrap-unusable;
	// no arbitrary sleeps, privileged ports or external destinations are needed.
	first, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	second, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		_ = first.Close()
		t.Fatal(err)
	}
	if first.Addr().String() > second.Addr().String() {
		first, second = second, first
	}
	healthyAddress := second.Addr().String()
	_ = second.Close()
	var refused atomic.Int64
	rejectDone := make(chan struct{})
	go func() {
		defer close(rejectDone)
		for {
			conn, err := first.Accept()
			if err != nil {
				return
			}
			refused.Add(1)
			_ = conn.Close()
		}
	}()
	t.Cleanup(func() { _ = first.Close(); <-rejectDone })
	newAuthority := func() *engine.Engine {
		n, err := engine.New(engine.Config{NetworkID: network, Application: "qualification-room", Identity: authorityIdentity, Bind: healthyAddress, Logger: log.New(io.Discard, "", 0)})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = n.Close() })
		if err = n.Start(context.Background()); err != nil {
			t.Fatal(err)
		}
		return n
	}
	authority := newAuthority()
	room := startRoom(t, authority, filepath.Join(dir, "room.json"), identity.SigningID(), nil, func(checkpoint) {})
	seed := newNode(t, network, nil)
	join(t, authority, seed)
	waitUntil(t, func() bool { return !noPeer(seed, authority.PeerID()) })
	initial := newNode(t, network, nil)
	join(t, initial, seed)
	// Discovery is already establishing this link; do not race it with a
	// redundant manual dial. Prove bidirectional admission and an application
	// response before measuring loss of a different peer (the seed).
	waitUntil(t, func() bool { return !noPeer(initial, authority.PeerID()) && !noPeer(authority, initial.PeerID()) })
	initialClient := client(t, initial, authority.SigningID(), authority.PeerID(), filepath.Join(dir, "initial.json"))
	if r := initialClient.exchange(t, request{Kind: "sync"}); r.Kind != "delta" {
		t.Fatal(r)
	}
	_ = seed.Close()
	waitUntil(t, func() bool { return noPeer(initial, seed.PeerID()) && noPeer(authority, seed.PeerID()) })
	// Loss of seed must not stop verified traffic to a surviving connected peer.
	if r := initialClient.exchange(t, request{Kind: "sync"}); r.Kind != "delta" {
		t.Fatal(r)
	}
	_ = initial.Close()
	waitUntil(t, func() bool { return noPeer(authority, initial.PeerID()) })
	var joins, recoveries []float64
	accepted, delivered, explicitFailures := 0, 0, 0
	peak := sampleResources()
	for round := 0; round < rounds; round++ {
		t.Run(fmt.Sprint(round), func(t *testing.T) {
			n := newNode(t, network, identity)
			inv, err := authority.Invitation(nil, time.Time{})
			if err != nil {
				t.Fatal(err)
			}
			fake := inv.Bootstraps[0]
			fake.Address = first.Addr().String()
			inv.Bootstraps = append([]engine.BootstrapPeer{fake}, inv.Bootstraps...)
			started := time.Now()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			result, err := n.Join(ctx, inv, engine.JoinOptions{AllowedCIDRs: []string{"127.0.0.1/32"}})
			cancel()
			if err != nil || len(result.Failures) != 1 || len(result.Connected) != 1 {
				t.Fatal("fallback", result, err)
			}
			joins = append(joins, float64(time.Since(started).Microseconds())/1000)
			explicitFailures += len(result.Failures)
			c := client(t, n, authority.SigningID(), authority.PeerID(), filepath.Join(dir, fmt.Sprintf("client-%d.json", round)))
			if r := c.exchange(t, request{Kind: "sync"}); r.Kind != "delta" && r.Kind != "snapshot" {
				t.Fatal(r)
			}
			action := action{ID: fmt.Sprint("round-", round), Expected: c.state.Seq, Value: round + 1}
			if r := c.exchange(t, request{Kind: "action", Action: action}); r.Kind != "applied" {
				t.Fatal(r)
			}
			accepted++
			delivered++ // application acknowledgements, not just enqueue nil
			started = time.Now()
			c.current = false
			if round == rounds/2 {
				// Abrupt loss of the active engine; reconstruct authority from disk with
				// the same identity/address and application ledger, not a new empty room.
				room.close()
				authority = newAuthority()
				room = startRoom(suite, authority, filepath.Join(dir, "room.json"), identity.SigningID(), nil, func(checkpoint) {})
			} else {
				if err = n.Disconnect(authority.PeerID()); err != nil {
					t.Fatal(err)
				}
			}
			waitUntil(t, func() bool { return noPeer(n, authority.PeerID()) && noPeer(authority, n.PeerID()) })
			join(t, n, authority)
			r := c.exchange(t, request{Kind: "sync", After: c.state.Seq})
			if r.Seq != uint64(round+1) || r.Value != round+1 || !c.current {
				t.Fatal("recovery diverged", r)
			}
			recoveries = append(recoveries, float64(time.Since(started).Microseconds())/1000)
			sample := sampleResources()
			if sample.Goroutines > peak.Goroutines {
				peak.Goroutines = sample.Goroutines
			}
			if sample.HeapBytes > peak.HeapBytes {
				peak.HeapBytes = sample.HeapBytes
			}
			if sample.RSSKiB != nil && (peak.RSSKiB == nil || *sample.RSSKiB > *peak.RSSKiB) {
				peak.RSSKiB = sample.RSSKiB
			}
			address := n.Address()
			_ = n.Close()
			listener, err := net.Listen("tcp", address)
			if err != nil {
				t.Fatal("client listener leak", err)
			}
			_ = listener.Close()
			waitUntil(t, func() bool { return noPeer(authority, n.PeerID()) })
		})
	}
	room.close()
	_ = first.Close()
	<-rejectDone
	waitUntil(t, func() bool { return runtime.NumGoroutine() <= before.Goroutines+8 })
	runtime.GC()
	after := sampleResources()
	sort.Float64s(joins)
	sort.Float64s(recoveries)
	report := map[string]any{"rounds": rounds, "join_ms_p50": joins[len(joins)/2], "join_ms_max": joins[len(joins)-1], "recovery_ms_p50": recoveries[len(recoveries)/2], "recovery_ms_max": recoveries[len(recoveries)-1], "actions_accepted": accepted, "actions_delivered_acknowledged": delivered, "bootstrap_explicit_failures": explicitFailures, "unusable_service_connections": refused.Load(), "queue_drops": "not exposed by public API; see deterministic pressure probe", "resources_before": before, "resources_peak_sampled": peak, "resources_after_shutdown": after, "resource_scope": "one test process; sampled, not peak RSS or a hard memory bound", "seed_loss": "surviving peer sync succeeded"}
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	t.Log("METRICS " + string(data))
}
