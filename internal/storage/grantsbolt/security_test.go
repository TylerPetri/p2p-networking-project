package grantsbolt

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
	"p2p-park/internal/app/grants"
	"p2p-park/internal/proto"
)

func testGrant() proto.QuizGrant {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	g := proto.QuizGrant{GrantID: strings.Repeat("01", 16), QuizID: strings.Repeat("02", 16), GrantorID: hex.EncodeToString(pub), RecipientID: hex.EncodeToString(pub), Points: 1, Timestamp: time.Now().Unix()}
	b, _ := proto.EncodeQuizGrantCanonical(g)
	g.Signature = ed25519.Sign(priv, b)
	return g
}
func TestPersistentGrantQuotaAndReplay(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "grants.bolt"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	g := testGrant()
	if ok, err := s.Put(g); !ok || err != nil {
		t.Fatal(ok, err)
	}
	if ok, err := s.Put(g); ok || err != nil {
		t.Fatal("persistent replay", ok, err)
	}
	// Fill only the index in one transaction so this boundary test does not
	// require thousands of signatures or fsyncs. The write path still verifies g.
	err = s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bByID))
		for i := 0; i < grants.MaxGrants-1; i++ {
			if err := b.Put([]byte(fmt.Sprintf("filler-%05d", i)), []byte("x")); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := s.Put(testGrant()); ok || err == nil {
		t.Fatal("accepted entry beyond quota")
	}
}
