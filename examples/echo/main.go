// Three local nodes join through public invitations, survive bootstrap loss,
// and reconnect without game packages, accounts, shared passwords or a website.
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
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var nodes []*engine.Engine
	defer func() {
		for _, n := range nodes {
			_ = n.Close()
		}
	}()
	create := func(index int, networkID string) (*engine.Engine, error) {
		id, err := engine.LoadOrCreateIdentity(filepath.Join(dir, fmt.Sprintf("identity-%d.json", index)))
		if err != nil {
			return nil, err
		}
		n, err := engine.New(engine.Config{NetworkID: networkID, Application: "echo", Identity: id, Name: fmt.Sprintf("node-%d", index)})
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, n)
		if err = n.Start(ctx); err != nil {
			return nil, err
		}
		return n, nil
	}
	networkID, err := engine.NewNetworkID()
	if err != nil {
		return err
	}
	a, err := create(0, networkID)
	if err != nil {
		return err
	}
	invitation, err := a.Invitation(nil, time.Now().Add(time.Hour))
	if err != nil {
		return err
	}
	uri, err := invitation.Encode()
	if err != nil {
		return err
	}
	fmt.Printf("Public loopback invitation (not an Internet endpoint): %s\n", uri)
	parsed, err := engine.ParseInvitation(uri)
	if err != nil {
		return err
	}
	// Receiving operator explicitly approves local destinations. Public QR/link data
	// cannot enable access to the recipient's private or loopback networks itself.
	policy := engine.JoinOptions{AllowedCIDRs: []string{"127.0.0.1/32"}}
	b, err := create(1, parsed.NetworkID)
	if err != nil {
		return err
	}
	if _, err = b.Join(ctx, parsed, policy); err != nil {
		return err
	}
	c, err := create(2, parsed.NetworkID)
	if err != nil {
		return err
	}
	if _, err = c.Join(ctx, parsed, policy); err != nil {
		return err
	}
	connected := func(n *engine.Engine, id string) bool {
		for _, p := range n.Peers() {
			if p.ID == id {
				return true
			}
		}
		return false
	}
	for !connected(b, c.PeerID()) || !connected(c, b.PeerID()) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Millisecond):
		}
	}
	backup, err := b.Invitation(nil, time.Now().Add(time.Hour))
	if err != nil {
		return err
	}
	invitation.Bootstraps = append(invitation.Bootstraps, backup.Bootstraps...)
	backupURI, err := invitation.Encode()
	if err != nil {
		return err
	}
	if err = a.Close(); err != nil {
		return err
	}
	if err = b.Send(ctx, c.PeerID(), "greeting", []byte("communication survives bootstrap loss")); err != nil {
		return err
	}
	if err = expect(ctx, c, b.SigningID(), "communication survives bootstrap loss"); err != nil {
		return err
	}
	// Restore the original public identity, using another reachable bootstrap.
	restart, err := create(0, parsed.NetworkID)
	if err != nil {
		return err
	}
	fallback, err := engine.ParseInvitation(backupURI)
	if err != nil {
		return err
	}
	result, err := restart.Join(ctx, fallback, policy)
	if err != nil {
		return err
	}
	if len(result.Connected) == 0 {
		return fmt.Errorf("no fallback bootstrap authenticated")
	}
	if err = restart.Send(ctx, b.PeerID(), "greeting", []byte("reconnected through surviving peer")); err != nil {
		return err
	}
	if err = expect(ctx, b, restart.SigningID(), "reconnected through surviving peer"); err != nil {
		return err
	}
	fmt.Println("PASS: invitation join, three-node discovery, bootstrap loss, authenticated messages and fallback reconnection.")
	return nil
}
func expect(ctx context.Context, n *engine.Engine, signingID, payload string) error {
	select {
	case msg, ok := <-n.Messages():
		if !ok || hex.EncodeToString(msg.SigningKey) != signingID || msg.Topic != "greeting" || string(msg.Data) != payload {
			return fmt.Errorf("unexpected authenticated message")
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
