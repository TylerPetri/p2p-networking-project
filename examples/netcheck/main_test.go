package main

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"
)

func TestNoImplicitNonLoopbackExposure(t *testing.T) {
	for _, bind := range []string{"0.0.0.0:44001", "[::]:44001", "192.168.1.10:44001", "example.org:44001"} {
		err := run(context.Background(), config{state: t.TempDir(), bind: bind, duration: time.Second, probeTimeout: time.Second}, strings.NewReader("quit\n"), io.Discard)
		if err == nil {
			t.Fatal("accepted non-explicit local bind", bind)
		}
	}
}
func TestMalformedInviteBeforeNodeStartup(t *testing.T) {
	path := t.TempDir() + "/missing-invitation"
	err := run(context.Background(), config{state: t.TempDir(), bind: "127.0.0.1:0", invite: path, duration: time.Second, probeTimeout: time.Second}, strings.NewReader("quit\n"), io.Discard)
	if err == nil {
		t.Fatal("missing invitation accepted")
	}
}
