package p2p

import (
	"fmt"
	"os"
	"testing"
)

// Concurrent go test processes must not discover one another through a shared
// persisted peer inventory or modify the developer's application state.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "p2p-test-state-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := os.Setenv("P2P_PARK_DATA_DIR", dir); err != nil {
		_ = os.RemoveAll(dir)
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}
