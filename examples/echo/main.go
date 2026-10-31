// A three-node example using only the public engine API, with no game packages.
package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"p2p-park/engine"
)

func run() error {
	dir, err := os.MkdirTemp("", "engine-echo-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	network, err := engine.NewNetworkID()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var nodes []*engine.Engine
	defer func() {
		for _, n := range nodes {
			_ = n.Close()
		}
	}()
	for i := 0; i < 3; i++ {
		id, err := engine.LoadOrCreateIdentity(filepath.Join(dir, fmt.Sprintf("identity-%d.json", i)))
		if err != nil {
			return err
		}
		n, err := engine.New(engine.Config{NetworkID: network, Application: "echo", Identity: id, Name: fmt.Sprintf("node-%d", i)})
		if err != nil {
			return err
		}
		nodes = append(nodes, n)
		if err := n.Start(ctx); err != nil {
			return err
		}
	}
	for _, n := range nodes[1:] {
		if err := n.Connect(ctx, nodes[0].Address()); err != nil {
			return err
		}
	}
	for len(nodes[0].Peers()) < 2 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Millisecond):
		}
	}
	if err := nodes[1].Publish(ctx, "greeting", []byte("hello from an application-independent engine")); err != nil {
		return err
	}
	select {
	case msg := <-nodes[2].Messages():
		if hex.EncodeToString(msg.SigningKey) != nodes[1].SigningID() || msg.ClaimedPeerID != nodes[1].PeerID() || msg.Topic != "greeting" || string(msg.Data) != "hello from an application-independent engine" {
			return fmt.Errorf("unexpected authenticated message")
		}
		fmt.Printf("PASS: three engine nodes; verified origin %s; payload: %s\n", hex.EncodeToString(msg.SigningKey), string(msg.Data))
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
