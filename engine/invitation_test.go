package engine_test

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"p2p-park/engine"
)

var loopbackInvites = engine.JoinOptions{AllowedCIDRs: []string{"127.0.0.1/32"}}

func invite(t *testing.T, n *engine.Engine) engine.Invitation {
	t.Helper()
	i, err := n.Invitation(nil, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	return i
}
func uriJSON(s string) string {
	return "p2p://invite/" + base64.RawURLEncoding.EncodeToString([]byte(s))
}
func invitationJoin(t *testing.T, n *engine.Engine, i engine.Invitation) engine.JoinResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result, err := n.Join(ctx, i, loopbackInvites)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func waitFor(t *testing.T, f func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if f() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition not satisfied")
}
func TestInvitationCanonicalParsingAndPolicy(t *testing.T) {
	a := node(t, network(t), "invitation", nil)
	i := invite(t, a)
	encoded, err := i.Encode()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := engine.ParseInvitation(encoded)
	if err != nil {
		t.Fatal(err)
	}
	again, err := parsed.Encode()
	if err != nil || again != encoded {
		t.Fatal("unstable encoding", err)
	}
	if len(encoded) > engine.MaxInvitationSize || parsed.Bootstraps[0].SigningID != a.SigningID() {
		t.Fatal("invalid generated invitation")
	}
	raw, _ := json.Marshal(i)
	valid := string(raw)
	cases := []string{
		"", strings.Repeat("x", engine.MaxInvitationSize+1), encoded + "=", encoded + "\n", encoded + "?x=1", strings.ToUpper(encoded),
		uriJSON(" " + valid), uriJSON(valid + " {}"), uriJSON(valid + "\n"),
		uriJSON(strings.Replace(valid, `"version":1`, `"version":1,"version":1`, 1)),
		uriJSON(strings.Replace(valid, `"version":1`, `"version":1,"extra":true`, 1)),
		uriJSON(strings.Replace(valid, `"version":1`, `"version":1,"expires_unix":null`, 1)),
		uriJSON(strings.Replace(valid, `"version":1`, `"version":1.0`, 1)),
	}
	for _, bad := range cases {
		if _, err := engine.ParseInvitation(bad); err == nil {
			t.Fatalf("accepted noncanonical invitation %.60q", bad)
		}
	}
	two := parsed
	two.Bootstraps = append([]engine.BootstrapPeer(nil), parsed.Bootstraps...)
	extra := two.Bootstraps[0]
	extra.Address = "127.0.0.1:1"
	two.Bootstraps = append(two.Bootstraps, extra)
	sortedURI, err := two.Encode()
	if err != nil {
		t.Fatal(err)
	}
	two.Bootstraps[0], two.Bootstraps[1] = two.Bootstraps[1], two.Bootstraps[0]
	reverseURI, err := two.Encode()
	if err != nil || reverseURI != sortedURI {
		t.Fatal("bootstrap order changed canonical representation", err)
	}
	rawSorted, _ := json.Marshal(two)
	if uriJSON(string(rawSorted)) == sortedURI {
		two.Bootstraps[0], two.Bootstraps[1] = two.Bootstraps[1], two.Bootstraps[0]
	}
	unsorted, _ := json.Marshal(two)
	if uriJSON(string(unsorted)) != sortedURI {
		if _, err := engine.ParseInvitation(uriJSON(string(unsorted))); err == nil {
			t.Fatal("noncanonical bootstrap order accepted")
		}
	}
	if _, err := a.Invitation(nil, time.Unix(0, 0)); !errors.Is(err, engine.ErrInvitationExpired) {
		t.Fatal("explicit Unix epoch expiry became no expiry", err)
	}
	i.Version = 2
	if _, err := i.Encode(); !errors.Is(err, engine.ErrUnsupportedInvitationVersion) {
		t.Fatal(err)
	}
	if _, err := engine.ParseInvitation(uriJSON(strings.Replace(valid, `"version":1`, `"version":2`, 1))); !errors.Is(err, engine.ErrUnsupportedInvitationVersion) {
		t.Fatal(err)
	}
	i = parsed
	i.ExpiresUnix = time.Now().Add(-time.Second).Unix()
	if _, err := i.Encode(); !errors.Is(err, engine.ErrInvitationExpired) {
		t.Fatal(err)
	}
	badEndpoints := []string{"localhost:8080", "example.com:80", "http://127.0.0.1:80", "127.0.0.1:0", "127.0.0.1:080", "0.0.0.0:80", "0.1.2.3:80", "169.254.169.254:80", "168.63.129.16:80", "100.100.100.200:80", "224.0.0.1:80", "255.255.255.255:80", "240.0.0.1:80", "192.0.2.1:80", "[::]:80", "[fe80::1]:80", "[fe80::1%eth0]:80", "[::ffff:127.0.0.1]:80", "[64:ff9b::7f00:1]:80", "[2001:db8::1]:80", "[2002:7f00:1::1]:80", "[ff02::1]:80"}
	for _, address := range badEndpoints {
		i = parsed
		i.Bootstraps = append([]engine.BootstrapPeer(nil), parsed.Bootstraps...)
		i.Bootstraps[0].Address = address
		if _, err := i.Encode(); err == nil {
			t.Fatal("accepted unsafe address", address)
		}
	}
	for _, address := range []string{"10.0.0.1:80", "[fd00::1]:80", "100.64.0.1:80", "[::1]:80", "8.8.8.8:443", "[2606:4700::1111]:443"} {
		i = parsed
		i.Bootstraps = append([]engine.BootstrapPeer(nil), parsed.Bootstraps...)
		i.Bootstraps[0].Address = address
		if _, err := i.Encode(); err != nil {
			t.Fatal("rejected structurally valid endpoint", address, err)
		}
	}
	i = parsed
	i.Bootstraps = append(i.Bootstraps, i.Bootstraps[0])
	if _, err := i.Encode(); err == nil {
		t.Fatal("duplicate endpoint accepted")
	}
	i = parsed
	i.Bootstraps = make([]engine.BootstrapPeer, engine.MaxInvitationBootstraps+1)
	if _, err := i.Encode(); err == nil {
		t.Fatal("oversized bootstrap list accepted")
	}
}

func TestInvitationJoinDiscoverySurvivesBootstrapLoss(t *testing.T) {
	id := network(t)
	a := node(t, id, "invite-echo", nil)
	b := node(t, id, "invite-echo", nil)
	c := node(t, id, "invite-echo", nil)
	original := invite(t, a)
	if result := invitationJoin(t, b, original); len(result.Connected) != 1 || len(result.Failures) != 0 {
		t.Fatal(result)
	}
	invitationJoin(t, c, original)
	waitPeer(t, b, c.PeerID())
	waitPeer(t, c, b.PeerID()) // peer-list discovery, not a second manual Connect
	bInvite := invite(t, b)
	alternate := original
	alternate.Bootstraps = append(alternate.Bootstraps, bInvite.Bootstraps...)
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return len(b.Peers()) == 1 && len(c.Peers()) == 1 })
	if err := b.Send(context.Background(), c.PeerID(), "after-bootstrap", []byte("still connected")); err != nil {
		t.Fatal(err)
	}
	if m := receive(t, c); string(m.Data) != "still connected" || hex.EncodeToString(m.SigningKey) != b.SigningID() {
		t.Fatal(m)
	}
	d := node(t, id, "invite-echo", nil)
	result := invitationJoin(t, d, alternate)
	if len(result.Connected) != 1 || len(result.Failures) != 1 {
		t.Fatal("fallback/partial result", result)
	}
	if err := d.Send(context.Background(), b.PeerID(), "fallback", []byte("rejoined")); err != nil {
		t.Fatal(err)
	}
	if m := receive(t, b); string(m.Data) != "rejoined" {
		t.Fatal(m)
	}
}

