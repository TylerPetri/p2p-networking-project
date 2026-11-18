package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"testing"
	"time"
)

func reliabilityWait(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for !condition() {
		select {
		case <-tick.C:
		case <-deadline.C:
			t.Fatal("synchronization deadline exceeded")
		}
	}
}
func reliabilityNode(t *testing.T, network string, delayedPump bool) *Engine {
	t.Helper()
	e, err := New(Config{NetworkID: network, Application: "delivery-regression", Logger: log.New(io.Discard, "", 0)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.Close() })
	if delayedPump {
		// Test-only schedule control: run the real transport before the SDK pump.
		// No production hook/API or queue sizing change is needed.
		if err = e.node.Start(); err != nil {
			t.Fatal(err)
		}
		e.state = 1
	} else if err = e.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	return e
}

type deliveryObservation struct {
	SenderNil, OverflowReceived, SubsequentReceived bool
	Primed, Retained                                int
}

func observeStalledConsumer(t *testing.T) deliveryObservation {
	t.Helper()
	network, err := NewNetworkID()
	if err != nil {
		t.Fatal(err)
	}
	receiver := reliabilityNode(t, network, true)
	sender := reliabilityNode(t, network, false)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err = sender.Connect(ctx, receiver.Address()); err != nil {
		t.Fatal(err)
	}
	// Prime the actual public receive queue to represent a stalled consumer. These
	// sentinels are test data, not claimed as messages sent through the network.
	capacity := cap(receiver.messages)
	for i := 0; i < capacity; i++ {
		receiver.messages <- Message{Topic: "primed", Data: []byte(fmt.Sprint(i))}
	}
	err = sender.Send(ctx, receiver.PeerID(), "overflow", []byte("critical"))
	observation := deliveryObservation{SenderNil: err == nil, Primed: capacity}
	if err != nil {
		t.Fatalf("overflow enqueue failed unexpectedly: %v", err)
	}
	// Real signed/encrypted input has arrived, while the pump is held back.
	reliabilityWait(t, func() bool { return len(receiver.node.Incoming()) == 1 })
	receiver.wg.Add(1)
	go receiver.pump()
	reliabilityWait(t, func() bool { return len(receiver.node.Incoming()) == 0 })
	// A newly generated peer event is a pump barrier: it cannot be forwarded until
	// the already-dequeued overflow message's delivery case has finished. Merely
	// seeing Incoming empty would race the pump's output select.
	barrier := reliabilityNode(t, network, false)
	if err = barrier.Connect(ctx, receiver.Address()); err != nil {
		t.Fatal(err)
	}
	for {
		select {
		case event := <-receiver.Events():
			if event.Type == EventPeerConnected && event.Peer.ID == barrier.PeerID() {
				goto processed
			}
		case <-ctx.Done():
			t.Fatal("pump barrier: ", ctx.Err())
		}
	}
processed:
	for len(receiver.messages) > 0 {
		m := <-receiver.messages
		observation.Retained++
		if m.Topic == "overflow" {
			observation.OverflowReceived = true
		}
	}
	if err = sender.Send(ctx, receiver.PeerID(), "subsequent", []byte("still-live")); err != nil {
		t.Fatal(err)
	}
	select {
	case m := <-receiver.Messages():
		observation.SubsequentReceived = m.Topic == "subsequent" && string(m.Data) == "still-live"
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	t.Logf("DELIVERY sender_nil=%t overflow_received=%t primed=%d retained=%d subsequent_received=%t", observation.SenderNil, observation.OverflowReceived, observation.Primed, observation.Retained, observation.SubsequentReceived)
	return observation
}

// A passing behavior test records the current loss, NOT a reliable-delivery pass.
func TestSendStalledConsumerCurrentBehavior(t *testing.T) {
	got := observeStalledConsumer(t)
	if !got.SenderNil || got.OverflowReceived || got.Retained != got.Primed || !got.SubsequentReceived {
		t.Fatalf("best-effort behavior changed; revisit the release gate and contract: %+v", got)
	}
}

// Opt-in acceptance gate: require explicit receiver-pressure feedback or receipt.
// It intentionally FAILS on the current engine; it never converts loss to PASS.
func TestQualificationReceiverPressureFeedback(t *testing.T) {
	if os.Getenv("P2P_QUALIFY") != "1" {
		t.Skip("explicit gate: python3 scripts/qualify.py --gate delivery")
	}
	got := observeStalledConsumer(t)
	if got.SenderNil && !got.OverflowReceived {
		t.Fatal("FAIL receiver-pressure feedback: Send returned nil, receiver omitted the message; sender cannot distinguish receipt from receiver queue loss")
	}
}

func TestPublishZeroPeersCurrentBehavior(t *testing.T) {
	network, err := NewNetworkID()
	if err != nil {
		t.Fatal(err)
	}
	n := reliabilityNode(t, network, false)
	if len(n.Peers()) != 0 {
		t.Fatal("unexpected peers")
	}
	if err = n.Publish(context.Background(), "room-action", []byte("no recipients")); err != nil {
		t.Fatal(err)
	}
	if err = n.Send(context.Background(), n.PeerID(), "room-action", nil); !errors.Is(err, ErrPeerClosed) {
		t.Fatal("zero-peer Send", err)
	}
	t.Log("DELIVERY publish_nil=true connected_peers=0 delivered=0 send_to_absent_peer=peer_closed; no receipt guarantee")
}
