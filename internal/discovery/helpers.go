package discovery

import (
	"net"
	"strconv"
	"strings"
)

func listenPortOnly(listenAddr string) string {
	// returns ":12345"
	_, port, err := net.SplitHostPort(listenAddr)
	if err == nil && port != "" {
		return ":" + port
	}
	// try raw ":1234"
	if strings.HasPrefix(listenAddr, ":") {
		return listenAddr
	}
	return listenAddr
}

func normalizeListenFromPong(sender *net.UDPAddr, listen string) string {
	// A UDP advertisement may choose a port, never a third-party dial target.
	if sender == nil || sender.IP == nil {
		return ""
	}
	_, port, err := net.SplitHostPort(listen)
	if err != nil {
		return ""
	}
	n, err := strconv.Atoi(port)
	if err != nil || n <= 0 || n > 65535 {
		return ""
	}
	return net.JoinHostPort(sender.IP.String(), port)
}