func TestInvitationMultipleBootstrapsAndConcurrentJoins(t *testing.T) {
	id := network(t)
	a := node(t, id, "parallel-invite", nil)
	b := node(t, id, "parallel-invite", nil)
	i := invite(t, a)
	i.Bootstraps = append(i.Bootstraps, invite(t, b).Bootstraps...)
	var clients []*engine.Engine
	for j := 0; j < 4; j++ {
		clients = append(clients, node(t, id, "parallel-invite", nil))
	}
	var wg sync.WaitGroup
	for _, client := range clients {
		wg.Add(1)
		go func(c *engine.Engine) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			result, err := c.Join(ctx, i, loopbackInvites)
			if err != nil || len(result.Connected) != 2 {
				t.Errorf("concurrent join result %v %v", result, err)
			}
		}(client)
	}
	wg.Wait()
	// Repeat concurrent joins on one already connected node: no trust mutation.
	for j := 0; j < 4; j++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := clients[0].Join(context.Background(), i, loopbackInvites); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
}

func TestInvitationPinsMembershipIsolationAndPreflight(t *testing.T) {
	id := network(t)
	a := node(t, id, "pins", nil)
	b := node(t, id, "pins", nil)
	good := invite(t, a)
	// Private/local invitations require receiving-operator approval, even for a loopback bind.
	if _, err := b.Join(context.Background(), good, engine.JoinOptions{}); !errors.Is(err, engine.ErrInvalidInvitation) {
		t.Fatal(err)
	}
	if len(b.Peers()) != 0 {
		t.Fatal("scope rejection dialed")
	}
	for _, which := range []string{"noise", "signing"} {
		bad := good
		bad.Bootstraps = append([]engine.BootstrapPeer(nil), good.Bootstraps...)
		if which == "noise" {
			bad.Bootstraps[0].PeerID = b.PeerID()
		} else {
			bad.Bootstraps[0].SigningID = b.SigningID()
		}
		if _, err := b.Join(context.Background(), bad, loopbackInvites); !errors.Is(err, engine.ErrBootstrapUnavailable) {
			t.Fatal(which, err)
		}
		if len(b.Peers()) != 0 || len(a.Peers()) != 0 {
			t.Fatal("identity mismatch admitted")
		}
	}
	invitationJoin(t, b, good)
	// An existing connection cannot bypass the signing pin on another invitation.
	bad := good
	bad.Bootstraps = append([]engine.BootstrapPeer(nil), good.Bootstraps...)
	bad.Bootstraps[0].SigningID = b.SigningID()
	if _, err := b.Join(context.Background(), bad, loopbackInvites); err == nil {
		t.Fatal("existing peer bypassed signing pin")
	}
	other := node(t, network(t), "pins", nil)
	if _, err := other.Join(context.Background(), good, loopbackInvites); !errors.Is(err, engine.ErrInvalidInvitation) {
		t.Fatal(err)
	}
	// Matching invitation/config domain still cannot cross an actual peer's domain.
	cross := good
	cross.NetworkID = network(t)
	crossNode := node(t, cross.NetworkID, "pins", nil)
	if _, err := crossNode.Join(context.Background(), cross, loopbackInvites); !errors.Is(err, engine.ErrBootstrapUnavailable) {
		t.Fatal(err)
	}
	restricted := node(t, id, "pins", func(c *engine.Config) { c.Admission = engine.Restricted })
	if _, err := restricted.Join(context.Background(), good, loopbackInvites); !errors.Is(err, engine.ErrBootstrapUnavailable) {
		t.Fatal("invitation bypassed admission", err)
	}
	// Preflight the entire list; a later invalid record must not dial the first.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	accepted := make(chan struct{})
	go func() {
		conn, err := listener.Accept()
		if err == nil {
			conn.Close()
			close(accepted)
		}
	}()
	malformed := good
	malformed.Bootstraps = []engine.BootstrapPeer{{Address: listener.Addr().String(), PeerID: a.PeerID(), SigningID: a.SigningID()}, {Address: "169.254.169.254:80", PeerID: a.PeerID(), SigningID: a.SigningID()}}
	if _, err := node(t, id, "preflight", nil).Join(context.Background(), malformed, loopbackInvites); err == nil {
		t.Fatal("malformed invitation accepted")
	}
	select {
	case <-accepted:
		t.Fatal("network activity before full preflight")
	case <-time.After(30 * time.Millisecond):
	}
}

