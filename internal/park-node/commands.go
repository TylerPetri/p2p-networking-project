package parknode

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
	"time"

	"p2p-park/internal/crypto/channel"
	proto "p2p-park/internal/gameproto"
)

func (a *App) readStdin(_ any) {
	sc := bufio.NewScanner(os.Stdin)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		a.handleCommand(line)
	}
}

func (a *App) handleCommand(line string) {
	switch {
	case strings.HasPrefix(line, "/quit"), strings.HasPrefix(line, "/exit"):
		a.ui.Println("quitting...")
		a.StopAll()
		sleepBrief()
		os.Exit(0)

	case line == "/me":
		userID := a.userIDHex()
		networkID := a.Network.PeerID() // Noise hex ID

		a.ui.Println()
		a.ui.Println("== You ==")
		a.ui.Printf("  Name:       %s\n", a.cfg.Name)
		a.ui.Printf("  UserID:     %s\n", userID)
		a.ui.Printf("  PeerID:     %s\n", networkID)
		a.ui.Printf("  Listen on:  %s\n", a.Network.Address())
		a.ui.Printf("  Peers:      %d\n", len(a.Network.Peers()))
		a.ui.Println()

	case line == "/peers":
		peers := a.Network.Peers()
		if len(peers) == 0 {
			a.ui.Println("no peers connected")
			return
		}

		a.ui.Println()
		a.ui.Println("Connected peers:")
		a.ui.Printf("%-16s  %-10s  %-10s  %s\n", "NAME", "USERID", "NETID", "ADDR")
		a.ui.Printf("%-16s  %-10s  %-10s  %s\n", "----", "------", "-----", "----")

		for _, p := range peers {
			name := p.Name
			if name == "" {
				name = shortID(p.ID)
			}
			coloredName := formatName(name, p.ID)

			shortUser := "-"
			if p.SigningID != "" {
				shortUser = shortID(p.SigningID)
			}
			shortNet := shortID(p.ID)

			a.ui.Printf("%-16s  %-10s  %-10s  %s\n", coloredName, shortUser, shortNet, p.Address)
		}
		a.ui.Println()

	case strings.HasPrefix(line, "/say "):
		msg := strings.TrimSpace(strings.TrimPrefix(line, "/say"))
		chat := proto.ChatMessage{
			Text:      msg,
			From:      a.cfg.Name,
			Timestamp: time.Now().Unix(),
		}
		body, _ := json.Marshal(chat)
		_ = a.net.publish("global", body)

	case strings.HasPrefix(line, "/mkchan "):
		chName := strings.TrimSpace(strings.TrimPrefix(line, "/mkchan"))
		if chName == "" {
			a.ui.Println("usage: /mkchan <name>")
			return
		}
		k, err := channel.NewRandomKey()
		if err != nil {
			a.ui.Printf("mkchan: %v\n", err)
			return
		}
		a.encMu.Lock()
		a.encChannels[chName] = k
		a.encMu.Unlock()

		a.ui.Printf("[CHAN] created channel %q\n", chName)
		a.ui.Printf("[CHAN] share this key with others:\n  %s\n", channel.KeyToHex(k))

	case strings.HasPrefix(line, "/joinchan "):
		parts := strings.Fields(line)
		if len(parts) != 3 {
			a.ui.Println("usage: /joinchan <name> <hexkey>")
			return
		}
		chName := parts[1]
		hexKey := parts[2]

		k, err := channel.ParseKeyHex(hexKey)
		if err != nil {
			a.ui.Printf("joinchan: %v\n", err)
			return
		}
		a.encMu.Lock()
		a.encChannels[chName] = k
		a.encMu.Unlock()

		a.ui.Printf("[CHAN] joined channel %q\n", chName)

	case strings.HasPrefix(line, "/encsay "):
		rest := strings.TrimSpace(strings.TrimPrefix(line, "/encsay"))
		parts := strings.Fields(rest)
		if len(parts) < 2 {
			a.ui.Println("usage: /encsay <channel> <message>")
			return
		}
		chName := parts[0]
		msg := strings.TrimSpace(strings.TrimPrefix(rest, chName))

		a.sendEncrypted(chName, msg)

	default:
		a.ui.Println("unknown command")
		PrintCommands(a.ui)
	}
}

func (a *App) sendEncrypted(chName, msg string) {
	a.encMu.RLock()
	key, ok := a.encChannels[chName]
	a.encMu.RUnlock()
	if !ok {
		a.ui.Printf("not joined to channel %q\n", chName)
		return
	}

	plaintext, _ := json.Marshal(map[string]any{
		"text":      msg,
		"from":      a.cfg.Name,
		"channel":   chName,
		"timestamp": time.Now().Unix(),
	})

	nonce, ct, err := channel.EncryptForChannel(key, chName, plaintext)
	if err != nil {
		a.ui.Printf("encrypt failed: %v\n", err)
		return
	}

	em := proto.EncryptedMessage{
		Channel:    chName,
		Nonce:      nonce,
		Ciphertext: ct,
	}

	body, _ := json.Marshal(em)
	_ = a.net.publish("encrypted", body)
}
