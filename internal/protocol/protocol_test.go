package protocol

import (
	"bytes"
	"testing"
	"time"
)

// --- Frame round-trip tests ---

func TestFrameRoundTrip(t *testing.T) {
	tests := []struct {
		name    string
		payload []byte
	}{
		{"empty payload", []byte{}},
		{"small payload", []byte("hello")},
		{"binary payload", []byte{0x00, 0xFF, 0xAB, 0xCD}},
		{"large payload", bytes.Repeat([]byte("x"), 10000)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			frame, err := EncodeFrame(tt.payload)
			if err != nil {
				t.Fatalf("EncodeFrame: %v", err)
			}

			got, err := DecodeFrame(bytes.NewReader(frame))
			if err != nil {
				t.Fatalf("DecodeFrame: %v", err)
			}

			if !bytes.Equal(got, tt.payload) {
				t.Errorf("round-trip failed: got %d bytes, want %d", len(got), len(tt.payload))
			}
		})
	}
}

func TestFrameInvalidMagic(t *testing.T) {
	frame := []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0xFF}
	_, err := DecodeFrame(bytes.NewReader(frame))
	if err == nil {
		t.Error("expected error for invalid magic bytes")
	}
}

func TestFrameTruncated(t *testing.T) {
	// Only 3 bytes — header requires 6.
	_, err := DecodeFrame(bytes.NewReader([]byte{0xCA, 0xCE, 0x00}))
	if err == nil {
		t.Error("expected error for truncated header")
	}
}

// --- Command round-trip tests ---

func TestCommandRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		cmd  *Command
	}{
		{"PING", &Command{Type: CmdPing}},
		{"GET", &Command{Type: CmdGet, Key: "mykey"}},
		{"DEL", &Command{Type: CmdDel, Key: "mykey"}},
		{"SET without TTL", &Command{Type: CmdSet, Key: "k", Value: []byte("v")}},
		{"SET with TTL", &Command{Type: CmdSet, Key: "k", Value: []byte("v"), TTL: 5 * time.Second}},
		{"SET empty value", &Command{Type: CmdSet, Key: "k", Value: []byte{}}},
		{"EXPIRE", &Command{Type: CmdExpire, Key: "k", TTL: 10 * time.Second}},
		{"KEYS", &Command{Type: CmdKeys, Pattern: "user:*"}},
		{"RANGE", &Command{Type: CmdRange, StartKey: "a", EndKey: "z"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := SerializeCommand(tt.cmd)
			got, err := ParseCommand(data)
			if err != nil {
				t.Fatalf("ParseCommand: %v", err)
			}

			if got.Type != tt.cmd.Type {
				t.Errorf("Type: got 0x%02x, want 0x%02x", got.Type, tt.cmd.Type)
			}
			if got.Key != tt.cmd.Key {
				t.Errorf("Key: got %q, want %q", got.Key, tt.cmd.Key)
			}
			if !bytes.Equal(got.Value, tt.cmd.Value) {
				t.Errorf("Value: got %v, want %v", got.Value, tt.cmd.Value)
			}
			if got.TTL != tt.cmd.TTL {
				t.Errorf("TTL: got %v, want %v", got.TTL, tt.cmd.TTL)
			}
			if got.Pattern != tt.cmd.Pattern {
				t.Errorf("Pattern: got %q, want %q", got.Pattern, tt.cmd.Pattern)
			}
			if got.StartKey != tt.cmd.StartKey {
				t.Errorf("StartKey: got %q, want %q", got.StartKey, tt.cmd.StartKey)
			}
			if got.EndKey != tt.cmd.EndKey {
				t.Errorf("EndKey: got %q, want %q", got.EndKey, tt.cmd.EndKey)
			}
		})
	}
}

func TestCommandParseEmpty(t *testing.T) {
	_, err := ParseCommand([]byte{})
	if err == nil {
		t.Error("expected error for empty payload")
	}
}

func TestCommandParseTruncatedGET(t *testing.T) {
	// GET with key length but no key data.
	_, err := ParseCommand([]byte{CmdGet, 0x00, 0x05})
	if err == nil {
		t.Error("expected error for truncated GET payload")
	}
}

// --- Response round-trip tests ---

func TestResponseRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		resp *Response
	}{
		{"OK", NewOKResponse()},
		{"Value", NewValueResponse([]byte("hello world"))},
		{"Nil", NewNilResponse()},
		{"Error", NewErrorResponse("key not found")},
		{"Empty value", NewValueResponse([]byte{})},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := SerializeResponse(tt.resp)
			got, err := ParseResponse(data)
			if err != nil {
				t.Fatalf("ParseResponse: %v", err)
			}

			if got.Status != tt.resp.Status {
				t.Errorf("Status: got 0x%02x, want 0x%02x", got.Status, tt.resp.Status)
			}
			if !bytes.Equal(got.Data, tt.resp.Data) {
				t.Errorf("Data: got %v, want %v", got.Data, tt.resp.Data)
			}
		})
	}
}

func TestArrayResponseRoundTrip(t *testing.T) {
	items := [][]byte{[]byte("key1"), []byte("key2"), []byte("key3")}
	resp := NewArrayResponse(items)
	data := SerializeResponse(resp)
	got, err := ParseResponse(data)
	if err != nil {
		t.Fatalf("ParseResponse: %v", err)
	}

	parsedItems, err := ParseArrayData(got.Data)
	if err != nil {
		t.Fatalf("ParseArrayData: %v", err)
	}

	if len(parsedItems) != len(items) {
		t.Fatalf("array length: got %d, want %d", len(parsedItems), len(items))
	}
	for i := range items {
		if !bytes.Equal(parsedItems[i], items[i]) {
			t.Errorf("item[%d]: got %q, want %q", i, parsedItems[i], items[i])
		}
	}
}

// --- Full round-trip: command → frame → decode → parse ---

func TestFullProtocolRoundTrip(t *testing.T) {
	cmd := &Command{Type: CmdSet, Key: "user:1", Value: []byte(`{"name":"alice"}`), TTL: 30 * time.Second}

	// Serialize command → encode frame.
	payload := SerializeCommand(cmd)
	frame, err := EncodeFrame(payload)
	if err != nil {
		t.Fatalf("EncodeFrame: %v", err)
	}

	// Decode frame → parse command.
	decoded, err := DecodeFrame(bytes.NewReader(frame))
	if err != nil {
		t.Fatalf("DecodeFrame: %v", err)
	}
	got, err := ParseCommand(decoded)
	if err != nil {
		t.Fatalf("ParseCommand: %v", err)
	}

	if got.Key != cmd.Key {
		t.Errorf("Key: got %q, want %q", got.Key, cmd.Key)
	}
	if !bytes.Equal(got.Value, cmd.Value) {
		t.Errorf("Value: got %q, want %q", got.Value, cmd.Value)
	}
	if got.TTL != cmd.TTL {
		t.Errorf("TTL: got %v, want %v", got.TTL, cmd.TTL)
	}
}
