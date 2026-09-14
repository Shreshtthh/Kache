package server

import (
	"bytes"
	"context"
	"net"
	"testing"
	"time"

	"github.com/kache-store/kache/internal/command"
	"github.com/kache-store/kache/internal/engine"
	"github.com/kache-store/kache/internal/eviction"
	"github.com/kache-store/kache/internal/protocol"
	"github.com/kache-store/kache/internal/ttl"
)

// startTestServer starts a server on a random port and returns its address.
func startTestServer(t *testing.T, ctx context.Context) string {
	t.Helper()
	sl := engine.NewSkipList(16, 0.5)
	lru := eviction.NewLRU(10000)
	tm := ttl.NewManager()
	eng := engine.NewKVEngine(sl, lru, tm, 10000)
	exec := command.NewExecutor(eng)

	srv := NewServer("127.0.0.1:0", exec)
	go srv.ListenAndServe(ctx)
	<-srv.Ready()

	return srv.Addr()
}

// sendCommand connects to the server, sends a command, and returns the response.
func sendCommand(t *testing.T, addr string, cmd *protocol.Command) *protocol.Response {
	t.Helper()

	conn, err := net.DialTimeout("tcp", addr, time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// Send command.
	payload := protocol.SerializeCommand(cmd)
	frame, err := protocol.EncodeFrame(payload)
	if err != nil {
		t.Fatalf("encode frame: %v", err)
	}
	if _, err := conn.Write(frame); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Read response.
	respPayload, err := protocol.DecodeFrame(conn)
	if err != nil {
		t.Fatalf("decode response frame: %v", err)
	}
	resp, err := protocol.ParseResponse(respPayload)
	if err != nil {
		t.Fatalf("parse response: %v", err)
	}
	return resp
}

func TestServerPing(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	addr := startTestServer(t, ctx)

	resp := sendCommand(t, addr, &protocol.Command{Type: protocol.CmdPing})
	if resp.Status != protocol.StatusValue {
		t.Fatalf("PING: got status 0x%02x, want VALUE", resp.Status)
	}
	if string(resp.Data) != "PONG" {
		t.Errorf("PING: got %q, want PONG", resp.Data)
	}
}

func TestServerSetGet(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	addr := startTestServer(t, ctx)

	// Use a persistent connection for SET then GET.
	conn, err := net.DialTimeout("tcp", addr, time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// SET
	setPayload := protocol.SerializeCommand(&protocol.Command{
		Type: protocol.CmdSet, Key: "greeting", Value: []byte("hello"),
	})
	frame, _ := protocol.EncodeFrame(setPayload)
	conn.Write(frame)

	respPayload, _ := protocol.DecodeFrame(conn)
	resp, _ := protocol.ParseResponse(respPayload)
	if resp.Status != protocol.StatusOK {
		t.Fatalf("SET: got status 0x%02x, want OK", resp.Status)
	}

	// GET
	getPayload := protocol.SerializeCommand(&protocol.Command{
		Type: protocol.CmdGet, Key: "greeting",
	})
	frame, _ = protocol.EncodeFrame(getPayload)
	conn.Write(frame)

	respPayload, _ = protocol.DecodeFrame(conn)
	resp, _ = protocol.ParseResponse(respPayload)
	if resp.Status != protocol.StatusValue {
		t.Fatalf("GET: got status 0x%02x, want VALUE", resp.Status)
	}
	if !bytes.Equal(resp.Data, []byte("hello")) {
		t.Errorf("GET: got %q, want %q", resp.Data, "hello")
	}
}

func TestServerConcurrentConnections(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	addr := startTestServer(t, ctx)

	const numClients = 20
	errCh := make(chan error, numClients)

	for i := 0; i < numClients; i++ {
		go func(id int) {
			conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
			if err != nil {
				errCh <- err
				return
			}
			defer conn.Close()

			// Each client does a SET + GET.
			key := []byte("key")
			val := []byte("val")

			setCmd := protocol.SerializeCommand(&protocol.Command{
				Type: protocol.CmdSet, Key: string(key), Value: val,
			})
			frame, _ := protocol.EncodeFrame(setCmd)
			conn.Write(frame)
			protocol.DecodeFrame(conn) // read SET response

			getCmd := protocol.SerializeCommand(&protocol.Command{
				Type: protocol.CmdGet, Key: string(key),
			})
			frame, _ = protocol.EncodeFrame(getCmd)
			conn.Write(frame)
			protocol.DecodeFrame(conn) // read GET response

			errCh <- nil
		}(i)
	}

	for i := 0; i < numClients; i++ {
		if err := <-errCh; err != nil {
			t.Errorf("client error: %v", err)
		}
	}
}

func TestServerGracefulShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	addr := startTestServer(t, ctx)

	// Connect.
	conn, err := net.DialTimeout("tcp", addr, time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// Cancel context — server should shut down.
	cancel()
	time.Sleep(100 * time.Millisecond)

	// Try to connect again — should fail (server stopped).
	_, err = net.DialTimeout("tcp", addr, 200*time.Millisecond)
	if err == nil {
		t.Error("expected connection to fail after shutdown")
	}
}
