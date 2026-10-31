package engine_test

import (
	"context"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"p2p-park/engine"
)

func TestJoinAttemptLifetimeAndFailureDoNotChangeSessionTrust(t *testing.T) {
	id := network(t)
	a := node(t, id, "compatibility", nil)
	b := node(t, id, "compatibility", nil)
	invitation, err := a.Invitation(nil, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	attempt, cancel := context.WithTimeout(context.Background(), time.Second)
	result, err := b.Join(attempt, invitation, loopbackInvites)
	if err != nil || len(result.Connected) != 1 {
		t.Fatal(result, err)
	}
	cancel() // completed session belongs to Start, not the invitation attempt
	bad := invitation
	bad.Bootstraps = append([]engine.BootstrapPeer(nil), invitation.Bootstraps...)
	bad.Bootstraps[0].SigningID = strings.Repeat("a", 64)
	if _, err = b.Join(context.Background(), bad, loopbackInvites); !errors.Is(err, engine.ErrBootstrapUnavailable) {
		t.Fatal("wrong signer did not fail closed", err)
	}
	if _, err = b.Join(context.Background(), invitation, loopbackInvites); err != nil {
		t.Fatal("failed Join changed trust/session", err)
	}
	if err = b.Send(context.Background(), a.PeerID(), "still-live", []byte("compatible")); err != nil {
		t.Fatal(err)
	}
	message := receive(t, a)
	if string(message.Data) != "compatible" || hex.EncodeToString(message.SigningKey) != b.SigningID() {
		t.Fatal("session did not survive attempt cancellation")
	}
}
