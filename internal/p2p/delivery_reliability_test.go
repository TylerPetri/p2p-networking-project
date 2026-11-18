package p2p

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/TylerPetri/p2p-networking-project/internal/wire"
)

// A writer withheld by the fixture makes enqueue pressure independent of OS TCP
// buffer sizes, scheduling and arbitrary sleeps. No network worker is spawned.
func deliveryPeer(t *testing.T, n *Node, id string, capacity int) *peer {
	t.Helper()
	conn, other := net.Pipe()
	t.Cleanup(func() { _ = conn.Close(); _ = other.Close() })
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	p := &peer{id: id, ctx: ctx, cancel: cancel, conn: conn, sendCh: make(chan wire.Envelope, capacity)}
	n.peers[id] = p
	return p
}
func TestDeliveryFanoutIsolatesFullPeerQueue(t *testing.T) {
	n := securityNode(t)
	slow := deliveryPeer(t, n, "slow", 1)
	healthy := deliveryPeer(t, n, "healthy", 1)
	slow.sendCh <- wire.Envelope{Type: wire.MsgIdentify}
	err := n.PublishGossip(wire.Gossip{ID: "fanout", Channel: "engine/delivery", Body: []byte(`{}`)})
	if !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("expected explicit partial fanout error, got %v", err)
	}
	if len(healthy.sendCh) != 1 {
		t.Fatal("slow peer prevented healthy enqueue")
	}
	if _, ok := n.peers[slow.id]; ok {
		t.Fatal("full peer not disconnected")
	}
	select {
	case <-slow.ctx.Done():
	default:
		t.Fatal("slow peer context not cancelled")
	}
	t.Log("DELIVERY fanout_error=resource_limit healthy_enqueued=1 slow_enqueued=0 slow_disconnected=true; enqueue is not receipt")
}
func TestDeliveryDisconnectAfterSuccessfulEnqueue(t *testing.T) {
	n := securityNode(t)
	remote := securityNode(t)
	p := deliveryPeer(t, n, remote.ID(), 1)
	err := n.SendGossip(p.id, wire.Gossip{ID: "unknown-outcome", Channel: "engine/delivery", Body: []byte(`{}`)})
	if err != nil || len(p.sendCh) != 1 {
		t.Fatal("message was not enqueued", err)
	}
	n.removePeer(p.id, p)
	if err = n.SendGossip(p.id, wire.Gossip{ID: "later", Channel: "engine/delivery", Body: []byte(`{}`)}); !errors.Is(err, ErrPeerClosed) {
		t.Fatal(err)
	}
	if len(p.sendCh) != 1 {
		t.Fatal("fixture unexpectedly wrote the queued message")
	}
	t.Log("DELIVERY first_send_nil=true disconnected_after_enqueue=true receiver_delivery=0 next_send=peer_closed")
}

func TestDeliveryCoreWireVersionMatrix(t *testing.T) {
	const current = "p2p-engine/0.1.0"
	for _, version := range []string{current, "", "park-p2p/0.2.0", "p2p-engine/0.2.0"} {
		t.Run("version="+version, func(t *testing.T) {
			local, remote := securityNode(t), securityNode(t)
			local.cfg.NetworkID = local.ID()
			remote.cfg.NetworkID = local.cfg.NetworkID
			local.cfg.Protocol = current
			remote.cfg.Protocol = version
			local.cfg.BindAddr = "127.0.0.1:0"
			remote.cfg.BindAddr = "127.0.0.1:0"
			if err := local.Start(); err != nil {
				t.Fatal(err)
			}
			if err := remote.Start(); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			err := local.ConnectContext(ctx, remote.ListenAddr())
			if version == current {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil {
				t.Fatal("incompatible peer admitted")
			} else if len(local.SnapshotPeers()) != 0 || len(remote.SnapshotPeers()) != 0 {
				t.Fatal("incompatible peer leaked into admitted snapshot")
			}
			t.Logf("COMPAT local=%q remote=%q connect_error=%v", current, version, err)
		})
	}
}
