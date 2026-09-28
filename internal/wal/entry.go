package wal

import (
	"encoding/binary"
	"errors"
	"hash/crc32"
)

var (
	ErrInvalidCRC = errors.New("invalid CRC32 checksum")
	ErrTooShort   = errors.New("entry too short")
)

// Entry represents a single record in the Write-Ahead Log.
type Entry struct {
	CRC       uint32
	Length    uint32
	Timestamp uint64
	CmdType   byte
	Data      []byte
}

// HeaderSize is the fixed overhead per entry: CRC(4) + Length(4) + Timestamp(8) + CmdType(1).
const HeaderSize = 17

// SerializeEntry converts an Entry into its binary format.
// Format: [CRC32: 4B] [Length: 4B] [Timestamp: 8B] [CmdType: 1B] [Data: variable]
func SerializeEntry(e *Entry) []byte {
	buf := make([]byte, HeaderSize+len(e.Data))

	e.Length = uint32(HeaderSize + len(e.Data))

	binary.BigEndian.PutUint32(buf[4:8], e.Length)
	binary.BigEndian.PutUint64(buf[8:16], e.Timestamp)
	buf[16] = e.CmdType
	copy(buf[17:], e.Data)

	// CRC covers everything after the CRC field itself.
	e.CRC = crc32.ChecksumIEEE(buf[4:])
	binary.BigEndian.PutUint32(buf[0:4], e.CRC)

	return buf
}

// DeserializeEntry parses binary data into an Entry, validating the CRC.
func DeserializeEntry(data []byte) (*Entry, error) {
	if len(data) < HeaderSize {
		return nil, ErrTooShort
	}

	storedCRC := binary.BigEndian.Uint32(data[0:4])
	computedCRC := crc32.ChecksumIEEE(data[4:])
	if storedCRC != computedCRC {
		return nil, ErrInvalidCRC
	}

	length := binary.BigEndian.Uint32(data[4:8])
	if uint32(len(data)) != length {
		return nil, errors.New("data length mismatch")
	}

	timestamp := binary.BigEndian.Uint64(data[8:16])
	cmdType := data[16]

	payload := make([]byte, len(data)-HeaderSize)
	copy(payload, data[HeaderSize:])

	return &Entry{
		CRC:       storedCRC,
		Length:    length,
		Timestamp: timestamp,
		CmdType:   cmdType,
		Data:      payload,
	}, nil
}
