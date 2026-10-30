package discovery

import (
	"net"
	"testing"
)

func TestLANAdvertisementUsesSenderAddress(t *testing.T) {
	sender := &net.UDPAddr{IP: net.ParseIP("192.0.2.1"), Port: 1}
	for _, advertised := range []string{":4000", "127.0.0.1:4000", "metadata.example:4000", "[::1]:4000"} {
		if got := normalizeListenFromPong(sender, advertised); got != "192.0.2.1:4000" {
			t.Fatalf("untrusted host used: %q", got)
		}
	}
	for _, bad := range []string{"", ":0", ":-1", ":65536", ":http", "host"} {
		if got := normalizeListenFromPong(sender, bad); got != "" {
			t.Fatalf("accepted %q", bad)
		}
	}
}
