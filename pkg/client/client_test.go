package client

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/kache-store/kache/internal/command"
	"github.com/kache-store/kache/internal/engine"
	"github.com/kache-store/kache/internal/eviction"
	"github.com/kache-store/kache/internal/server"
	"github.com/kache-store/kache/internal/ttl"
)

// startServer starts a test server and returns its address.
func startServer(t *testing.T, ctx context.Context) string {
	t.Helper()
	sl := engine.NewSkipList(16, 0.5)
	lru := eviction.NewLRU(10000)
	tm := ttl.NewManager()
	eng := engine.NewKVEngine(sl, lru, tm, 10000)
	exec := command.NewExecutor(eng)

	srv := server.NewServer("127.0.0.1:0", exec)
	go srv.ListenAndServe(ctx)
	<-srv.Ready()

	return srv.Addr()
}


func TestClientPing(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	addr := startServer(t, ctx)

	c, err := Connect(addr)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer c.Close()

	if err := c.Ping(); err != nil {
		t.Errorf("Ping: %v", err)
	}
}

func TestClientCRUD(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	addr := startServer(t, ctx)

	c, err := Connect(addr)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer c.Close()

	// SET
	if err := c.Set("name", []byte("alice")); err != nil {
		t.Fatalf("Set: %v", err)
	}

	// GET
	val, err := c.Get("name")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(val) != "alice" {
		t.Errorf("Get: got %q, want 'alice'", val)
	}

	// DEL
	if err := c.Del("name"); err != nil {
		t.Fatalf("Del: %v", err)
	}

	// GET after DEL
	_, err = c.Get("name")
	if !errors.Is(err, ErrKeyNotFound) {
		t.Errorf("Get after Del: got err=%v, want ErrKeyNotFound", err)
	}
}

func TestClientTTL(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	addr := startServer(t, ctx)

	c, err := Connect(addr)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer c.Close()

	if err := c.SetWithTTL("temp", []byte("data"), 150*time.Millisecond); err != nil {
		t.Fatalf("SetWithTTL: %v", err)
	}

	// Should exist immediately.
	if _, err := c.Get("temp"); err != nil {
		t.Fatalf("Get before TTL: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

	// Should be gone.
	_, err = c.Get("temp")
	if !errors.Is(err, ErrKeyNotFound) {
		t.Errorf("Get after TTL: got err=%v, want ErrKeyNotFound", err)
	}
}

func TestClientConcurrent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	addr := startServer(t, ctx)

	const numClients = 10
	const opsPerClient = 50

	var wg sync.WaitGroup
	errCh := make(chan error, numClients)

	for i := 0; i < numClients; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			c, err := Connect(addr)
			if err != nil {
				errCh <- err
				return
			}
			defer c.Close()

			for j := 0; j < opsPerClient; j++ {
				key := "key"
				val := []byte("val")
				if err := c.Set(key, val); err != nil {
					errCh <- err
					return
				}
				if _, err := c.Get(key); err != nil {
					errCh <- err
					return
				}
			}
		}(i)
	}

	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Errorf("concurrent client error: %v", err)
	}
}

func TestClientConnectFailure(t *testing.T) {
	_, err := Connect("127.0.0.1:1") // port 1 — should fail
	if err == nil {
		t.Error("expected error connecting to non-existent server")
	}
}
