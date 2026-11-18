// These are application-layer test fixtures, not a shipped delivery protocol.
package qualification_test

import (
	"bufio"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/TylerPetri/p2p-networking-project/engine"
)

type action struct {
	ID           string
	Expected     uint64
	Value        int
	ClaimedActor string
}
type applied struct {
	Action action
	Signer string
	Seq    uint64
}
type roomState struct {
	Seq     uint64
	Value   int
	Applied map[string]applied
}
type request struct {
	Kind   string
	Action action
	After  uint64
}
type response struct {
	Kind, ID string
	Seq      uint64
	Value    int
	Changes  []applied
}
type checkpoint struct {
	ID, Stage   string
	Seq         uint64
	AckEnqueued bool
}

func save(path string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".room-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
func load(path string, value any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, value)
}
func newRoomState() roomState { return roomState{Applied: make(map[string]applied)} }

// The single authority serializes actions. Apply + stable-ID record are written
// together before acknowledgement. This fixture has one writer and no claim of
// consensus, power-loss directory durability, unbounded retention or production use.
type roomAuthority struct {
	node        *engine.Engine
	path, owner string
	state       roomState
	faults      map[string]string
	observed    func(checkpoint)
	done        chan struct{}
	cancel      context.CancelFunc
}

func startRoom(t *testing.T, node *engine.Engine, path, owner string, faults map[string]string, observed func(checkpoint)) *roomAuthority {
	t.Helper()
	s := &roomAuthority{node: node, path: path, owner: owner, state: newRoomState(), faults: faults, observed: observed, done: make(chan struct{})}
	if err := load(path, &s.state); err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	go func() {
		defer close(s.done)
		for {
			select {
			case <-ctx.Done():
				return
			case <-node.Done():
				return
			case message, ok := <-node.Messages():
				if !ok {
					return
				}
				s.handle(message)
			}
		}
	}()
	t.Cleanup(func() { s.close() })
	return s
}
func (s *roomAuthority) close() { s.cancel(); _ = s.node.Close(); <-s.done }
func (s *roomAuthority) handle(message engine.Message) {
	var req request
	if message.Topic != "room-request" || json.Unmarshal(message.Data, &req) != nil {
		return
	}
	signer := hex.EncodeToString(message.SigningKey)
	reply := response{Seq: s.state.Seq, Value: s.state.Value, ID: req.Action.ID}
	stage := "responded"
	if req.Kind == "sync" {
		if req.After > s.state.Seq {
			reply.Kind = "conflict"
		} else if s.state.Seq-req.After > 2 {
			reply.Kind = "snapshot"
		} else {
			reply.Kind = "delta"
			for sequence := req.After + 1; sequence <= s.state.Seq; sequence++ {
				for _, entry := range s.state.Applied {
					if entry.Seq == sequence {
						reply.Changes = append(reply.Changes, entry)
					}
				}
			}
		}
	} else if req.Kind != "action" || signer != s.owner {
		reply.Kind = "denied"
	} else {
		// ClaimedActor is intentionally not an authorization input.
		previous, exists := s.state.Applied[req.Action.ID]
		switch {
		case exists:
			if previous.Signer != signer || previous.Action != req.Action {
				reply.Kind = "conflict"
			} else {
				reply.Kind = "applied"
				reply.Seq = previous.Seq
				reply.Value = previous.Action.Value
			}
		case req.Action.ID == "" || req.Action.Expected != s.state.Seq:
			reply.Kind = "conflict"
		case s.faults[req.Action.ID] == "before_apply":
			delete(s.faults, req.Action.ID)
			s.observed(checkpoint{ID: req.Action.ID, Stage: "before_apply", Seq: s.state.Seq})
			return
		default:
			next := newRoomState()
			next.Seq = s.state.Seq + 1
			next.Value = req.Action.Value
			for id, entry := range s.state.Applied {
				next.Applied[id] = entry
			}
			next.Applied[req.Action.ID] = applied{Action: req.Action, Signer: signer, Seq: next.Seq}
			if err := save(s.path, next); err != nil {
				reply.Kind = "storage_error"
			} else {
				s.state = next
				reply.Kind = "applied"
				reply.Seq = next.Seq
				reply.Value = next.Value
				if s.faults[req.Action.ID] == "after_apply" {
					delete(s.faults, req.Action.ID)
					s.observed(checkpoint{ID: req.Action.ID, Stage: "applied_without_ack", Seq: next.Seq})
					return
				}
			}
		}
	}
	data, err := json.Marshal(reply)
	enqueued := false
	if err == nil {
		// Route only through the direct, verified signing-key mapping. Do not reply to
		// an untrusted ClaimedPeerID or an actor identifier in the request payload.
		for _, peer := range s.node.Peers() {
			if peer.SigningID == signer {
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				enqueued = s.node.Send(ctx, peer.ID, "room-response", data) == nil
				cancel()
				break
			}
		}
	}
	s.observed(checkpoint{ID: req.Action.ID, Stage: stage, Seq: s.state.Seq, AckEnqueued: enqueued})
}

