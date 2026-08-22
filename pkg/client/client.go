package client

import (
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/kache-store/kache/internal/protocol"
)

var ErrKeyNotFound = errors.New("key not found")

// Client is a Go client for the Kache server.
type Client struct {
	conn net.Conn
}

// Connect establishes a TCP connection to the Kache server.
func Connect(addr string) (*Client, error) {
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("connecting to %s: %w", addr, err)
	}
	return &Client{conn: conn}, nil
}

// Close closes the connection.
func (c *Client) Close() error {
	return c.conn.Close()
}

// send serializes a command, sends it, reads the response.
func (c *Client) send(cmd *protocol.Command) (*protocol.Response, error) {
	payload := protocol.SerializeCommand(cmd)
	frame, err := protocol.EncodeFrame(payload)
	if err != nil {
		return nil, err
	}

	if _, err := c.conn.Write(frame); err != nil {
		return nil, fmt.Errorf("writing to server: %w", err)
	}

	respPayload, err := protocol.DecodeFrame(c.conn)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	resp, err := protocol.ParseResponse(respPayload)
	if err != nil {
		return nil, fmt.Errorf("parsing response: %w", err)
	}

	if resp.Status == protocol.StatusError {
		return nil, fmt.Errorf("server error: %s", resp.Data)
	}

	return resp, nil
}

// Ping sends a PING and expects PONG.
func (c *Client) Ping() error {
	resp, err := c.send(&protocol.Command{Type: protocol.CmdPing})
	if err != nil {
		return err
	}
	if string(resp.Data) != "PONG" {
		return fmt.Errorf("unexpected ping response: %q", resp.Data)
	}
	return nil
}

// Set stores a key-value pair.
func (c *Client) Set(key string, value []byte) error {
	resp, err := c.send(&protocol.Command{Type: protocol.CmdSet, Key: key, Value: value})
	if err != nil {
		return err
	}
	if resp.Status != protocol.StatusOK {
		return fmt.Errorf("unexpected status: 0x%02x", resp.Status)
	}
	return nil
}

// SetWithTTL stores a key-value pair with an expiration.
func (c *Client) SetWithTTL(key string, value []byte, ttl time.Duration) error {
	resp, err := c.send(&protocol.Command{Type: protocol.CmdSet, Key: key, Value: value, TTL: ttl})
	if err != nil {
		return err
	}
	if resp.Status != protocol.StatusOK {
		return fmt.Errorf("unexpected status: 0x%02x", resp.Status)
	}
	return nil
}

// Get retrieves a value by key. Returns ErrKeyNotFound if the key doesn't exist.
func (c *Client) Get(key string) ([]byte, error) {
	resp, err := c.send(&protocol.Command{Type: protocol.CmdGet, Key: key})
	if err != nil {
		return nil, err
	}
	if resp.Status == protocol.StatusNil {
		return nil, ErrKeyNotFound
	}
	return resp.Data, nil
}

// Del deletes a key.
func (c *Client) Del(key string) error {
	resp, err := c.send(&protocol.Command{Type: protocol.CmdDel, Key: key})
	if err != nil {
		return err
	}
	if resp.Status != protocol.StatusOK {
		return fmt.Errorf("unexpected status: 0x%02x", resp.Status)
	}
	return nil
}

// Expire sets a TTL on an existing key.
func (c *Client) Expire(key string, ttl time.Duration) error {
	resp, err := c.send(&protocol.Command{Type: protocol.CmdExpire, Key: key, TTL: ttl})
	if err != nil {
		return err
	}
	if resp.Status != protocol.StatusOK {
		return fmt.Errorf("unexpected status: 0x%02x", resp.Status)
	}
	return nil
}

// Keys returns all keys matching a glob pattern.
func (c *Client) Keys(pattern string) ([]string, error) {
	resp, err := c.send(&protocol.Command{Type: protocol.CmdKeys, Pattern: pattern})
	if err != nil {
		return nil, err
	}
	items, err := protocol.ParseArrayData(resp.Data)
	if err != nil {
		return nil, err
	}
	keys := make([]string, len(items))
	for i, item := range items {
		keys[i] = string(item)
	}
	return keys, nil
}

// Range returns key-value pairs in a lexicographic range.
func (c *Client) Range(start, end string) (map[string][]byte, error) {
	resp, err := c.send(&protocol.Command{Type: protocol.CmdRange, StartKey: start, EndKey: end})
	if err != nil {
		return nil, err
	}
	items, err := protocol.ParseArrayData(resp.Data)
	if err != nil {
		return nil, err
	}
	result := make(map[string][]byte)
	for i := 0; i+1 < len(items); i += 2 {
		result[string(items[i])] = items[i+1]
	}
	return result, nil
}
