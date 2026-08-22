package protocol

import (
	"encoding/binary"
	"errors"
	"time"
)

// Command types.
const (
	CmdGet    byte = 0x01
	CmdSet    byte = 0x02
	CmdDel    byte = 0x03
	CmdExpire byte = 0x04
	CmdPing   byte = 0x05
	CmdKeys   byte = 0x06
	CmdRange  byte = 0x07
)

var ErrMalformedCommand = errors.New("malformed command payload")

// Command represents a parsed client command.
type Command struct {
	Type     byte
	Key      string
	Value    []byte
	TTL      time.Duration // 0 means no TTL
	StartKey string        // for RANGE
	EndKey   string        // for RANGE
	Pattern  string        // for KEYS
}

// SerializeCommand converts a Command to its binary payload format.
//
// Layout varies by command type:
//
//	GET/DEL:    [CmdType:1] [KeyLen:2 BE] [Key]
//	SET:        [CmdType:1] [KeyLen:2 BE] [Key] [ValueLen:4 BE] [Value] [TTL:8 BE (ns), optional]
//	EXPIRE:     [CmdType:1] [KeyLen:2 BE] [Key] [TTL:8 BE (ns)]
//	PING:       [CmdType:1]
//	KEYS:       [CmdType:1] [PatternLen:2 BE] [Pattern]
//	RANGE:      [CmdType:1] [StartLen:2 BE] [Start] [EndLen:2 BE] [End]
func SerializeCommand(cmd *Command) []byte {
	switch cmd.Type {
	case CmdPing:
		return []byte{CmdPing}

	case CmdGet, CmdDel:
		keyBytes := []byte(cmd.Key)
		buf := make([]byte, 1+2+len(keyBytes))
		buf[0] = cmd.Type
		binary.BigEndian.PutUint16(buf[1:3], uint16(len(keyBytes)))
		copy(buf[3:], keyBytes)
		return buf

	case CmdSet:
		keyBytes := []byte(cmd.Key)
		baseLen := 1 + 2 + len(keyBytes) + 4 + len(cmd.Value)
		hasTTL := cmd.TTL > 0
		if hasTTL {
			baseLen += 8
		}
		buf := make([]byte, baseLen)
		off := 0
		buf[off] = CmdSet
		off++
		binary.BigEndian.PutUint16(buf[off:off+2], uint16(len(keyBytes)))
		off += 2
		copy(buf[off:], keyBytes)
		off += len(keyBytes)
		binary.BigEndian.PutUint32(buf[off:off+4], uint32(len(cmd.Value)))
		off += 4
		copy(buf[off:], cmd.Value)
		off += len(cmd.Value)
		if hasTTL {
			binary.BigEndian.PutUint64(buf[off:off+8], uint64(cmd.TTL.Nanoseconds()))
		}
		return buf

	case CmdExpire:
		keyBytes := []byte(cmd.Key)
		buf := make([]byte, 1+2+len(keyBytes)+8)
		off := 0
		buf[off] = CmdExpire
		off++
		binary.BigEndian.PutUint16(buf[off:off+2], uint16(len(keyBytes)))
		off += 2
		copy(buf[off:], keyBytes)
		off += len(keyBytes)
		binary.BigEndian.PutUint64(buf[off:off+8], uint64(cmd.TTL.Nanoseconds()))
		return buf

	case CmdKeys:
		pat := []byte(cmd.Pattern)
		buf := make([]byte, 1+2+len(pat))
		buf[0] = CmdKeys
		binary.BigEndian.PutUint16(buf[1:3], uint16(len(pat)))
		copy(buf[3:], pat)
		return buf

	case CmdRange:
		startBytes := []byte(cmd.StartKey)
		endBytes := []byte(cmd.EndKey)
		buf := make([]byte, 1+2+len(startBytes)+2+len(endBytes))
		off := 0
		buf[off] = CmdRange
		off++
		binary.BigEndian.PutUint16(buf[off:off+2], uint16(len(startBytes)))
		off += 2
		copy(buf[off:], startBytes)
		off += len(startBytes)
		binary.BigEndian.PutUint16(buf[off:off+2], uint16(len(endBytes)))
		off += 2
		copy(buf[off:], endBytes)
		return buf

	default:
		return []byte{cmd.Type}
	}
}

