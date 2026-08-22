package protocol

import (
	"encoding/binary"
	"fmt"
)

// Response status codes.
const (
	StatusOK    byte = 0x00
	StatusValue byte = 0x01
	StatusNil   byte = 0x02
	StatusError byte = 0x03
	StatusArray byte = 0x04
)

// Response represents a server response to a client command.
type Response struct {
	Status byte
	Data   []byte
}

// NewOKResponse creates a response with status OK and no data.
func NewOKResponse() *Response {
	return &Response{Status: StatusOK}
}

// NewValueResponse creates a response carrying a value.
func NewValueResponse(data []byte) *Response {
	return &Response{Status: StatusValue, Data: data}
}

// NewNilResponse creates a response indicating the key was not found.
func NewNilResponse() *Response {
	return &Response{Status: StatusNil}
}

// NewErrorResponse creates an error response with a message.
func NewErrorResponse(msg string) *Response {
	return &Response{Status: StatusError, Data: []byte(msg)}
}

// NewArrayResponse creates a response carrying multiple values.
// The data format: [Count: 4 BE] [Len1: 4 BE] [Data1] [Len2: 4 BE] [Data2] ...
func NewArrayResponse(items [][]byte) *Response {
	// Calculate total size: 4 (count) + sum(4 + len(item)) for each item.
	size := 4
	for _, item := range items {
		size += 4 + len(item)
	}

	data := make([]byte, size)
	binary.BigEndian.PutUint32(data[0:4], uint32(len(items)))
	off := 4
	for _, item := range items {
		binary.BigEndian.PutUint32(data[off:off+4], uint32(len(item)))
		off += 4
		copy(data[off:], item)
		off += len(item)
	}

	return &Response{Status: StatusArray, Data: data}
}

// SerializeResponse converts a Response into its binary payload format.
// Layout: [Status: 1 byte] [DataLen: 4 bytes BE] [Data]
func SerializeResponse(resp *Response) []byte {
	buf := make([]byte, 1+4+len(resp.Data))
	buf[0] = resp.Status
	binary.BigEndian.PutUint32(buf[1:5], uint32(len(resp.Data)))
	copy(buf[5:], resp.Data)
	return buf
}

// ParseResponse deserializes a binary payload into a Response.
func ParseResponse(payload []byte) (*Response, error) {
	if len(payload) < 5 {
		return nil, fmt.Errorf("response payload too short: %d bytes", len(payload))
	}
	status := payload[0]
	dataLen := binary.BigEndian.Uint32(payload[1:5])
	if len(payload) < 5+int(dataLen) {
		return nil, fmt.Errorf("response data truncated: expected %d bytes, got %d", dataLen, len(payload)-5)
	}
	data := make([]byte, dataLen)
	copy(data, payload[5:5+dataLen])
	return &Response{Status: status, Data: data}, nil
}

// ParseArrayData extracts individual items from an array response's data.
func ParseArrayData(data []byte) ([][]byte, error) {
	if len(data) < 4 {
		return nil, fmt.Errorf("array data too short")
	}
	count := binary.BigEndian.Uint32(data[0:4])
	off := 4
	items := make([][]byte, 0, count)
	for i := 0; i < int(count); i++ {
		if off+4 > len(data) {
			return nil, fmt.Errorf("array item %d: truncated length", i)
		}
		itemLen := binary.BigEndian.Uint32(data[off : off+4])
		off += 4
		if off+int(itemLen) > len(data) {
			return nil, fmt.Errorf("array item %d: truncated data", i)
		}
		item := make([]byte, itemLen)
		copy(item, data[off:off+int(itemLen)])
		off += int(itemLen)
		items = append(items, item)
	}
	return items, nil
}
