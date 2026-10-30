package noiseconn

import (
	"encoding/binary"
	"fmt"
	"io"
)

// writeHandshakeMsg sends a length-prefixed handshake message.
func writeHandshakeMsg(w io.Writer, msg []byte) error {
	var lenBuf [2]byte
	if len(msg) == 0 || len(msg) > 0xffff {
		return fmt.Errorf("handshake message too long")
	}
	binary.BigEndian.PutUint16(lenBuf[:], uint16(len(msg)))
	if err := writeFull(w, lenBuf[:]); err != nil {
		return err
	}
	return writeFull(w, msg)
}

// readHandshakeMsg reads a single length-prefixed handshake message.
func readHandshakeMsg(r io.Reader) ([]byte, error) {
	var lenBuf [2]byte
	if _, err := io.ReadFull(r, lenBuf[:]); err != nil {
		return nil, err
	}
	n := binary.BigEndian.Uint16(lenBuf[:])
	if n == 0 {
		return nil, fmt.Errorf("invalid handshake message length")
	}
	msg := make([]byte, n)
	if _, err := io.ReadFull(r, msg); err != nil {
		return nil, err
	}
	return msg, nil
}

// writeFull handles short progress and rejects writers that cannot progress.
func writeFull(w io.Writer, p []byte) error {
	for len(p) > 0 {
		n, err := w.Write(p)
		if n < 0 || n > len(p) {
			return io.ErrShortWrite
		}
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		p = p[n:]
	}
	return nil
}
