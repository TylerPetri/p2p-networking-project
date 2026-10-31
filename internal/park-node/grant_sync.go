package parknode

import (
	"encoding/json"
	"time"

	"p2p-park/engine"
	"p2p-park/internal/app/grants"
	proto "p2p-park/internal/gameproto"
)

const (
	grantSyncRecentIDs = 64
	grantSyncLimit     = 128
	grantSyncOverlap   = 5 * time.Minute
)

func (a *App) initiateGrantSync(peerID string) {
	if a.GrantStore == nil {
		return
	}
	maxTS, err := a.GrantStore.MaxTimestamp()
	if err != nil {
		a.logf("grant store max ts: %v", err)
		return
	}
	recent, err := a.GrantStore.RecentGrantIDs(grantSyncRecentIDs)
	if err != nil {
		a.logf("grant store recent ids: %v", err)
		return
	}

	sum := proto.GrantSyncSummary{
		MaxTimestamp:   maxTS,
		RecentGrantIDs: recent,
	}

	body, _ := json.Marshal(sum)
	_ = a.net.send(peerID, "grants.summary", body)
}

func (a *App) handleGrantSyncSummary(m engine.Message) {
	if a.GrantStore == nil {
		return
	}
	var sum proto.GrantSyncSummary
	if err := json.Unmarshal(m.Data, &sum); err != nil {
		return
	}

	localMax, err := a.GrantStore.MaxTimestamp()
	if err != nil {
		return
	}

	// If remote is ahead of us, request missing grants.
	if sum.MaxTimestamp > localMax {
		since := localMax - int64(grantSyncOverlap.Seconds())
		if since < 0 {
			since = 0
		}
		req := proto.GrantSyncRequest{SinceTimestamp: since, Limit: grantSyncLimit}
		body, _ := json.Marshal(req)
		_ = a.net.reply(m, "grants.request", body)
	}

}

func (a *App) handleGrantSyncRequest(m engine.Message) {
	if a.GrantStore == nil {
		return
	}
	var req proto.GrantSyncRequest
	if err := json.Unmarshal(m.Data, &req); err != nil {
		return
	}
	if req.Limit <= 0 || req.Limit > grantSyncLimit {
		req.Limit = grantSyncLimit
	}
	grs, err := a.GrantStore.ListSince(req.SinceTimestamp, req.Limit)
	if err != nil {
		return
	}
	resp := proto.GrantSyncResponse{Grants: grs}
	body, _ := json.Marshal(resp)
	_ = a.net.reply(m, "grants.response", body)
}

func (a *App) handleGrantSyncResponse(m engine.Message) {
	if a.GrantStore == nil {
		return
	}
	var resp proto.GrantSyncResponse
	if err := json.Unmarshal(m.Data, &resp); err != nil {
		return
	}

	if len(resp.Grants) > grantSyncLimit {
		return
	}
	for _, g := range resp.Grants {
		// verify first to avoid persisting garbage
		if err := grants.VerifyGrant(g); err != nil {
			continue
		}
		a.acceptGrant(g)
	}
}

func (a *App) acceptGrant(g proto.QuizGrant) bool {
	if grants.VerifyGrant(g) != nil {
		return false
	}
	if a.GrantStore != nil {
		if _, err := a.GrantStore.Put(g); err != nil {
			a.logf("grant persistence rejected: %v", err)
			return false
		}
	}
	return a.Ledger.ApplyGrant(g)
}
