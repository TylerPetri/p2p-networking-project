package engine

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type invitationVector struct {
	Name       string `json:"name"`
	URI        string `json:"uri"`
	JSON       string `json:"json"`
	ParseError string `json:"parse_error"`
	Scenario   string `json:"join_scenario"`
}
type invitationCorpus struct {
	ReferenceUnix int64                        `json:"reference_unix"`
	Identities    map[string]map[string]string `json:"identities"`
	Cases         []invitationVector           `json:"cases"`
}

func loadInvitationCorpus(t *testing.T) invitationCorpus {
	t.Helper()
	b, err := os.ReadFile("testdata/invitations/v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var c invitationCorpus
	if err = json.Unmarshal(b, &c); err != nil {
		t.Fatal(err)
	}
	return c
}
func vectorError(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, ErrInvitationExpired):
		return "expired"
	case errors.Is(err, ErrUnsupportedInvitationVersion):
		return "unsupported"
	case errors.Is(err, ErrInvalidInvitation):
		return "invalid"
	default:
		return err.Error()
	}
}
func TestInvitationGoldenVectors(t *testing.T) {
	c := loadInvitationCorpus(t)
	now := time.Unix(c.ReferenceUnix, 0)
	for _, v := range c.Cases {
		t.Run(v.Name, func(t *testing.T) {
			i, err := parseInvitationAt(v.URI, now)
			if got := vectorError(err); got != v.ParseError {
				t.Fatalf("error=%q want=%q", got, v.ParseError)
			}
			if err != nil {
				return
			}
			encoded, err := i.encodeAt(now)
			if err != nil || encoded != v.URI {
				t.Fatalf("canonical bytes changed: %v", err)
			}
			raw, err := json.Marshal(i)
			if err != nil || string(raw) != v.JSON {
				t.Fatal("canonical JSON changed", err)
			}
			// Encoder sorts a copy; shuffled inputs cannot change v1 bytes.
			if len(i.Bootstraps) > 1 {
				i.Bootstraps[0], i.Bootstraps[1] = i.Bootstraps[1], i.Bootstraps[0]
				reordered, err := i.encodeAt(now)
				if err != nil || reordered != v.URI {
					t.Fatal("order-dependent encoding", err)
				}
			}
		})
	}
}

// Public, deliberately reproducible TEST ONLY material. Never use these keys
// outside local fixtures. Private material is generated only in temporary files.
func vectorIdentity(t *testing.T, index byte) *Identity {
	t.Helper()
	seed, noise := make([]byte, 32), make([]byte, 32)
	for i := range seed {
		seed[i] = byte(i) + index
		noise[i] = byte(96+i) + index
	}
	b, err := json.Marshal(diskIdentity{Version: 1, SigningSeed: seed, NoisePrivate: noise})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "test-only-identity.json")
	if err = os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	id, err := LoadOrCreateIdentity(path)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
func vectorNode(t *testing.T, id *Identity, networkID string) *Engine {
	t.Helper()
	n, err := New(Config{NetworkID: networkID, Application: "vector-test", Identity: id})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = n.Close() })
	if err = n.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	return n
}
func TestInvitationFixtureLiveContracts(t *testing.T) {
	c := loadInvitationCorpus(t)
	a, b := vectorIdentity(t, 1), vectorIdentity(t, 2)
	if a.PeerID() != c.Identities["a"]["peer_id"] || a.SigningID() != c.Identities["a"]["signing_id"] || b.PeerID() != c.Identities["b"]["peer_id"] || b.SigningID() != c.Identities["b"]["signing_id"] {
		t.Fatal("fixture public identities drifted")
	}
	// Bind ephemeral ports for test isolation; retain fixture identities and domains.
	for _, v := range c.Cases {
		if v.Scenario == "" {
			continue
		}
		t.Run(v.Name, func(t *testing.T) {
			server := vectorNode(t, a, strings.Repeat("01", 32))
			client := vectorNode(t, b, strings.Repeat("01", 32))
			i, err := parseInvitationAt(v.URI, time.Unix(c.ReferenceUnix, 0))
			if err != nil {
				t.Fatal(err)
			}
			i.Bootstraps[0].Address = server.Address()
			options := JoinOptions{AllowedCIDRs: []string{"127.0.0.1/32"}}
			if v.Scenario == "scope-denied" {
				options = JoinOptions{}
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			result, err := client.Join(ctx, i, options)
			switch v.Scenario {
			case "success":
				if err != nil || len(result.Connected) != 1 {
					t.Fatal(result, err)
				}
				if err = client.Send(ctx, server.PeerID(), "probe", []byte("fixture-message")); err != nil {
					t.Fatal(err)
				}
				select {
				case m := <-server.Messages():
					if hex.EncodeToString(m.SigningKey) != b.SigningID() || string(m.Data) != "fixture-message" {
						t.Fatal("unverified message")
					}
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
				if _, err = client.Join(ctx, i, options); err != nil {
					t.Fatal("existing authenticated session not reusable", err)
				}
			case "wrong-noise", "wrong-signing":
				if !errors.Is(err, ErrBootstrapUnavailable) || len(client.Peers()) != 0 {
					t.Fatal("identity mismatch admitted", err)
				}
			case "wrong-network", "scope-denied":
				if !errors.Is(err, ErrInvalidInvitation) || len(client.Peers()) != 0 {
					t.Fatal("preflight bypass", err)
				}
			default:
				t.Fatal("unknown scenario")
			}
		})
	}
}
