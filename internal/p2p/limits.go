package p2p

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	proto "p2p-park/internal/wire"
	"time"
)

const (
	MaxEnvelopeSize       = 128 * 1024
	DefaultMaxConnections = 32 // pending handshakes and live peers share this budget
	OutboundQueueSize     = 16
	maxDiscoveryBatch     = 16
)

var ErrResourceLimit = errors.New("p2p: resource limit reached")

// Encoder emits one JSON object per line. Enforce a bound while reading, before
// JSON decoding, even when a document spans many Noise frames. Read-ahead survives
// Hello setup because this reader is retained for the entire session.
type envelopeDecoder struct{ r *bufio.Reader }

func newEnvelopeDecoder(r io.Reader) *envelopeDecoder {
	return &envelopeDecoder{bufio.NewReaderSize(r, 4096)}
}
func (d *envelopeDecoder) Decode(v any) error {
	var line []byte
	for {
		part, err := d.r.ReadSlice('\n')
		if len(line)+len(part) > MaxEnvelopeSize {
			return ErrResourceLimit
		}
		line = append(line, part...)
		if err == nil {
			return json.Unmarshal(bytes.TrimSpace(line), v)
		}
		if err != bufio.ErrBufferFull {
			if err == io.EOF && len(line) > 0 {
				return io.ErrUnexpectedEOF
			}
			return err
		}
	}
}

func (n *Node) reserveConnection() bool {
	n.lifeMu.Lock()
	defer n.lifeMu.Unlock()
	if n.ctx.Err() != nil {
		return false
	}
	select {
	case n.slots <- struct{}{}:
		n.sessions.Add(1)
		return true
	default:
		return false
	}
}
func (n *Node) releaseConnection() { <-n.slots; n.sessions.Done() }

func (n *Node) decodePeer(p *peer, env any) error {
	timer := time.AfterFunc(n.cfg.MessageTimeout, func() { _ = p.conn.Close() })
	defer timer.Stop()
	return p.reader.Decode(env)
}

func validEnvelopeSize(env proto.Envelope) bool {
	b, err := json.Marshal(env)
	return err == nil && len(b)+1 <= MaxEnvelopeSize
}

func (n *Node) launchWorker(fn func()) {
	n.lifeMu.Lock()
	defer n.lifeMu.Unlock()
	if n.ctx.Err() != nil {
		return
	}
	n.workers.Add(1)
	go func() { defer n.workers.Done(); fn() }()
}
