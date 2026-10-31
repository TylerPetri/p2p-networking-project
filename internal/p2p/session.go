package p2p

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"p2p-park/internal/crypto/noiseconn"
	"p2p-park/internal/netx"
	proto "p2p-park/internal/wire"
	"strconv"
	"time"
)

type deadlineConn interface {
	SetReadDeadline(t time.Time) error
}

func (n *Node) establishPeer(rawConn netx.Conn, inbound bool, expected ...string) (*peer, io.Closer, error) {
	id := n.Identity()

	ip := proto.NoiseIdentityPayload{
		Name:      n.cfg.Name,
		UserPub:   id.SignPub,
		Signature: ed25519.Sign(id.SignPriv, identityBinding(id.NoisePub[:])),
	}
	payloadBytes, err := json.Marshal(ip)
	if err != nil {
		return nil, nil, err
	}

	var hs *noiseconn.HandshakeResult
	if inbound {
		hs, err = noiseconn.NewSecureServer(rawConn, id.NoisePriv[:], id.NoisePub[:], payloadBytes)
	} else {
		hs, err = noiseconn.NewSecureClient(rawConn, id.NoisePriv[:], id.NoisePub[:], payloadBytes)
	}
	if err != nil {
		return nil, nil, err
	}

	secure := hs.Conn

	// decode remote payload
	var remoteName string
	var remoteUserPub ed25519.PublicKey
	var remoteUserID string

	if len(hs.RemotePayload) > 0 {
		var rip proto.NoiseIdentityPayload
		if err := json.Unmarshal(hs.RemotePayload, &rip); err != nil {
			_ = secure.Close()
			return nil, nil, err
		}
		if !validIdentityPayload(rip, hs.RemoteStatic) {
			_ = secure.Close()
			return nil, nil, errors.New("invalid signing identity binding")
		}
		if len(rip.Name) > 256 {
			_ = secure.Close()
			return nil, nil, errors.New("peer name too long")
		}
		remoteName = rip.Name
		remoteUserPub = ed25519.PublicKey(rip.UserPub)
		remoteUserID = hex.EncodeToString(remoteUserPub)
	}

	if len(expected) > 0 && expected[0] != "" && hex.EncodeToString(hs.RemoteStatic) != expected[0] {
		_ = secure.Close()
		return nil, nil, errors.New("peer pin mismatch")
	}
	dec := newEnvelopeDecoder(secure)
	enc := json.NewEncoder(secure)

	// hello handshake
	if err := n.sendHello(enc); err != nil {
		_ = secure.Close()
		return nil, nil, err
	}

	env, err := n.readEnvelopeWithTimeout(rawConn, dec, 5*time.Second)
	if err != nil {
		_ = secure.Close()
		return nil, nil, err
	}
	if env.Type != proto.MsgHello {
		_ = secure.Close()
		return nil, nil, errors.New("expected hello")
	}

	var hello proto.Hello
	if err := json.Unmarshal(env.Payload, &hello); err != nil {
		_ = secure.Close()
		return nil, nil, err
	}

	if env.FromID != hex.EncodeToString(hs.RemoteStatic) {
		_ = secure.Close()
		return nil, nil, errors.New("hello peer ID does not match Noise static key")
	}
	if len(remoteUserPub) != ed25519.PublicKeySize {
		_ = secure.Close()
		return nil, nil, errors.New("missing signing identity")
	}
	if hello.Protocol != n.cfg.Protocol {
		_ = secure.Close()
		return nil, nil, errors.New("incompatible protocol")
	}
	if len(hello.Listen) > 128 {
		_ = secure.Close()
		return nil, nil, errors.New("listen address too long")
	}
	// The peer may advertise a port, not an unrelated destination host.
	_, port, portErr := net.SplitHostPort(hello.Listen)
	remoteHost, _, hostErr := net.SplitHostPort(string(rawConn.RemoteAddr()))
	portNumber, numErr := strconv.Atoi(port)
	if portErr != nil || hostErr != nil || numErr != nil || portNumber < 1 || portNumber > 65535 {
		_ = secure.Close()
		return nil, nil, errors.New("invalid advertised listen endpoint")
	}
	hello.Listen = net.JoinHostPort(remoteHost, port)
	if hello.NetworkID != n.cfg.NetworkID {
		return nil, secure, fmt.Errorf("network mismatch")
	}
	if !n.admitted(env.FromID) {
		return nil, secure, ErrAdmissionDenied
	}
	if n.cfg.NetworkID != "" {
		readReady := func() error {
			ack, err := n.readEnvelopeWithTimeout(rawConn, dec, 5*time.Second)
			if err != nil {
				return err
			}
			if ack.Type != proto.MsgReady || ack.FromID != env.FromID {
				return fmt.Errorf("invalid admission acknowledgement")
			}
			return nil
		}
		writeReady := func() error { return enc.Encode(proto.Envelope{Type: proto.MsgReady, FromID: n.ID()}) }
		if inbound {
			if err := readReady(); err != nil {
				return nil, secure, err
			}
			if err := writeReady(); err != nil {
				return nil, secure, err
			}
		} else {
			if err := writeReady(); err != nil {
				return nil, secure, err
			}
			if err := readReady(); err != nil {
				return nil, secure, err
			}
		}
	}
	peerID := env.FromID
	pctx, cancel := context.WithCancel(n.ctx)
	p := &peer{
		id:           peerID,
		name:         remoteName,
		addr:         netx.Addr(hello.Listen),
		observedAddr: rawConn.RemoteAddr(),
		conn:         secure,
		writer:       enc,
		reader:       dec,
		sendCh:       make(chan proto.Envelope, OutboundQueueSize), // TODO: make configurable
		ctx:          pctx,
		cancel:       cancel,
		userPub:      remoteUserPub,
		userID:       remoteUserID,
	}

	if !n.addPeer(p) {
		cancel()
		_ = secure.Close()
		return nil, nil, nil
	}

	p.writerDone = make(chan struct{})
	go func() { defer close(p.writerDone); p.writeLoop(n) }()
	return p, secure, nil
}