// ParseCommand deserializes a binary payload into a Command.
func ParseCommand(payload []byte) (*Command, error) {
	if len(payload) == 0 {
		return nil, ErrMalformedCommand
	}

	cmdType := payload[0]

	switch cmdType {
	case CmdPing:
		return &Command{Type: CmdPing}, nil

	case CmdGet, CmdDel:
		if len(payload) < 3 {
			return nil, ErrMalformedCommand
		}
		keyLen := binary.BigEndian.Uint16(payload[1:3])
		if len(payload) < 3+int(keyLen) {
			return nil, ErrMalformedCommand
		}
		return &Command{
			Type: cmdType,
			Key:  string(payload[3 : 3+keyLen]),
		}, nil

	case CmdSet:
		if len(payload) < 3 {
			return nil, ErrMalformedCommand
		}
		off := 1
		keyLen := binary.BigEndian.Uint16(payload[off : off+2])
		off += 2
		if len(payload) < off+int(keyLen)+4 {
			return nil, ErrMalformedCommand
		}
		key := string(payload[off : off+int(keyLen)])
		off += int(keyLen)
		valLen := binary.BigEndian.Uint32(payload[off : off+4])
		off += 4
		if len(payload) < off+int(valLen) {
			return nil, ErrMalformedCommand
		}
		value := make([]byte, valLen)
		copy(value, payload[off:off+int(valLen)])
		off += int(valLen)

		var ttl time.Duration
		if len(payload) >= off+8 {
			ttlNs := binary.BigEndian.Uint64(payload[off : off+8])
			ttl = time.Duration(ttlNs)
		}

		return &Command{
			Type:  CmdSet,
			Key:   key,
			Value: value,
			TTL:   ttl,
		}, nil

	case CmdExpire:
		if len(payload) < 3 {
			return nil, ErrMalformedCommand
		}
		off := 1
		keyLen := binary.BigEndian.Uint16(payload[off : off+2])
		off += 2
		if len(payload) < off+int(keyLen)+8 {
			return nil, ErrMalformedCommand
		}
		key := string(payload[off : off+int(keyLen)])
		off += int(keyLen)
		ttlNs := binary.BigEndian.Uint64(payload[off : off+8])
		return &Command{
			Type: CmdExpire,
			Key:  key,
			TTL:  time.Duration(ttlNs),
		}, nil

	case CmdKeys:
		if len(payload) < 3 {
			return nil, ErrMalformedCommand
		}
		patLen := binary.BigEndian.Uint16(payload[1:3])
		if len(payload) < 3+int(patLen) {
			return nil, ErrMalformedCommand
		}
		return &Command{
			Type:    CmdKeys,
			Pattern: string(payload[3 : 3+patLen]),
		}, nil

	case CmdRange:
		if len(payload) < 3 {
			return nil, ErrMalformedCommand
		}
		off := 1
		startLen := binary.BigEndian.Uint16(payload[off : off+2])
		off += 2
		if len(payload) < off+int(startLen)+2 {
			return nil, ErrMalformedCommand
		}
		startKey := string(payload[off : off+int(startLen)])
		off += int(startLen)
		endLen := binary.BigEndian.Uint16(payload[off : off+2])
		off += 2
		if len(payload) < off+int(endLen) {
			return nil, ErrMalformedCommand
		}
		endKey := string(payload[off : off+int(endLen)])
		return &Command{
			Type:     CmdRange,
			StartKey: startKey,
			EndKey:   endKey,
		}, nil

	default:
		return nil, fmt.Errorf("unknown command type: 0x%02x", cmdType)
	}
}
