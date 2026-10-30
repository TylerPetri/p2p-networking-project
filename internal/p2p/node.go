package p2p

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/netip"
	"p2p-park/internal/dht"
	"p2p-park/internal/netx"
	"p2p-park/internal/proto"
	"p2p-park/internal/telemetry"
	"sync"
	"time"
)

type NodeConfig struct {
	MaxConnections int
	MessageTimeout time.Duration
	// Explicit operator trust: address -> expected Noise static key hex.
	PeerPins map[netx.Addr]string
	// Automatic discovery never resolves DNS; additional numeric destinations
	// require operator-approved CIDRs. Same-host referrals remain allowed.
	DiscoveryCIDRs []string
	Name           string           // user-facing name
	Network        netx.Network     // transport implementation
	BindAddr       string           // e.g. ":0" to choose random port
	Bootstraps     []netx.Addr      // known peers to try on startup
	Protocol       string           // protocol version string
	Logger         telemetry.Logger // system logger
	Debug          bool             // flag for showing hidden logs to debug
	IsSeed         bool             // if true, this node will keep NAT registry & relay
}

type peer struct {
	rateMu       sync.Mutex
	rateLast     time.Time
	rateTokens   float64
	id           string
	addr         netx.Addr
	observedAddr netx.Addr
	conn         io.ReadWriteCloser // stream type for Noise secure encrypted
	writer       *json.Encoder
	reader       *envelopeDecoder

	writerDone chan struct{}
	sendCh     chan proto.Envelope
	ctx        context.Context
	cancel     context.CancelFunc
	once       sync.Once

	name    string
	userPub ed25519.PublicKey
	userID  string
}

// PeerSnapshot is a read-only view of a connected peer.
type PeerSnapshot struct {
	NetworkID string // Noise hex ID (p.id)
	Name      string // p.name from Identify
	UserID    string // hex(ed25519 pub) if known
	Addr      string // listen address string
}

type Node struct {
	lifeMu            sync.Mutex
	sessions          sync.WaitGroup
	workers           sync.WaitGroup
	slots             chan struct{}
	referrals         chan referral
	dialing           map[netx.Addr]string
	discoveryPrefixes []netip.Prefix
	cfg               NodeConfig
	id                *Identity
	addr              netx.Addr

	mu            sync.RWMutex
	peers         map[string]*peer
	peersByUserID map[string]*peer

	ctx    context.Context
	cancel context.CancelFunc

	incoming    chan proto.Envelope // gossip and other messages
	natByUserID map[string]*peer    // only meaningful when cfg.IsSeed == true

	events chan Event
	seen   *seenCache

	dht *dht.DHT
}

func NewNode(cfg NodeConfig) (*Node, error) {
	if cfg.MaxConnections <= 0 {
		cfg.MaxConnections = DefaultMaxConnections
	}
	if cfg.MessageTimeout <= 0 {
		cfg.MessageTimeout = 2 * time.Minute
	}
	// Copy caller-owned trust configuration: never race mutable maps/slices.
	pins := make(map[netx.Addr]string, len(cfg.PeerPins))
	for addr, id := range cfg.PeerPins {
		if !validPeerID(id) {
			return nil, fmt.Errorf("invalid peer pin for %s", addr)
		}
		pins[addr] = id
	}
	cfg.PeerPins = pins
	var prefixes []netip.Prefix
	for _, s := range cfg.DiscoveryCIDRs {
		p, err := netip.ParsePrefix(s)
		if err != nil {
			return nil, err
		}
		prefixes = append(prefixes, p)
	}
	if cfg.Logger == nil {
		cfg.Logger = log.Default()
	}
	id, err := NewIdentity()
	if err != nil {
		return nil, err
	}
	dd, err := dht.New(id.ID, dht.WithStore(""))
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	n := &Node{
		cfg:               cfg,
		slots:             make(chan struct{}, cfg.MaxConnections),
		referrals:         make(chan referral, 64),
		dialing:           make(map[netx.Addr]string),
		discoveryPrefixes: prefixes,
		id:                id,
		peers:             make(map[string]*peer),
		peersByUserID:     make(map[string]*peer),
		ctx:               ctx,
		cancel:            cancel,
		incoming:          make(chan proto.Envelope, 128),
		events:            make(chan Event, 128),
		seen:              newSeenCache(gossipLifetime + gossipFutureSkew + time.Second),
		dht:               dd,
	}
	if cfg.IsSeed {
		n.natByUserID = make(map[string]*peer)
	}
	return n, nil
}

// TODO: clean methods
func (n *Node) ID() string                      { return n.id.ID }
func (n *Node) Identity() *Identity             { return n.id }
func (n *Node) ListenAddr() netx.Addr           { return n.addr }
func (n *Node) Incoming() <-chan proto.Envelope { return n.incoming }
func (n *Node) Name() string                    { return n.cfg.Name }
func (n *Node) Events() <-chan Event            { return n.events }
func (n *Node) Debug() bool                     { return n.cfg.Debug }

// Start brings the node online.
func (n *Node) Start() error {
	addr, err := n.cfg.Network.Listen(n.cfg.BindAddr)
	if err != nil {
		return err
	}
	n.addr = addr
	n.Logf("listening on %s, peerID=%s", n.addr, n.id.ID)

	n.launchWorker(n.acceptLoop)
	n.launchWorker(n.referralLoop)

	n.coldStartDHTBootstrap()

	n.launchWorker(n.discoveryLoop)

	n.startDHTBootstrapLoop(DefaultDHTBootstrapConfig())

	return nil
}

// Stop shuts down the node.
func (n *Node) Stop() error {
	n.lifeMu.Lock()
	n.cancel()
	n.lifeMu.Unlock()
	n.mu.RLock()
	peers := make([]*peer, 0, len(n.peers))
	for _, p := range n.peers {
		peers = append(peers, p)
	}
	n.mu.RUnlock()
	for _, p := range peers {
		_ = p.conn.Close()
	}
	err := n.cfg.Network.Close()
	n.sessions.Wait()
	n.workers.Wait()
	return err
}

func (n *Node) emit(e Event) {
	select {
	case n.events <- e:
	default:
		// drop to avoid deadlock; optionally log once
	}
}

// Broadcast sends a gossip mesage to all connected peers.
func (n *Node) Broadcast(g proto.Gossip) {
	env := proto.Envelope{
		Type:    proto.MsgGossip,
		FromID:  n.id.ID,
		Payload: proto.MustMarshal(g),
	}
	signed, err := n.signGossip(env)
	if err != nil {
		return
	}
	if !validEnvelopeSize(signed) {
		return
	}
	n.seen.Seen(n.ID() + "/" + g.ID)
	n.relay(n.id.ID, signed)
}

func (n *Node) relay(originID string, env proto.Envelope) {
	n.mu.RLock()
	peers := make([]*peer, 0, len(n.peers))
	for id, p := range n.peers {
		if id == originID || p == nil {
			continue
		}
		peers = append(peers, p)
	}
	n.mu.RUnlock()

	for _, p := range peers {
		n.sendAsync(p, env)
	}
}

// Done signals node shutdown to discovery workers.
func (n *Node) Done() <-chan struct{} { return n.ctx.Done() }
