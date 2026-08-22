package protocol

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// Magic bytes to identify the Kache protocol.
var Magic = [2]byte{0xCA, 0xCE}

// MaxPayloadSize is the maximum allowed payload (64MB).
const MaxPayloadSize = 64 * 1024 * 1024

// Frame header: [Magic: 2 bytes] [Length: 4 bytes BE]
const frameHeaderSize = 6

var (
	ErrInvalidMagic   = errors.New("invalid magic bytes: not a kache protocol connection")
	ErrPayloadTooLarge = errors.New("payload exceeds maximum size")
)

// EncodeFrame wraps a payload in a Kache wire frame.
// Returns: [Magic (2B)] [Length (4B BE)] [Payload]
func EncodeFrame(payload []byte) ([]byte, error) {
	if len(payload) > MaxPayloadSize {
		return nil, ErrPayloadTooLarge
	}
	frame := make([]byte, frameHeaderSize+len(payload))
	frame[0] = Magic[0]
	frame[1] = Magic[1]
	binary.BigEndian.PutUint32(frame[2:6], uint32(len(payload)))
	copy(frame[6:], payload)
	return frame, nil
}

// DecodeFrame reads a complete frame from a reader and returns the payload.
// Validates magic bytes and enforces payload size limits.
func DecodeFrame(r io.Reader) ([]byte, error) {
	header := make([]byte, frameHeaderSize)
	if _, err := io.ReadFull(r, header); err != nil {
		return nil, fmt.Errorf("reading frame header: %w", err)
	}

	if header[0] != Magic[0] || header[1] != Magic[1] {
		return nil, ErrInvalidMagic
	}

	length := binary.BigEndian.Uint32(header[2:6])
	if length > MaxPayloadSize {
		return nil, ErrPayloadTooLarge
	}

	payload := make([]byte, length)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, fmt.Errorf("reading frame payload: %w", err)
	}

	return payload, nil
}