func TestInvitationCancellationExpiryAndCleanup(t *testing.T) {
	id := network(t)
	n := node(t, id, "cancel-invite", func(c *engine.Config) { c.MaxConnections = 1 })
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	accepted := make(chan net.Conn, 1)
	go func() {
		c, err := listener.Accept()
		if err == nil {
			accepted <- c
		}
	}()
	peer := node(t, id, "cancel-invite", nil)
	i := invite(t, peer)
	i.Bootstraps[0].Address = listener.Addr().String()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := n.Join(ctx, i, loopbackInvites); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	select {
	case c := <-accepted:
		defer c.Close()
	case <-time.After(time.Second):
		t.Fatal("no stalled connection")
	}
	// Cancellation frees the budget for an actual authenticated attempt.
	waitFor(t, func() bool {
		_, err := n.Join(context.Background(), invite(t, peer), loopbackInvites)
		return err == nil
	})
	expired := invite(t, peer)
	expired.ExpiresUnix = time.Now().Add(-time.Second).Unix()
	if _, err := n.Join(context.Background(), expired, loopbackInvites); !errors.Is(err, engine.ErrInvitationExpired) {
		t.Fatal(err)
	}
	canceled, stop := context.WithCancel(context.Background())
	stop()
	if _, err := n.Join(canceled, invite(t, peer), loopbackInvites); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	addr := n.Address()
	if err := n.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := n.Join(context.Background(), invite(t, peer), loopbackInvites); !errors.Is(err, engine.ErrClosed) {
		t.Fatal(err)
	}
	l, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatal("listener leaked", err)
	}
	l.Close()
}

