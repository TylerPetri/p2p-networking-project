package discovery

import (
	"fmt"
	"os"
	"testing"
)

// Isolate persistent hints from other test processes and real application state.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "discovery-test-state-")
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
