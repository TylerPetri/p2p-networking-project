package parknode

import (
	"context"
	"io"
	"log"
	"testing"
	"time"

	"p2p-park/engine"
)

func TestEngineAndParkFailClosedAcrossProtocols(t *testing.T) {
	t.Setenv("P2P_PARK_DATA_DIR", t.TempDir())
	app, err := New(Config{Name: "park", Bind: "127.0.0.1:0", DataDir: t.TempDir(), DisableLAN: true}, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	defer app.StopAll()
	if err := app.Start(); err != nil {
		t.Fatal(err)
	}
	network, _ := engine.NewNetworkID()
	n, err := engine.New(engine.Config{NetworkID: network, Application: "compat"})
	if err != nil {
		t.Fatal(err)
	}
	defer n.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := n.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := n.Connect(ctx, string(app.Node.ListenAddr())); err == nil {
		t.Fatal("incompatible engine/Park protocols connected")
	}
	if len(n.Peers()) != 0 || app.Node.PeerCount() != 0 {
		t.Fatal("incompatible peer admitted")
	}
}
