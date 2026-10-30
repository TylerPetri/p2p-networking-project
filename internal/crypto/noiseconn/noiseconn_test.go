package noiseconn

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/flynn/noise"
)

// Fixed keys only in unit tests; each test starts fresh cipher states.
func testCipher() *noise.CipherState {
	return noise.UnsafeNewCipherState(noise.NewCipherSuite(noise.DH25519, noise.CipherChaChaPoly, noise.HashBLAKE2s), [32]byte{1}, 0)
}

type testStream struct {
	bytes.Buffer
	chunk     int
	zero      bool
	closed    bool
	failAfter int
}

func (s *testStream) Close() error { s.closed = true; return nil }
func (s *testStream) Write(p []byte) (int, error) {
	if s.zero {
		return 0, nil
	}
	if s.failAfter > 0 && s.Len() >= s.failAfter {
		return 0, io.ErrClosedPipe
	}
	if s.chunk > 0 && len(p) > s.chunk {
		p = p[:s.chunk]
	}
	return s.Buffer.Write(p)
}
func frame(t *testing.T, p []byte) []byte {
	t.Helper()
	s := &testStream{}
	c := &SecureConn{underlying: s, writeCS: testCipher()}
	if n, err := c.Write(p); err != nil || n != len(p) {
		t.Fatalf("write: %d %v", n, err)
	}
	return append([]byte(nil), s.Bytes()...)
}

func TestRejectFrameLengthBeforeBody(t *testing.T) {
	for _, size := range []uint32{0, 1, 15, MaxFrameSize + 1, 0xffffffff} {
		t.Run(string(rune(size%26+'a')), func(t *testing.T) {
			s := &testStream{}
			var prefix [4]byte
			binary.BigEndian.PutUint32(prefix[:], size)
			s.Buffer.Write(prefix[:])
			c := &SecureConn{underlying: s, readCS: testCipher()}
			if _, err := c.Read(make([]byte, 1)); err == nil || errors.Is(err, io.EOF) {
				t.Fatalf("expected length rejection before body, got %v", err)
			}
			if !s.closed {
				t.Fatal("invalid frame must close stream")
			}
		})
	}
}

func TestStreamSmallReadsLargeWriteAndShortWrites(t *testing.T) {
	want := bytes.Repeat([]byte("abcdef"), 30000)
	s := &testStream{chunk: 3}
	w := &SecureConn{underlying: s, writeCS: testCipher()}
	if n, err := w.Write(want); n != len(want) || err != nil {
		t.Fatalf("write: %d %v", n, err)
	}
	// Check every advertised frame, including the boundary frame.
	wire := append([]byte(nil), s.Bytes()...)
	for len(wire) > 0 {
		n := int(binary.BigEndian.Uint32(wire))
		if n > MaxFrameSize || n < 16 {
			t.Fatalf("size %d", n)
		}
		wire = wire[4+n:]
	}
	r := &SecureConn{underlying: s, readCS: testCipher()}
	if n, err := r.Read(nil); n != 0 || err != nil {
		t.Fatalf("empty read: %d %v", n, err)
	}
	got := make([]byte, 0, len(want))
	buf := make([]byte, 7)
	for len(got) < len(want) {
		n, err := r.Read(buf)
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, buf[:n]...)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("plaintext lost or changed")
	}
}

func TestTruncatedFrameIsTerminal(t *testing.T) {
	wire := frame(t, []byte("payload"))
	for _, end := range []int{1, 3, 4, 5, len(wire) - 1} {
		s := &testStream{}
		s.Buffer.Write(wire[:end])
		c := &SecureConn{underlying: s, readCS: testCipher()}
		_, err := c.Read(make([]byte, 32))
		if err == nil || !s.closed {
			t.Fatalf("end %d: %v", end, err)
		}
		s.Buffer.Write(wire)
		if _, again := c.Read(make([]byte, 32)); again != err {
			t.Fatalf("non-terminal error: %v", again)
		}
	}
	// A genuinely fragmented reader must succeed.
	s := &testStream{}
	s.Buffer.Write(wire)
	c := &SecureConn{underlying: &oneByteReader{testStream: s}, readCS: testCipher()}
	got := make([]byte, 7)
	if _, err := io.ReadFull(c, got); err != nil || string(got) != "payload" {
		t.Fatalf("fragmented: %q %v", got, err)
	}
}

type oneByteReader struct{ *testStream }