func FuzzInvitationParse(f *testing.F) {
	seed := engine.Invitation{Version: 1, NetworkID: strings.Repeat("1", 64), Bootstraps: []engine.BootstrapPeer{{Address: "127.0.0.1:4000", PeerID: strings.Repeat("2", 64), SigningID: strings.Repeat("3", 64)}}}
	encoded, _ := seed.Encode()
	f.Add(encoded)
	f.Add("")
	f.Add(uriJSON(`{"version":2}`))
	f.Add(strings.Repeat("x", engine.MaxInvitationSize+1))
	f.Fuzz(func(t *testing.T, input string) {
		i, err := engine.ParseInvitation(input)
		if err != nil {
			return
		}
		output, err := i.Encode()
		if errors.Is(err, engine.ErrInvitationExpired) {
			return
		}
		if err != nil || output != input {
			t.Fatalf("accepted noncanonical input: %v", err)
		}
	})
}

func TestInvitationPreflightPolicyAndConfiguredPins(t *testing.T) {
	id := network(t)
	server := node(t, id, "preflight-pins", nil)
	good := invite(t, server)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	accepted := make(chan struct{})
	go func() {
		c, err := l.Accept()
		if err == nil {
			c.Close()
			close(accepted)
		}
	}()
	local := good.Bootstraps[0]
	local.Address = l.Addr().String()
	// A valid but unapproved private destination must reject the full invitation.
	candidate := good
	candidate.Bootstraps = []engine.BootstrapPeer{local, {Address: "10.0.0.1:4000", PeerID: server.PeerID(), SigningID: server.SigningID()}}
	client := node(t, id, "preflight-pins", nil)
	if _, err := client.Join(context.Background(), candidate, loopbackInvites); !errors.Is(err, engine.ErrInvalidInvitation) {
		t.Fatal(err)
	}
	// Operator pins cannot be overwritten by imported pins, even if another entry
	// could have connected first. All policy conflicts are checked before dialing.
	pinned := node(t, id, "preflight-pins", func(c *engine.Config) { c.PeerPins = map[string]string{server.Address(): client.PeerID()} })
	candidate = good
	candidate.Bootstraps = append([]engine.BootstrapPeer{local}, good.Bootstraps...)
	if _, err := pinned.Join(context.Background(), candidate, loopbackInvites); !errors.Is(err, engine.ErrInvalidInvitation) {
		t.Fatal(err)
	}
	select {
	case <-accepted:
		t.Fatal("dial before policy/pin preflight")
	case <-time.After(30 * time.Millisecond):
	}
	if _, err := client.Invitation(make([]string, engine.MaxInvitationBootstraps+1), time.Time{}); err == nil {
		t.Fatal("generator did not bound endpoint count")
	}
}
