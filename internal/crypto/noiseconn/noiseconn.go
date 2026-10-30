package noiseconn

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
	"sync"

	"github.com/flynn/noise"
)

// MaxFrameSize bounds ciphertext allocation before any peer-controlled body is read.
// Noise messages are limited to 65535 bytes, including the 16-byte AEAD tag.
const MaxFrameSize = 65535
const maxPlaintextSize = MaxFrameSize - 16

// SecureConn is a byte stream. Readers and writers are serialized independently;
// one reader and one writer can progress concurrently. Close unblocks network I/O.
// Callers own deadlines/cancellation on the underlying stream.
type SecureConn struct {
	underlying        io.ReadWriteCloser
	readCS, writeCS   *noise.CipherState
	readMu, writeMu   sync.Mutex
	pending           []byte
	readErr, writeErr error
}

func (c *SecureConn) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	c.readMu.Lock()
	defer c.readMu.Unlock()
	for len(c.pending) == 0 {
		if c.readErr != nil {
			return 0, c.readErr
		}
		var prefix [4]byte
		_, err := io.ReadFull(c.underlying, prefix[:])
		if err != nil {
			return c.failRead(err)
		}
		n := binary.BigEndian.Uint32(prefix[:])
		if n < 16 || n > MaxFrameSize {
			return c.failRead(fmt.Errorf("invalid frame length: %d", n))
		}
		ct := make([]byte, int(n))
		if _, err := io.ReadFull(c.underlying, ct); err != nil {
			return c.failRead(err)
		}
		pt, err := c.readCS.Decrypt(nil, nil, ct)
		if err != nil {
			return c.failRead(err)
		}
		c.pending = pt
	}
	n := copy(p, c.pending)
	c.pending = c.pending[n:]
	return n, nil
}

func (c *SecureConn) failRead(err error) (int, error) {
	c.readErr = err
	_ = c.underlying.Close()
	return 0, err
}

// Write splits a byte stream into bounded frames. A failed frame makes the
// stream unusable: retrying after advancing a Noise nonce would desynchronize it.
func (c *SecureConn) Write(p []byte) (int, error) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if c.writeErr != nil {
		return 0, c.writeErr
	}
	written := 0
	for len(p) > 0 {
		size := len(p)
		if size > maxPlaintextSize {
			size = maxPlaintextSize
		}
		ct, err := c.writeCS.Encrypt(nil, nil, p[:size])
		if err == nil {
			var prefix [4]byte
			binary.BigEndian.PutUint32(prefix[:], uint32(len(ct)))
			err = writeFull(c.underlying, prefix[:])
			if err == nil {
				err = writeFull(c.underlying, ct)
			}
		}
		if err != nil {
			c.writeErr = err
			_ = c.underlying.Close()
			return written, err
		}
		written += size
		p = p[size:]
	}
	return written, nil
}

func (c *SecureConn) Close() error { return c.underlying.Close() }

// HandshakeResult contains the secure connection plus the remote's identity payload.
type HandshakeResult struct {
	Conn          *SecureConn
	RemotePayload []byte
	RemoteStatic  []byte // authenticated remote Noise static public key
}

// NewSecureClient runs a Noise_XX handshake as initiator and attaches localPayload
// as the final handshake payload (identity, etc.).
func NewSecureClient(
	underlying io.ReadWriteCloser,
	staticPriv, staticPub, localPayload []byte,
) (*HandshakeResult, error) {
	cs := noise.NewCipherSuite(noise.DH25519, noise.CipherChaChaPoly, noise.HashBLAKE2s)

	cfg := noise.Config{
		CipherSuite:   cs,
		Random:        rand.Reader,
		Pattern:       noise.HandshakeXX,
		Initiator:     true,
		StaticKeypair: noise.DHKey{Private: staticPriv, Public: staticPub},
	}

	hs, err := noise.NewHandshakeState(cfg)
	if err != nil {
		return nil, err
	}

	// XX pattern: 3 messages total.

	// -> e
	msg1, _, _, err := hs.WriteMessage(nil, nil)
	if err != nil {
		return nil, err
	}
	if err := writeHandshakeMsg(underlying, msg1); err != nil {
		return nil, err
	}

	// <- e, ee, s, es
	msg2, err := readHandshakeMsg(underlying)
	if err != nil {
		return nil, err
	}
	remotePayload, _, _, err := hs.ReadMessage(nil, msg2)
	if err != nil {
		return nil, err
	}

	// -> s, se, payload (our identity payload)
	msg3, cs1, cs2, err := hs.WriteMessage(nil, localPayload)
	if err != nil {
		return nil, err
	}
	if err := writeHandshakeMsg(underlying, msg3); err != nil {
		return nil, err
	}

	// Per noise docs, for the party that *writes* the final message:
	//   first CipherState = sending, second = receiving.
	return &HandshakeResult{
		Conn: &SecureConn{
			underlying: underlying,
			readCS:     cs2, // receiving
			writeCS:    cs1, // sending
		},
		RemotePayload: remotePayload,
		RemoteStatic:  append([]byte(nil), hs.PeerStatic()...),
	}, nil
}

// NewSecureServer runs a Noise_XX handshake as responder and returns a SecureConn,
// plus the remote's identity payload (from initiator).
func NewSecureServer(
	underlying io.ReadWriteCloser,
	staticPriv, staticPub, localPayload []byte,
) (*HandshakeResult, error) {
	cs := noise.NewCipherSuite(noise.DH25519, noise.CipherChaChaPoly, noise.HashBLAKE2s)

	cfg := noise.Config{
		CipherSuite:   cs,
		Random:        rand.Reader,
		Pattern:       noise.HandshakeXX,
		Initiator:     false,
		StaticKeypair: noise.DHKey{Private: staticPriv, Public: staticPub},
	}

	hs, err := noise.NewHandshakeState(cfg)
	if err != nil {
		return nil, err
	}

	// <- e
	msg1, err := readHandshakeMsg(underlying)
	if err != nil {
		return nil, err
	}
	if _, _, _, err := hs.ReadMessage(nil, msg1); err != nil {
		return nil, err
	}

	// -> e, ee, s, es
	msg2, _, _, err := hs.WriteMessage(nil, localPayload)
	if err != nil {
		return nil, err
	}
	if err := writeHandshakeMsg(underlying, msg2); err != nil {
		return nil, err
	}

	// <- s, se, payload (initiator's identity payload)
	msg3, err := readHandshakeMsg(underlying)
	if err != nil {
		return nil, err
	}
	remotePayload, cs1, cs2, err := hs.ReadMessage(nil, msg3)
	if err != nil {
		return nil, err
	}

	// For the party that *reads* the final message:
	//   first CipherState = receiving, second = sending.
	return &HandshakeResult{
		Conn: &SecureConn{
			underlying: underlying,
			readCS:     cs1, // receiving
			writeCS:    cs2, // sending
		},
		RemotePayload: remotePayload,
		RemoteStatic:  append([]byte(nil), hs.PeerStatic()...),
	}, nil
}