type clientState struct {
	Seq     uint64
	Value   int
	Pending *action
}
type roomClient struct {
	node                  *engine.Engine
	authority, peer, path string
	state                 clientState
	current               bool
}

func client(t *testing.T, node *engine.Engine, authority, peer, path string) *roomClient {
	t.Helper()
	c := &roomClient{node: node, authority: authority, peer: peer, path: path}
	if err := load(path, &c.state); err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	return c // A restored cache is not current until an authoritative sync/ack.
}
func (c *roomClient) send(ctx context.Context, req request) error {
	c.current = false
	if req.Kind == "action" {
		c.state.Pending = &req.Action
		if err := save(c.path, c.state); err != nil {
			return err
		}
	}
	data, err := json.Marshal(req)
	if err != nil {
		return err
	}
	return c.node.Send(ctx, c.peer, "room-request", data)
}
func (c *roomClient) receive(ctx context.Context) (response, error) {
	select {
	case m, ok := <-c.node.Messages():
		if !ok {
			return response{}, engine.ErrClosed
		}
		if m.Topic != "room-response" || hex.EncodeToString(m.SigningKey) != c.authority {
			return response{}, errors.New("unexpected room authority")
		}
		var r response
		if err := json.Unmarshal(m.Data, &r); err != nil {
			return r, err
		}
		next := c.state
		switch r.Kind {
		case "applied":
			if next.Pending == nil || r.ID != next.Pending.ID || r.Seq != next.Pending.Expected+1 {
				return r, errors.New("unmatched acknowledgement")
			}
			// An old duplicate ack confirms that action, not the latest room state.
			if r.Seq < next.Seq {
				return r, errors.New("stale acknowledgement requires sync")
			}
			next.Seq = r.Seq
			next.Value = r.Value
			next.Pending = nil
		case "delta":
			for _, entry := range r.Changes {
				if entry.Seq != next.Seq+1 {
					return r, errors.New("noncontiguous recovery")
				}
				next.Seq = entry.Seq
				next.Value = entry.Action.Value
			}
			if next.Seq != r.Seq || next.Value != r.Value {
				return r, errors.New("incomplete recovery")
			}
		case "snapshot":
			if r.Seq < next.Seq {
				return r, errors.New("stale snapshot")
			}
			next.Seq = r.Seq
			next.Value = r.Value
		default:
			return r, nil
		}
		if err := save(c.path, next); err != nil {
			return r, err
		}
		c.state = next
		c.current = true
		return r, nil
	case <-ctx.Done():
		return response{}, ctx.Err()
	}
}
func (c *roomClient) exchange(t *testing.T, req request) response {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.send(ctx, req); err != nil {
		t.Fatal(err)
	}
	r, err := c.receive(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func newNode(t *testing.T, network string, id *engine.Identity) *engine.Engine {
	t.Helper()
	n, err := engine.New(engine.Config{NetworkID: network, Identity: id, Application: "qualification-room", Logger: log.New(io.Discard, "", 0)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = n.Close() })
	if err = n.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	return n
}
func join(t *testing.T, n, server *engine.Engine) {
	t.Helper()
	inv, err := server.Invitation(nil, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	joinInvitation(t, n, inv)
}
func joinInvitation(t *testing.T, n *engine.Engine, inv engine.Invitation) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := n.Join(ctx, inv, engine.JoinOptions{AllowedCIDRs: []string{"127.0.0.1/32"}}); err != nil {
		t.Fatal(err)
	}
}
func networkID(t *testing.T) string {
	t.Helper()
	id, err := engine.NewNetworkID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}
func nextCheckpoint(t *testing.T, ch <-chan checkpoint, id, stage string) checkpoint {
	t.Helper()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		select {
		case p := <-ch:
			if p.ID == id && p.Stage == stage {
				return p
			}
		case <-timer.C:
			t.Fatal("authority checkpoint deadline", id, stage)
		}
	}
}

func TestRoomRecoveryAuthorizationAndIdempotency(t *testing.T) {
	dir := t.TempDir()
	network := networkID(t)
	owner := newNode(t, network, nil)
	viewer := newNode(t, network, nil)
	authority := newNode(t, network, nil)
	observed := make(chan checkpoint, 32)
	room := startRoom(t, authority, filepath.Join(dir, "room.json"), owner.SigningID(), map[string]string{"retry-before-apply": "before_apply", "lost-ack": "after_apply"}, func(p checkpoint) { observed <- p })
	join(t, owner, authority)
	join(t, viewer, authority)
	c := client(t, owner, authority.SigningID(), authority.PeerID(), filepath.Join(dir, "client.json"))
	v := client(t, viewer, authority.SigningID(), authority.PeerID(), filepath.Join(dir, "viewer.json"))
	if r := v.exchange(t, request{Kind: "action", Action: action{ID: "unauthorized", Value: 99, ClaimedActor: owner.SigningID()}}); r.Kind != "denied" {
		t.Fatal("public discovery granted a creator role", r)
	}
	// Failure before enqueue: explicit context error, no authoritative apply.
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := c.send(cancelled, request{Kind: "action", Action: action{ID: "cancelled", Value: 9}}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if r := c.exchange(t, request{Kind: "sync"}); r.Seq != 0 || r.Value != 0 {
		t.Fatal(r)
	}
	a := action{ID: "retry-before-apply", Value: 1}
	if err := c.send(context.Background(), request{Kind: "action", Action: a}); err != nil {
		t.Fatal(err)
	}
	if p := nextCheckpoint(t, observed, a.ID, "before_apply"); p.Seq != 0 || p.AckEnqueued {
		t.Fatal(p)
	}
	if c.current {
		t.Fatal("unknown outcome shown as current")
	}
	if r := c.exchange(t, request{Kind: "action", Action: a}); r.Kind != "applied" || r.Seq != 1 {
		t.Fatal(r)
	}
	lost := action{ID: "lost-ack", Expected: 1, Value: 2}
	if err := c.send(context.Background(), request{Kind: "action", Action: lost}); err != nil {
		t.Fatal(err)
	}
	if p := nextCheckpoint(t, observed, lost.ID, "applied_without_ack"); p.Seq != 2 || p.AckEnqueued {
		t.Fatal(p)
	}
	ctx, stop := context.WithTimeout(context.Background(), 50*time.Millisecond)
	if _, err := c.receive(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("missing acknowledgement must be unknown", err)
	}
	stop()
	if c.current || c.state.Seq != 1 {
		t.Fatal("lost acknowledgement advanced confirmed client state")
	}
	if r := c.exchange(t, request{Kind: "sync", After: c.state.Seq}); r.Kind != "delta" || c.state.Seq != 2 || c.state.Value != 2 {
		t.Fatal("delta recovery", r)
	}
	if r := c.exchange(t, request{Kind: "action", Action: lost}); r.Seq != 2 || r.Kind != "applied" {
		t.Fatal("duplicate applied twice", r)
	}
	if r := c.exchange(t, request{Kind: "action", Action: action{ID: lost.ID, Expected: 1, Value: 77}}); r.Kind != "conflict" {
		t.Fatal("stable ID payload substitution", r)
	}
	if r := c.exchange(t, request{Kind: "action", Action: action{ID: "out-of-order", Expected: 7, Value: 8}}); r.Kind != "conflict" {
		t.Fatal("required ordering bypassed", r)
	}
	for sequence := uint64(2); sequence < 4; sequence++ {
		if r := c.exchange(t, request{Kind: "action", Action: action{ID: fmt.Sprint("advance-", sequence), Expected: sequence, Value: int(sequence + 1)}}); r.Kind != "applied" {
			t.Fatal(r)
		}
	}
	if r := v.exchange(t, request{Kind: "sync", After: 0}); r.Kind != "snapshot" || v.state.Seq != 4 || v.state.Value != 4 || !v.current {
		t.Fatal("snapshot recovery", r)
	}
	join(t, viewer, owner)
	// A joined viewer cannot substitute the authority of a response.
	forged, _ := json.Marshal(response{Kind: "snapshot", Seq: 999, Value: 999})
	if err := viewer.Send(context.Background(), owner.PeerID(), "room-response", forged); err != nil {
		t.Fatal(err)
	}
	ctx, stop = context.WithTimeout(context.Background(), 5*time.Second)
	if _, err := c.receive(ctx); err == nil {
		t.Fatal("viewer substituted room authority")
	}
	stop()
	room.close()
	for _, n := range []*engine.Engine{authority, owner, viewer} {
		address := n.Address()
		if err := n.Close(); err != nil {
			t.Fatal(err)
		}
		listener, err := net.Listen("tcp", address)
		if err != nil {
			t.Fatal("listener leaked", err)
		}
		_ = listener.Close()
	}
	t.Log("ROOM accepted=4 applied=4 duplicate_reapplications=0 unauthorized=denied ordering=checked recovery=delta+snapshot stale_cache_not_current=true")
}

// The following authority runs in a separate OS process. A parent test kills it
// after the persisted apply checkpoint, before acknowledgement, then restarts it.
func TestRoomAuthorityProcess(t *testing.T) {
	dir := os.Getenv("P2P_ROOM_HELPER")
	if dir == "" {
		t.Skip("subprocess helper")
	}
	id, err := engine.LoadOrCreateIdentity(filepath.Join(dir, "identity.json"))
	if err != nil {
		t.Fatal(err)
	}
	node := newNode(t, os.Getenv("P2P_ROOM_NETWORK"), id)
	inv, err := node.Invitation(nil, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	encoder := json.NewEncoder(os.Stdout)
	if err = encoder.Encode(inv); err != nil {
		t.Fatal(err)
	}
	faults := map[string]string{}
	if os.Getenv("P2P_ROOM_DROP_ACK") == "1" {
		faults["persistent-action"] = "after_apply"
	}
	room := startRoom(t, node, filepath.Join(dir, "room.json"), os.Getenv("P2P_ROOM_OWNER"), faults, func(p checkpoint) { _ = encoder.Encode(p) })
	scanner := bufio.NewScanner(os.Stdin)
	if scanner.Scan() && scanner.Text() != "quit" {
		t.Fatal("invalid helper command")
	}
	room.close()
}

type roomProcess struct {
	cmd   *exec.Cmd
	input io.WriteCloser
	lines <-chan string
	done  <-chan error
}

func launchRoomProcess(t *testing.T, dir, network, owner string, drop bool) (*roomProcess, engine.Invitation) {
	t.Helper()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "-test.run=^TestRoomAuthorityProcess$", "-test.timeout=30s")
	cmd.Env = append(os.Environ(), "P2P_ROOM_HELPER="+dir, "P2P_ROOM_NETWORK="+network, "P2P_ROOM_OWNER="+owner, fmt.Sprintf("P2P_ROOM_DROP_ACK=%d", map[bool]int{true: 1, false: 0}[drop]))
	output, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	input, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	lines := make(chan string, 32)
	done := make(chan error, 1)
	go func() {
		scanner := bufio.NewScanner(output)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
		close(lines)
		done <- cmd.Wait()
	}()
	process := &roomProcess{cmd: cmd, input: input, lines: lines, done: done}
	t.Cleanup(func() { _ = input.Close(); _ = cmd.Process.Kill() })
	var inv engine.Invitation
	select {
	case line := <-lines:
		if err = json.Unmarshal([]byte(line), &inv); err != nil {
			t.Fatal("helper startup", line, err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("helper startup deadline")
	}
	return process, inv
}
func TestRoomProcessRestartAfterLostAcknowledgement(t *testing.T) {
	dir := t.TempDir()
	network := networkID(t)
	identity, err := engine.LoadOrCreateIdentity(filepath.Join(dir, "client-identity.json"))
	if err != nil {
		t.Fatal(err)
	}
	owner := newNode(t, network, identity)
	authorityDir := filepath.Join(dir, "authority")
	if err = os.Mkdir(authorityDir, 0700); err != nil {
		t.Fatal(err)
	}
	process, inv := launchRoomProcess(t, authorityDir, network, owner.SigningID(), true)
	joinInvitation(t, owner, inv)
	c := client(t, owner, inv.Bootstraps[0].SigningID, inv.Bootstraps[0].PeerID, filepath.Join(dir, "client-state.json"))
	intent := action{ID: "persistent-action", Value: 42}
	if err = c.send(context.Background(), request{Kind: "action", Action: intent}); err != nil {
		t.Fatal(err)
	}
	select {
	case line := <-process.lines:
		var p checkpoint
		if err = json.Unmarshal([]byte(line), &p); err != nil || p.Stage != "applied_without_ack" || p.Seq != 1 {
			t.Fatal(line, err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("persisted apply checkpoint deadline")
	}
	_ = process.cmd.Process.Kill()
	select {
	case <-process.done:
	case <-time.After(5 * time.Second):
		t.Fatal("crashed authority did not exit")
	}
	_ = owner.Close()
	// Restart both authority and client as new OS processes. The client loads the
	// durable pending ID and last confirmed sequence, not parent in-memory state.
	process, again := launchRoomProcess(t, authorityDir, network, identity.SigningID(), false)
	if again.Bootstraps[0].SigningID != inv.Bootstraps[0].SigningID || again.Bootstraps[0].PeerID != inv.Bootstraps[0].PeerID {
		t.Fatal("authority identity changed")
	}
	if err = save(filepath.Join(dir, "invitation.json"), again); err != nil {
		t.Fatal(err)
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	childContext, childCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer childCancel()
	child := exec.CommandContext(childContext, binary, "-test.run=^TestRoomClientProcess$", "-test.timeout=12s")
	child.Env = append(os.Environ(), "P2P_ROOM_CLIENT_HELPER="+dir, "P2P_ROOM_NETWORK="+network,
		"P2P_ROOM_EXPECTED_AUTHORITY="+inv.Bootstraps[0].SigningID)
	if output, err := child.CombinedOutput(); err != nil {
		t.Fatalf("resumed client process: %v\n%s", err, output)
	}
	var confirmed clientState
	if err = load(filepath.Join(dir, "client-state.json"), &confirmed); err != nil || confirmed.Seq != 1 || confirmed.Value != 42 || confirmed.Pending != nil {
		t.Fatal("client confirmation did not survive process exit", confirmed, err)
	}
	_, _ = io.WriteString(process.input, "quit\n")
	_ = process.input.Close()
	select {
	case err = <-process.done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("helper shutdown deadline")
	}
	t.Log("ROOM process_killed_after_persist=true stable_id_survived=true applied=1 last_confirmed_recovered=true identity_preserved=true")
}

func TestRoomClientProcess(t *testing.T) {
	dir := os.Getenv("P2P_ROOM_CLIENT_HELPER")
	if dir == "" {
		t.Skip("subprocess helper")
	}
	identity, err := engine.LoadOrCreateIdentity(filepath.Join(dir, "client-identity.json"))
	if err != nil {
		t.Fatal(err)
	}
	var invitation engine.Invitation
	if err = load(filepath.Join(dir, "invitation.json"), &invitation); err != nil {
		t.Fatal(err)
	}
	if invitation.Bootstraps[0].SigningID != os.Getenv("P2P_ROOM_EXPECTED_AUTHORITY") {
		t.Fatal("authority substitution")
	}
	node := newNode(t, os.Getenv("P2P_ROOM_NETWORK"), identity)
	joinInvitation(t, node, invitation)
	c := client(t, node, invitation.Bootstraps[0].SigningID, invitation.Bootstraps[0].PeerID, filepath.Join(dir, "client-state.json"))
	if c.current || c.state.Seq != 0 || c.state.Pending == nil || c.state.Pending.ID != "persistent-action" {
		t.Fatal("restored cache is not confirmed current")
	}
	if r := c.exchange(t, request{Kind: "action", Action: *c.state.Pending}); r.Kind != "applied" || r.Seq != 1 || r.Value != 42 {
		t.Fatal("restart duplicate", r)
	}
	if r := c.exchange(t, request{Kind: "sync", After: c.state.Seq}); r.Seq != 1 || r.Value != 42 || !c.current {
		t.Fatal("restart convergence", r)
	}
}

func TestRoomRejectsStaleOrGappedRecovery(t *testing.T) {
	network := networkID(t)
	authority := newNode(t, network, nil)
	viewer := newNode(t, network, nil)
	join(t, viewer, authority)
	c := client(t, viewer, authority.SigningID(), authority.PeerID(), filepath.Join(t.TempDir(), "client.json"))
	c.state = clientState{Seq: 3, Value: 3, Pending: &action{ID: "old", Expected: 0, Value: 1}}
	for _, r := range []response{
		{Kind: "snapshot", Seq: 2, Value: 2},
		{Kind: "delta", Seq: 5, Value: 5, Changes: []applied{{Seq: 5, Action: action{Value: 5}}}},
		{Kind: "applied", ID: "old", Seq: 1, Value: 1},
	} {
		c.current = false
		data, _ := json.Marshal(r)
		if err := authority.Send(context.Background(), viewer.PeerID(), "room-response", data); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, err := c.receive(ctx)
		cancel()
		if err == nil || c.current || c.state.Seq != 3 || c.state.Value != 3 {
			t.Fatal("stale/gapped state accepted", r, err, c.state)
		}
	}
	t.Log("ROOM stale_snapshot=rejected sequence_gap=rejected stale_ack=requires_sync confirmed_state_unchanged=true")
}
