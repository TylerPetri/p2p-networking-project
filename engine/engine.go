// Package engine provides an experimental, application-independent API over the
// existing Noise/TCP, signed gossip, scoped discovery and DHT implementation.
// It is not production-ready or independently security-audited.
package engine

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"p2p-park/internal/netx"
	"p2p-park/internal/p2p"
	"p2p-park/internal/wire"
)

const Protocol = "p2p-engine/0.1.0"
const MaxMessageSize = 64 * 1024

const (
	EventPeerConnected    = "peer_connected"
	EventPeerDisconnected = "peer_disconnected"
	EventDiscoveryError   = "discovery_error"
)

var (
	ErrClosed         = errors.New("engine: closed")
	ErrNotStarted     = errors.New("engine: not started")
	ErrAlreadyStarted = errors.New("engine: already started")
	ErrInvalidMessage = errors.New("engine: invalid topic or message size")
)

type Admission string

const (
	Open       Admission = "open"
	Restricted Admission = "restricted"
)

// Config is copied by New. Empty Bind defaults to loopback and an OS-selected
// port. Empty Admission defaults to Open; Restricted with no approvals denies all
// remote peers. Empty Identity generates an ephemeral identity. No implicit disk
// peer store or LAN broadcaster is enabled by default.
type Config struct {
	NetworkID        string
	Application      string
	Identity         *Identity
	Bind             string
	Name             string
	Bootstraps       []string
	PeerPins         map[string]string
	DiscoveryCIDRs   []string
	Admission        Admission
	ApprovedPeers    map[string]string
	MaxConnections   int
	MessageTimeout   time.Duration
	PeerStorePath    string
	LANDiscovery     bool
	LANPeerStorePath string
	Rendezvous       bool
	Debug            bool
	Logger           *log.Logger
}

// Message authenticates SigningKey, not application authority. ClaimedPeerID is
// a signed transport-ID claim; restricted approvals pin its signing-key pair.
// Data and SigningKey are owned by the receiver and may be modified by it.
type Message struct {
	ID, ClaimedPeerID, Topic string
	SigningKey, Data         []byte
}
type Peer struct{ ID, Name, Address, SigningID string }
type Event struct {
	Error string
	Type  string
	Peer  Peer
}
type applicationMessage struct {
	Topic string `json:"topic"`
	Data  []byte `json:"data"`
}

type Engine struct {
	mu           sync.Mutex
	state        int // new=0, running=1, closed=2; no restart of a closed instance
	node         *p2p.Node
	application  string
	networkID    string
	peerPins     map[netx.Addr]string
	done         chan struct{}
	messages     chan Message
	events       chan Event
	wg           sync.WaitGroup
	stopContext  func() bool
	closeErr     error
	lan          bool
	lanStorePath string
}

func NewNetworkID() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
func validApplication(s string) bool {
	if s == "" || len(s) > 64 {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("._-", c)) {
			return false
		}
	}
	return true
}
func New(cfg Config) (*Engine, error) {
	if !validApplication(cfg.Application) {
		return nil, errors.New("engine: application must be 1..64 ASCII letters/digits/._-")
	}
	b, err := hex.DecodeString(cfg.NetworkID)
	if err != nil || len(b) != 32 || hex.EncodeToString(b) != cfg.NetworkID {
		return nil, errors.New("engine: network ID must be 32-byte lowercase hex")
	}
	if cfg.Admission == "" {
		cfg.Admission = Open
	}
	if cfg.Admission != Open && cfg.Admission != Restricted {
		return nil, errors.New("engine: invalid admission mode")
	}
	if cfg.Bind == "" {
		cfg.Bind = "127.0.0.1:0"
	}
	if len(cfg.Name) > 256 {
		return nil, errors.New("engine: name too long")
	}
	var id *p2p.Identity
	if cfg.Identity != nil {
		if cfg.Identity.id == nil {
			return nil, errors.New("engine: invalid identity")
		}
		id = cfg.Identity.id
	}
	pins := make(map[netx.Addr]string)
	for addr, pid := range cfg.PeerPins {
		pins[netx.Addr(addr)] = pid
	}
	boots := make([]netx.Addr, 0, len(cfg.Bootstraps))
	for _, addr := range cfg.Bootstraps {
		boots = append(boots, netx.Addr(addr))
	}
	for _, path := range []string{cfg.PeerStorePath, cfg.LANPeerStorePath} {
		if path == "" {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return nil, err
		}
	}
	n, err := p2p.NewNode(p2p.NodeConfig{IsSeed: cfg.Rendezvous, Debug: cfg.Debug, Logger: cfg.Logger, Identity: id, NetworkID: cfg.NetworkID, Restricted: cfg.Admission == Restricted, ApprovedPeers: cfg.ApprovedPeers, Name: cfg.Name, Network: netx.NewTCPNetwork(), BindAddr: cfg.Bind, Bootstraps: boots, Protocol: Protocol, PeerPins: pins, DiscoveryCIDRs: cfg.DiscoveryCIDRs, MaxConnections: cfg.MaxConnections, MessageTimeout: cfg.MessageTimeout, DisablePeerStore: cfg.PeerStorePath == "", PeerStorePath: cfg.PeerStorePath})
	if err != nil {
		return nil, err
	}
	return &Engine{networkID: cfg.NetworkID, peerPins: pins, lan: cfg.LANDiscovery, lanStorePath: cfg.LANPeerStorePath, node: n, application: cfg.Application, done: make(chan struct{}), messages: make(chan Message, 128), events: make(chan Event, 128)}, nil
}

