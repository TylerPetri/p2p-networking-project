package parknode

import (
	"encoding/hex"
	"encoding/json"
	"time"

	"p2p-park/engine"
	"p2p-park/internal/crypto/channel"
	proto "p2p-park/internal/gameproto"
)

func (a *App) handleMessage(m engine.Message) {
	switch m.Topic {
	case "global":
		a.handleGlobalChat(m)
	case "encrypted":
		a.handleEncrypted(m)
	}
}

func (a *App) handleGlobalChat(m engine.Message) {
	var chat proto.ChatMessage
	if err := json.Unmarshal(m.Data, &chat); err != nil {
		a.ui.Printf("[CHAT] bad chat payload: %v\n", err)
		return
	}

	if len(chat.Text) > 4096 {
		return
	}
	ts := time.Unix(chat.Timestamp, 0).Format("15:04:05")

	sender := a.net.displayName(hex.EncodeToString(m.SigningKey))
	if sender == "" {
		sender = shortID(hex.EncodeToString(m.SigningKey))
	}
	colored := formatName(sender, hex.EncodeToString(m.SigningKey))

	a.ui.Printf("%s[%s]%s %s: %s\n", ansiDim, ts, ansiReset, colored, chat.Text)
}

func (a *App) handleEncrypted(m engine.Message) {
	var em proto.EncryptedMessage
	if json.Unmarshal(m.Data, &em) != nil || len(em.Channel) == 0 || len(em.Channel) > 124 {
		return
	}
	chName := em.Channel
	a.encMu.RLock()
	key, ok := a.encChannels[chName]
	a.encMu.RUnlock()
	if !ok {
		return
	}
	pt, err := channel.DecryptForChannel(key, chName, em.Nonce, em.Ciphertext)
	if err != nil {
		a.ui.Printf("[enc:%s] decrypt failed: %v\n", chName, err)
		return
	}

	if !a.acceptCiphertext(key, em.Nonce, em.Ciphertext) {
		return
	}
	var payload map[string]any
	if err := json.Unmarshal(pt, &payload); err != nil {
		a.ui.Printf("[enc:%s] %s\n", chName, string(pt))
		return
	}

	from := a.net.displayName(hex.EncodeToString(m.SigningKey))
	text, _ := payload["text"].(string)

	if from == "" {
		from = a.net.displayName(hex.EncodeToString(m.SigningKey))
		if from == "" {
			from = shortID(hex.EncodeToString(m.SigningKey))
		}
	}

	a.ui.Printf("[enc:%s] %s: %s\n", chName, from, text)
}
