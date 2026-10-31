package parknode

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"crypto/ed25519"
	"p2p-park/engine"
	"p2p-park/internal/app/grants"
	"p2p-park/internal/app/points"
	"p2p-park/internal/app/quiz"
	"p2p-park/internal/appdata"
	"p2p-park/internal/crypto/channel"
	proto "p2p-park/internal/gameproto"
	"p2p-park/internal/paths"
	"p2p-park/internal/storage/grantsbolt"
	"p2p-park/internal/telemetry"
)

type App struct {
	replayMu        sync.Mutex
	encryptedReplay map[[32]byte]time.Time
	replayNextSweep time.Time
	cfg             Config
	ui              Printer
	logger          telemetry.Logger

	Network  *engine.Engine
	net      *parkNetwork
	identity *engine.Identity
	work     sync.WaitGroup
	lifeMu   sync.Mutex
	started  bool
	closed   bool

	stopOnce sync.Once

	// Points engine
	Points *points.Engine

	// Grant-based scoring (quiz awards)
	Ledger     *grants.Ledger
	GrantStore grants.Store

	// Quiz engine
	Quiz *quiz.Engine

	// Encrypted channels
	encMu       sync.RWMutex
	encChannels map[string]channel.ChannelKey
}

func New(cfg Config, logger *log.Logger) (*App, error) {
	if cfg.NetworkID == "" {
		cfg.NetworkID = DefaultNetworkID
	}
	identity := cfg.Identity
	var err error
	if identity == nil {
		identity, err = engine.GenerateIdentity()
		if err != nil {
			return nil, err
		}
	}
	stateDir := cfg.DiscoveryStateDir
	if stateDir == "" {
		stateDir = appdata.Dir()
	}
	n, err := engine.New(engine.Config{Debug: cfg.Debug, Logger: logger, Name: cfg.Name, Bind: cfg.Bind, NetworkID: cfg.NetworkID, Application: "p2p-park.0.3.0", Identity: identity, Bootstraps: cfg.Bootstraps, PeerPins: cfg.PeerPins, DiscoveryCIDRs: cfg.DiscoveryCIDRs, Admission: cfg.Admission, ApprovedPeers: cfg.ApprovedPeers, LANDiscovery: !cfg.DisableLAN, LANPeerStorePath: filepath.Join(stateDir, "peers.json"), PeerStorePath: filepath.Join(stateDir, "dht.json"), Rendezvous: cfg.IsSeed})
	if err != nil {
		return nil, err
	}
	pub := ed25519.PublicKey(identity.SigningPublicKey())
	pe := points.NewEngineWithSigner(cfg.Name, identity, pub)
	qe := quiz.NewEngineWithSigner(cfg.Name, identity, pub)
	ld := grants.NewLedger()

	// Persistent grant store (BoltDB)
	dataDir := cfg.DataDir
	if dataDir == "" {
		dataDir = paths.DefaultDataDir()
	}
	if dir, err := paths.EnsureDir(dataDir); err == nil {
		dataDir = dir
	}
	dbPath := filepath.Join(dataDir, "grants.bolt")
	gs, err := grantsbolt.Open(dbPath)
	if err != nil {
		_ = n.Close()
		return nil, err
	}
	// Replay persisted grants into the in-memory ledger.
	_ = gs.LoadAll(func(g proto.QuizGrant) error {
		ld.ApplyGrant(g)
		return nil
	})
	ld.NoteName(identity.SigningID(), cfg.Name)

	return &App{
		cfg:         cfg,
		logger:      logger,
		ui:          NewStdPrinter(os.Stdout),
		Network:     n,
		net:         &parkNetwork{engine: n},
		identity:    identity,
		Points:      pe,
		Quiz:        qe,
		Ledger:      ld,
		GrantStore:  gs,
		encChannels: make(map[string]channel.ChannelKey),
	}, nil
}

// DefaultNetworkID is public demo configuration, never a membership secret.
const DefaultNetworkID = "ed2d71df168e78e79f28854f86f7f57ed8db56d99e531ae07fca45f3fd387ce2"

func (a *App) Start() error {
	a.lifeMu.Lock()
	defer a.lifeMu.Unlock()
	if a.closed {
		return engine.ErrClosed
	}
	if a.started {
		return engine.ErrAlreadyStarted
	}
	if err := a.Network.Start(context.Background()); err != nil {
		return err
	}
	a.started = true
	a.work.Add(1)
	go func() { defer a.work.Done(); a.serve() }()
	if snap, err := a.Points.SnapshotSelf(); err == nil {
		a.broadcastPoints(snap)
	}
	return nil
}

// Run owns only CLI activity; application networking starts with Start, including
// for non-interactive consumers. Signal cancellation joins through StopAll.
func (a *App) Run(ctx context.Context) error {
	PrintBanner(a.ui, a.Network, a.cfg.Name)
	go a.readStdin(ctx) // process-owned stdin cannot be cancelled portably
	select {
	case <-ctx.Done():
	case <-a.Network.Done():
	}
	a.StopAll()
	return nil
}
func (a *App) serve() {
	for {
		select {
		case <-a.Network.Done():
			return
		case ev, ok := <-a.Network.Events():
			if !ok {
				return
			}
			switch ev.Type {
			case engine.EventPeerConnected:
				a.ui.Printf("[NET] peer connected: %s (%s)\n", ev.Peer.Name, ev.Peer.Address)
				a.initiateGrantSync(ev.Peer.ID)
				if snap, err := a.Points.SnapshotSelf(); err == nil {
					a.broadcastPoints(snap)
				}
			case engine.EventDiscoveryError:
				a.logf("LAN discovery: %s", ev.Error)
			case engine.EventPeerDisconnected:
				a.ui.Printf("[NET] peer disconnected: %s\n", ev.Peer.ID)
			}
		case msg, ok := <-a.Network.Messages():
			if !ok {
				return
			}
			a.handleMessage(msg)
		}
	}
}
func (a *App) StopAll() {
	a.stopOnce.Do(func() {
		a.lifeMu.Lock()
		a.closed = true
		a.lifeMu.Unlock()
		_ = a.Network.Close()
		a.work.Wait()
		if a.GrantStore != nil {
			_ = a.GrantStore.Close()
		}
	})
}

func (a *App) logf(format string, args ...any) {
	if a.logger != nil {
		a.logger.Printf(format, args...)
		return
	}
	log.Printf(format+"\n", args...)
}

func (a *App) broadcastPoints(snap proto.SignedPointsSnapshot) { a.net.broadcast("points", snap) }
func (a *App) userIDHex() string                               { return a.identity.SigningID() }

// For “settle” waits after quit
func sleepBrief() { time.Sleep(100 * time.Millisecond) }
