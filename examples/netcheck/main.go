// netcheck is a bounded owner-run diagnostic consumer of the public API.
// It is not a relay, deployment service or general-purpose messaging application.
package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/netip"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"p2p-park/engine"
)

type config struct {
	state, bind, advertise, network, invite, allow, discovery string
	nonloopback                                               bool
	duration, probeTimeout                                    time.Duration
}
type pendingProbe struct {
	signer   string
	deadline time.Time
}
type probeBody struct {
	Nonce string `json:"nonce"`
}

func main() {
	var c config
	flag.StringVar(&c.state, "state", "", "required private node state directory; never share between live nodes")
	flag.StringVar(&c.bind, "bind", "127.0.0.1:0", "explicit numeric local TCP endpoint")
	flag.StringVar(&c.advertise, "advertise", "", "optional reachable numeric endpoint (no automatic NAT mapping)")
	flag.StringVar(&c.network, "network", "", "expected overlay ID; otherwise use saved state or invitation")
	flag.StringVar(&c.invite, "invite", "", "trusted invitation text file; no HTTP fetching")
	flag.StringVar(&c.allow, "allow-cidrs", "", "local approval for invitation private/loopback destinations")
	flag.StringVar(&c.discovery, "discover-cidrs", "", "additional approved numeric referral scopes")
	flag.BoolVar(&c.nonloopback, "allow-nonloopback", false, "owner explicitly authorizes binding this non-loopback host/port")
	flag.DurationVar(&c.duration, "duration", 10*time.Minute, "maximum diagnostic process lifetime")
	flag.DurationVar(&c.probeTimeout, "probe-timeout", 10*time.Second, "authenticated round-trip timeout (1s..30s)")
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, c, os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func split(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, ",")
}
func canonicalID(s string) bool {
	if len(s) != 64 {
		return false
	}
	b, e := hex.DecodeString(s)
	return e == nil && hex.EncodeToString(b) == s
}
func readText(path string, limit int) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, int64(limit+1)))
	if err != nil {
		return "", err
	}
	if len(b) > limit {
		return "", errors.New("diagnostic file exceeds size limit")
	}
	return strings.TrimSuffix(string(b), "\n"), nil
}
func run(parent context.Context, c config, input io.Reader, output io.Writer) error {
	endpoint, err := netip.ParseAddrPort(c.bind)
	if err != nil || endpoint.Addr().Zone() != "" || endpoint.Addr().IsUnspecified() {
		return errors.New("bind must be an explicit numeric host:port")
	}
	if !endpoint.Addr().IsLoopback() && !c.nonloopback {
		return errors.New("non-loopback binding requires explicit -allow-nonloopback owner authorization")
	}
	if c.state == "" || c.duration <= 0 || c.duration > time.Hour || c.probeTimeout < time.Second || c.probeTimeout > 30*time.Second {
		return errors.New("invalid state directory, duration (0..1h) or probe timeout (1s..30s)")
	}
	var imported *engine.Invitation
	if c.invite != "" {
		text, err := readText(c.invite, engine.MaxInvitationSize+1)
		if err != nil {
			return err
		}
		i, err := engine.ParseInvitation(text)
		if err != nil {
			return err
		}
		imported = &i
		if c.network != "" && c.network != i.NetworkID {
			return errors.New("invitation differs from expected network")
		}
		c.network = i.NetworkID
	}
	if err = os.MkdirAll(c.state, 0700); err != nil {
		return err
	}
	networkPath := filepath.Join(c.state, "network-id.txt")
	saved, err := readText(networkPath, 65)
	missingNetwork := errors.Is(err, os.ErrNotExist)
	if err == nil {
		if c.network != "" && c.network != saved {
			return errors.New("saved network differs from requested network")
		}
		c.network = saved
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if c.network == "" {
		c.network, err = engine.NewNetworkID()
		if err != nil {
			return err
		}
	}
	if !canonicalID(c.network) {
		return errors.New("invalid network ID")
	}
	if missingNetwork { // write once; do not silently switch overlays
		f, err := os.OpenFile(networkPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return err
		}
		_, writeErr := f.WriteString(c.network + "\n")
		closeErr := f.Close()
		if writeErr != nil {
			return writeErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	id, err := engine.LoadOrCreateIdentity(filepath.Join(c.state, "identity.json"))
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(parent, c.duration)
	defer cancel()
	n, err := engine.New(engine.Config{Identity: id, NetworkID: c.network, Application: "engine-netcheck.0.1.0", Bind: c.bind, DiscoveryCIDRs: split(c.discovery)})
	if err != nil {
		return err
	}
	defer n.Close()
	if err = n.Start(ctx); err != nil {
		return err
	}
	var joined *engine.JoinResult
	if imported != nil {
		result, err := n.Join(ctx, *imported, engine.JoinOptions{AllowedCIDRs: split(c.allow)})
		if err != nil {
			return err
		}
		joined = &result
	}
	var addresses []string
	if c.advertise != "" {
		addresses = []string{c.advertise}
	}
	invitation, err := n.Invitation(addresses, time.Now().Add(time.Hour))
	if err != nil {
		return err
	}
	uri, err := invitation.Encode()
	if err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(c.state, "invitation.txt"), []byte(uri+"\n"), 0600); err != nil {
		return err
	}
	encoder := json.NewEncoder(output)
	emit := func(event string, fields map[string]any) {
		if fields == nil {
			fields = map[string]any{}
		}
		fields["event"] = event
		_ = encoder.Encode(fields)
	}
	var joinFailures []string
	if joined != nil {
		for _, err := range joined.Failures {
			joinFailures = append(joinFailures, err.Error())
		}
	}
	emit("ready", map[string]any{"network_id": c.network, "peer_id": n.PeerID(), "signing_id": n.SigningID(), "address": n.Address(), "invitation": uri, "joined": joined, "join_failures": joinFailures})
	// This is a process-owned stdin reader, not an exported library lifecycle API.
	commands := make(chan string)
	go func() {
		defer close(commands)
		scanner := bufio.NewScanner(input)
		scanner.Buffer(make([]byte, 4096), 4096)
		for scanner.Scan() {
			select {
			case commands <- scanner.Text():
			case <-ctx.Done():
				return
			}
		}
	}()
	pending := map[string]pendingProbe{}
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	sendProbe := func(peer, signer string) {
		if !canonicalID(peer) || !canonicalID(signer) || len(pending) >= 16 {
			emit("command_error", map[string]any{"error": "invalid IDs or pending-probe limit"})
			return
		}
		matched := false
		for _, p := range n.Peers() {
			if p.ID == peer && p.SigningID == signer {
				matched = true
			}
		}
		if !matched {
			emit("command_error", map[string]any{"error": "connected peer does not match expected signing identity"})
			return
		}
		var nonce [16]byte
		if _, err := rand.Read(nonce[:]); err != nil {
			emit("command_error", map[string]any{"error": err.Error()})
			return
		}
		key := hex.EncodeToString(nonce[:])
		body, _ := json.Marshal(probeBody{key})
		if err := n.Send(ctx, peer, "probe.ping", body); err != nil {
			emit("command_error", map[string]any{"error": err.Error()})
			return
		}
		pending[key] = pendingProbe{signer, time.Now().Add(c.probeTimeout)}
		emit("probe_sent", map[string]any{"nonce": key, "peer_id": peer})
	}
	for {
		select {
		case <-ctx.Done():
			emit("stopped", nil)
			return nil
		case <-n.Done():
			emit("stopped", nil)
			return nil
		case line, ok := <-commands:
			if !ok {
				return nil
			}
			args := strings.Fields(line)
			if len(args) == 0 {
				continue
			}
			switch {
			case args[0] == "quit" && len(args) == 1:
				return nil
			case args[0] == "peers" && len(args) == 1:
				emit("peers", map[string]any{"peers": n.Peers()})
			case args[0] == "probe" && len(args) == 3:
				sendProbe(args[1], args[2])
			case args[0] == "disconnect" && len(args) == 2:
				if err := n.Disconnect(args[1]); err != nil {
					emit("command_error", map[string]any{"error": err.Error()})
				}
			default:
				emit("command_error", map[string]any{"error": "commands: peers | probe PEER_ID SIGNING_ID | disconnect PEER_ID | quit"})
			}
		case event, ok := <-n.Events():
			if !ok {
				return nil
			}
			emit("peer_event", map[string]any{"type": event.Type, "peer": event.Peer})
		case m, ok := <-n.Messages():
			if !ok {
				return nil
			}
			if len(m.Data) > 128 {
				continue
			}
			var body probeBody
			if json.Unmarshal(m.Data, &body) != nil || len(body.Nonce) != 32 {
				continue
			}
			nonce, err := hex.DecodeString(body.Nonce)
			if err != nil || hex.EncodeToString(nonce) != body.Nonce {
				continue
			}
			signer := hex.EncodeToString(m.SigningKey)
			switch m.Topic {
			case "probe.ping":
				for _, p := range n.Peers() {
					if p.SigningID == signer {
						_ = n.Send(ctx, p.ID, "probe.pong", m.Data)
						break
					}
				}
			case "probe.pong":
				if p, ok := pending[body.Nonce]; ok && p.signer == signer && time.Now().Before(p.deadline) {
					delete(pending, body.Nonce)
					emit("probe_ok", map[string]any{"nonce": body.Nonce, "signing_id": signer, "rtt_ms": time.Since(p.deadline.Add(-c.probeTimeout)).Milliseconds()})
				}
			}
		case now := <-tick.C:
			for nonce, p := range pending {
				if !now.Before(p.deadline) {
					delete(pending, nonce)
					emit("probe_timeout", map[string]any{"nonce": nonce, "signing_id": p.signer})
				}
			}
		}
	}
}