// Start is one-shot. Its context owns the engine lifetime, including established
// peers. A pre-cancelled context leaves the instance unstarted and retryable.
func (e *Engine) Start(ctx context.Context) error {
	if ctx == nil {
		return errors.New("engine: nil context")
	}
	e.mu.Lock()
	if e.state == 2 {
		e.mu.Unlock()
		return ErrClosed
	}
	if e.state == 1 {
		e.mu.Unlock()
		return ErrAlreadyStarted
	}
	if err := ctx.Err(); err != nil {
		e.mu.Unlock()
		return err
	}
	e.state = 1
	if err := e.node.Start(); err != nil {
		e.mu.Unlock()
		_ = e.Close()
		return err
	}
	e.startLAN()
	e.wg.Add(1)
	go e.pump()
	e.stopContext = context.AfterFunc(ctx, func() { _ = e.Close() })
	e.mu.Unlock()
	return nil
}

// Close is concurrent-safe, idempotent, and waits for tracked engine/node work.
func (e *Engine) Close() error {
	e.mu.Lock()
	if e.state == 2 {
		done := e.done
		e.mu.Unlock()
		<-done
		e.mu.Lock()
		err := e.closeErr
		e.mu.Unlock()
		return err
	}
	e.state = 2
	stop := e.stopContext
	e.mu.Unlock()
	if stop != nil {
		stop()
	}
	err := e.node.Stop()
	e.wg.Wait()
	e.mu.Lock()
	e.closeErr = err
	close(e.messages)
	close(e.events)
	close(e.done)
	e.mu.Unlock()
	return err
}
func (e *Engine) running() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.state == 2 {
		return ErrClosed
	}
	if e.state == 0 {
		return ErrNotStarted
	}
	return nil
}
func (e *Engine) PeerID() string { return e.node.ID() }

// SigningID is the hex Ed25519 public key, used with PeerID in approval pins.
func (e *Engine) SigningID() string { return hex.EncodeToString(e.node.Identity().SignPub) }
func (e *Engine) Address() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return string(e.node.ListenAddr())
}
func (e *Engine) Done() <-chan struct{}    { return e.done }
func (e *Engine) Messages() <-chan Message { return e.messages }
func (e *Engine) Events() <-chan Event     { return e.events }

// Connect returns after identity/domain/admission checks, or failure. Its context
// only bounds this connection attempt, not the lifetime of a successful session.
func (e *Engine) Connect(ctx context.Context, addr string) error {
	if err := e.running(); err != nil {
		return err
	}
	return e.node.ConnectContext(ctx, netx.Addr(addr))
}
func (e *Engine) Disconnect(id string) error {
	if err := e.running(); err != nil {
		return err
	}
	return e.node.DisconnectPeer(id)
}
func (e *Engine) Peers() []Peer {
	src := e.node.SnapshotPeers()
	out := make([]Peer, 0, len(src))
	for _, p := range src {
		out = append(out, Peer{p.NetworkID, p.Name, p.Addr, p.UserID})
	}
	return out
}
func (e *Engine) gossip(topic string, data []byte) (wire.Gossip, error) {
	if len(topic) == 0 || len(topic) > 64 || len(data) > MaxMessageSize {
		return wire.Gossip{}, ErrInvalidMessage
	}
	b, err := json.Marshal(applicationMessage{topic, data})
	if err != nil {
		return wire.Gossip{}, err
	}
	return wire.Gossip{ID: p2p.NewMsgID(), Channel: "engine/" + e.application, Body: b}, nil
}

// Send enqueues a signed recipient-bound message; it does not broadcast or confirm
// remote delivery. Queue failure disconnects the slow peer and returns an error.
func (e *Engine) Send(ctx context.Context, peer, topic string, data []byte) error {
	if err := e.running(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	g, err := e.gossip(topic, data)
	if err != nil {
		return err
	}
	return e.node.SendGossip(peer, g)
}

// Publish is best-effort signed gossip. An error can mean partial fan-out.
func (e *Engine) Publish(ctx context.Context, topic string, data []byte) error {
	if err := e.running(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	g, err := e.gossip(topic, data)
	if err != nil {
		return err
	}
	return e.node.PublishGossip(g)
}
func (e *Engine) pump() {
	defer e.wg.Done()
	for {
		select {
		case <-e.node.Done():
			return
		case ev := <-e.node.Events():
			select {
			case e.events <- Event{Type: string(ev.Type), Peer: Peer{ID: ev.PeerID, Name: ev.PeerName, Address: ev.PeerAddr}}:
			default:
			}
		case env := <-e.node.Incoming():
			if env.Type != wire.MsgGossip {
				continue
			}
			var g wire.Gossip
			if json.Unmarshal(env.Payload, &g) != nil || g.Channel != "engine/"+e.application {
				continue
			}
			var m applicationMessage
			if json.Unmarshal(g.Body, &m) != nil || len(m.Topic) == 0 || len(m.Topic) > 64 || len(m.Data) > MaxMessageSize {
				continue
			}
			msg := Message{ID: g.ID, ClaimedPeerID: env.FromID, Topic: m.Topic, SigningKey: append([]byte(nil), env.OriginPub...), Data: m.Data}
			select {
			case e.messages <- msg:
			default:
			}
		}
	}
}
func (e *Engine) String() string { return fmt.Sprintf("engine peer=%s", e.PeerID()) }

// Public error identities preserve errors.Is without exposing internal types.
var (
	ErrResourceLimit   = p2p.ErrResourceLimit
	ErrPeerClosed      = p2p.ErrPeerClosed
	ErrAdmissionDenied = p2p.ErrAdmissionDenied
)
