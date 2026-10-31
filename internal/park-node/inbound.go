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
	case "grants.summary":
		a.handleGrantSyncSummary(m)
	case "grants.request":
		a.handleGrantSyncRequest(m)
	case "grants.response":
		a.handleGrantSyncResponse(m)
	case "global":
		a.handleGlobalChat(m)
	case "points":
		a.handlePoints(m)
	case "quiz":
		a.handleQuiz(m)
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

func (a *App) handlePoints(m engine.Message) {
	var signed proto.SignedPointsSnapshot
	if err := json.Unmarshal(m.Data, &signed); err != nil {
		a.ui.Printf("[POINTS] bad snapshot: %v\n", err)
		return
	}
	_ = a.Points.ApplyRemote(signed)
}

func (a *App) handleQuiz(m engine.Message) {
	var qw proto.QuizWire
	if err := json.Unmarshal(m.Data, &qw); err != nil {
		a.ui.Printf("[QUIZ] bad payload: %v\n", err)
		return
	}

	switch qw.Kind {
	case "open":
		if qw.Open == nil {
			return
		}
		if a.Quiz.ObserveOpen(*qw.Open) {
			// Best-effort name learning
			creator := qw.Open.Open.CreatorID
			a.Ledger.NoteName(creator, a.net.displayName(creator))
			a.ui.Printf("[QUIZ] open %s (%d pts): %s\n", shortID(qw.Open.Open.QuizID), qw.Open.Open.Points, qw.Open.Open.Question)
		}

	case "answer":
		if qw.Answer == nil {
			return
		}
		ans := *qw.Answer

		answererUserID := hex.EncodeToString(m.SigningKey)
		if answererUserID == "" {
			return
		}

		grant, correct, authoritative, already := a.Quiz.TryGrade(ans.QuizID, answererUserID, ans.Answer)
		_ = already
		if !authoritative {
			return
		}

		if correct && !a.acceptGrant(grant) {
			a.logf("issued grant could not be persisted; not broadcasting")
			return
		}
		// Direct UX result is authenticated by the engine gossip signature.
		res := proto.QuizResult{QuizID: ans.QuizID, PlayerID: answererUserID, Correct: correct, Delta: 0}
		if correct {
			res.Delta = grant.Points
		}
		resWire := proto.QuizWire{Kind: "result", Result: &res}
		resBody, _ := json.Marshal(resWire)
		_ = a.net.reply(m, "quiz", resBody)

		if correct {
			grantWire := proto.QuizWire{Kind: "grant", Grant: &grant}
			grantBody, _ := json.Marshal(grantWire)
			_ = a.net.publish("quiz", grantBody)
		}

	case "grant":
		if qw.Grant == nil {
			return
		}
		g := *qw.Grant

		if a.acceptGrant(g) {

			wire := proto.QuizWire{Kind: "grant", Grant: &g}
			body, _ := json.Marshal(wire)
			_ = a.net.publish("quiz", body)

			// announce if it's about us
			if g.RecipientID == a.userIDHex() {
				a.ui.Printf("[POINTS] +%d (quiz grant) => total %d\n", g.Points, a.Ledger.Total(g.RecipientID))
			}
		}

	case "result":
		if qw.Result == nil {
			return
		}
		if !a.Quiz.IsCreator(qw.Result.QuizID, hex.EncodeToString(m.SigningKey)) {
			return
		}
		if qw.Result.PlayerID != a.userIDHex() {
			return
		}
		if qw.Result.Correct {
			a.ui.Printf("[QUIZ] correct! +%d points (quiz %s)\n", qw.Result.Delta, shortID(qw.Result.QuizID))
		} else {
			a.ui.Printf("[QUIZ] incorrect (quiz %s)\n", shortID(qw.Result.QuizID))
		}
	}
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