func (n *Node) runPeerReadLoop(p *peer) {
	for {
		select {
		case <-n.ctx.Done():
			return
		case <-p.ctx.Done():
			return
		default:
		}
		var env proto.Envelope
		if err := n.decodePeer(p, &env); err != nil {
			n.Logf("read from %s failed: %v", p.id, err)
			return
		}
		n.handleEnvelope(p, env)
	}
}

func (n *Node) readEnvelopeWithTimeout(rawConn netx.Conn, dec interface{ Decode(any) error }, timeout time.Duration) (proto.Envelope, error) {
	if dc, ok := rawConn.(deadlineConn); ok {
		_ = dc.SetReadDeadline(time.Now().Add(timeout))
		defer func() { _ = dc.SetReadDeadline(time.Time{}) }()

		var env proto.Envelope
		err := dec.Decode(&env)
		return env, err
	}

	type result struct {
		env proto.Envelope
		err error
	}
	ch := make(chan result, 1)

	go func() {
		var env proto.Envelope
		err := dec.Decode(&env)
		ch <- result{env: env, err: err}
	}()

	select {
	case r := <-ch:
		return r.env, r.err
	case <-time.After(timeout):
		_ = rawConn.Close() // ensures decode unblocks and goroutine exits
		return proto.Envelope{}, errors.New("read timeout")
	}
}

func (n *Node) sendHello(enc *json.Encoder) error {
	h := proto.Hello{
		NetworkID: n.cfg.NetworkID,
		Name:      n.cfg.Name,
		Listen:    string(n.addr),
		Protocol:  n.cfg.Protocol,
	}
	env := proto.Envelope{
		Type:    proto.MsgHello,
		FromID:  n.id.ID,
		Payload: proto.MustMarshal(h),
	}
	return enc.Encode(env)
}

func identityBinding(static []byte) []byte {
	return append([]byte("p2p-park/noise-signing-identity/v1\x00"), static...)
}

func validIdentityPayload(p proto.NoiseIdentityPayload, static []byte) bool {
	return len(static) == 32 && len(p.UserPub) == ed25519.PublicKeySize &&
		ed25519.Verify(ed25519.PublicKey(p.UserPub), identityBinding(static), p.Signature)
}

// Identify may update a display name, but cannot replace the handshake identity.
func sameUserIdentity(a, b []byte) bool { return bytes.Equal(a, b) }