func (s *oneByteReader) Read(p []byte) (int, error) {
	if len(p) > 1 {
		p = p[:1]
	}
	return s.testStream.Read(p)
}

func TestReplayAndTamperAreTerminal(t *testing.T) {
	wire := frame(t, []byte("hello"))
	for _, tamper := range []bool{false, true} {
		s := &testStream{}
		s.Buffer.Write(wire)
		second := append([]byte(nil), wire...)
		if tamper {
			second[len(second)-1] ^= 1
		}
		s.Buffer.Write(second)
		c := &SecureConn{underlying: s, readCS: testCipher()}
		if _, err := c.Read(make([]byte, 10)); err != nil {
			t.Fatal(err)
		}
		if _, err := c.Read(make([]byte, 10)); err == nil || !s.closed {
			t.Fatal("accepted replay/tamper")
		}
	}
}

func TestFailedWriteIsTerminal(t *testing.T) {
	for _, s := range []*testStream{{zero: true}, {chunk: 2, failAfter: 4}} {
		c := &SecureConn{underlying: s, writeCS: testCipher()}
		if n, err := c.Write([]byte("payload")); err == nil || n != 0 || !s.closed {
			t.Fatalf("write: %d %v closed=%v", n, err, s.closed)
		}
		before := s.Len()
		s.zero = false
		s.failAfter = 0
		if _, err := c.Write([]byte("retry")); err == nil || s.Len() != before {
			t.Fatal("reused failed cipher stream")
		}
	}
}

func TestHandshakeFraming(t *testing.T) {
	s := &testStream{chunk: 1}
	if err := writeHandshakeMsg(s, []byte("handshake")); err != nil {
		t.Fatal(err)
	}
	got, err := readHandshakeMsg(&oneByteReader{testStream: s})
	if err != nil || string(got) != "handshake" {
		t.Fatalf("%q %v", got, err)
	}
	for _, wire := range [][]byte{{}, {0}, {0, 0}, {0, 3, 1}} {
		if _, err := readHandshakeMsg(bytes.NewReader(wire)); err == nil {
			t.Fatal("accepted truncated/empty handshake")
		}
	}
	if err := writeHandshakeMsg(&testStream{zero: true}, []byte("x")); !errors.Is(err, io.ErrShortWrite) {
		t.Fatal(err)
	}
	if err := writeHandshakeMsg(s, make([]byte, 65536)); err == nil {
		t.Fatal("oversize handshake")
	}
}

func TestHandshakeAndConcurrentDuplex(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	_ = a.SetDeadline(time.Now().Add(10 * time.Second))
	_ = b.SetDeadline(time.Now().Add(10 * time.Second))
	suite := noise.NewCipherSuite(noise.DH25519, noise.CipherChaChaPoly, noise.HashBLAKE2s)
	ka, err := suite.GenerateKeypair(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	kb, err := suite.GenerateKeypair(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	type result struct {
		hs  *HandshakeResult
		err error
	}
	ch := make(chan result, 1)
	go func() { hs, err := NewSecureServer(b, kb.Private, kb.Public, []byte("server")); ch <- result{hs, err} }()
	client, err := NewSecureClient(a, ka.Private, ka.Public, []byte("client"))
	if err != nil {
		t.Fatal(err)
	}
	server := <-ch
	if server.err != nil {
		t.Fatal(server.err)
	}
	if !bytes.Equal(client.RemoteStatic, kb.Public) || !bytes.Equal(server.hs.RemoteStatic, ka.Public) || string(client.RemotePayload) != "server" || string(server.hs.RemotePayload) != "client" {
		t.Fatal("handshake identity/payload mismatch")
	}
	// Multiple callers share each cipher state; both directions progress together.
	var wg sync.WaitGroup
	for _, c := range []*SecureConn{client.Conn, server.hs.Conn} {
		for i := 0; i < 4; i++ {
			wg.Add(2)
			go func(c *SecureConn) {
				defer wg.Done()
				for j := 0; j < 50; j++ {
					if _, err := c.Write([]byte{42}); err != nil {
						t.Error(err)
						return
					}
				}
			}(c)
			go func(c *SecureConn) {
				defer wg.Done()
				var p [1]byte
				for j := 0; j < 50; j++ {
					if _, err := io.ReadFull(c, p[:]); err != nil {
						t.Error(err)
						return
					}
					if p[0] != 42 {
						t.Error("corrupt plaintext")
					}
				}
			}(c)
		}
	}
	wg.Wait()
}
