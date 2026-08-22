package command

import (
	"testing"
	"time"

	"github.com/kache-store/kache/internal/engine"
	"github.com/kache-store/kache/internal/eviction"
	"github.com/kache-store/kache/internal/protocol"
	"github.com/kache-store/kache/internal/ttl"
)

// newTestEngine creates a KVEngine wired up for tests.
func newTestEngine() engine.Engine {
	sl := engine.NewSkipList(16, 0.5)
	lru := eviction.NewLRU(1000)
	tm := ttl.NewManager()
	return engine.NewKVEngine(sl, lru, tm, 1000)
}

func TestSetThenGet(t *testing.T) {
	exec := NewExecutor(newTestEngine())

	resp := exec.Execute(&protocol.Command{Type: protocol.CmdSet, Key: "k1", Value: []byte("v1")})
	if resp.Status != protocol.StatusOK {
		t.Fatalf("SET: got status 0x%02x, want OK", resp.Status)
	}

	resp = exec.Execute(&protocol.Command{Type: protocol.CmdGet, Key: "k1"})
	if resp.Status != protocol.StatusValue {
		t.Fatalf("GET: got status 0x%02x, want VALUE", resp.Status)
	}
	if string(resp.Data) != "v1" {
		t.Errorf("GET: got %q, want %q", resp.Data, "v1")
	}
}

func TestGetNonExistent(t *testing.T) {
	exec := NewExecutor(newTestEngine())
	resp := exec.Execute(&protocol.Command{Type: protocol.CmdGet, Key: "nope"})
	if resp.Status != protocol.StatusNil {
		t.Errorf("GET non-existent: got status 0x%02x, want NIL", resp.Status)
	}
}

func TestSetThenDelThenGet(t *testing.T) {
	exec := NewExecutor(newTestEngine())

	exec.Execute(&protocol.Command{Type: protocol.CmdSet, Key: "k", Value: []byte("v")})
	resp := exec.Execute(&protocol.Command{Type: protocol.CmdDel, Key: "k"})
	if resp.Status != protocol.StatusOK {
		t.Fatalf("DEL: got status 0x%02x, want OK", resp.Status)
	}

	resp = exec.Execute(&protocol.Command{Type: protocol.CmdGet, Key: "k"})
	if resp.Status != protocol.StatusNil {
		t.Errorf("GET after DEL: got status 0x%02x, want NIL", resp.Status)
	}
}

func TestSetWithTTL(t *testing.T) {
	exec := NewExecutor(newTestEngine())

	exec.Execute(&protocol.Command{Type: protocol.CmdSet, Key: "k", Value: []byte("v"), TTL: 100 * time.Millisecond})

	resp := exec.Execute(&protocol.Command{Type: protocol.CmdGet, Key: "k"})
	if resp.Status != protocol.StatusValue {
		t.Fatalf("GET before TTL: got status 0x%02x, want VALUE", resp.Status)
	}

	time.Sleep(150 * time.Millisecond)

	resp = exec.Execute(&protocol.Command{Type: protocol.CmdGet, Key: "k"})
	if resp.Status != protocol.StatusNil {
		t.Errorf("GET after TTL: got status 0x%02x, want NIL", resp.Status)
	}
}

func TestExpire(t *testing.T) {
	exec := NewExecutor(newTestEngine())

	exec.Execute(&protocol.Command{Type: protocol.CmdSet, Key: "k", Value: []byte("v")})
	exec.Execute(&protocol.Command{Type: protocol.CmdExpire, Key: "k", TTL: 100 * time.Millisecond})

	time.Sleep(150 * time.Millisecond)

	resp := exec.Execute(&protocol.Command{Type: protocol.CmdGet, Key: "k"})
	if resp.Status != protocol.StatusNil {
		t.Errorf("GET after EXPIRE: got status 0x%02x, want NIL", resp.Status)
	}
}

func TestPing(t *testing.T) {
	exec := NewExecutor(newTestEngine())
	resp := exec.Execute(&protocol.Command{Type: protocol.CmdPing})
	if resp.Status != protocol.StatusValue {
		t.Fatalf("PING: got status 0x%02x, want VALUE", resp.Status)
	}
	if string(resp.Data) != "PONG" {
		t.Errorf("PING: got %q, want %q", resp.Data, "PONG")
	}
}

func TestKeys(t *testing.T) {
	exec := NewExecutor(newTestEngine())

	exec.Execute(&protocol.Command{Type: protocol.CmdSet, Key: "user:1", Value: []byte("a")})
	exec.Execute(&protocol.Command{Type: protocol.CmdSet, Key: "user:2", Value: []byte("b")})
	exec.Execute(&protocol.Command{Type: protocol.CmdSet, Key: "order:1", Value: []byte("c")})

	resp := exec.Execute(&protocol.Command{Type: protocol.CmdKeys, Pattern: "user:*"})
	if resp.Status != protocol.StatusArray {
		t.Fatalf("KEYS: got status 0x%02x, want ARRAY", resp.Status)
	}

	items, err := protocol.ParseArrayData(resp.Data)
	if err != nil {
		t.Fatalf("ParseArrayData: %v", err)
	}
	if len(items) != 2 {
		t.Errorf("KEYS user:*: got %d keys, want 2", len(items))
	}
}

func TestRange(t *testing.T) {
	exec := NewExecutor(newTestEngine())

	exec.Execute(&protocol.Command{Type: protocol.CmdSet, Key: "a", Value: []byte("1")})
	exec.Execute(&protocol.Command{Type: protocol.CmdSet, Key: "b", Value: []byte("2")})
	exec.Execute(&protocol.Command{Type: protocol.CmdSet, Key: "c", Value: []byte("3")})
	exec.Execute(&protocol.Command{Type: protocol.CmdSet, Key: "d", Value: []byte("4")})

	resp := exec.Execute(&protocol.Command{Type: protocol.CmdRange, StartKey: "b", EndKey: "c"})
	if resp.Status != protocol.StatusArray {
		t.Fatalf("RANGE: got status 0x%02x, want ARRAY", resp.Status)
	}

	items, err := protocol.ParseArrayData(resp.Data)
	if err != nil {
		t.Fatalf("ParseArrayData: %v", err)
	}
	// Should be [b, 2, c, 3] = 4 items (key-value pairs flattened).
	if len(items) != 4 {
		t.Errorf("RANGE b-c: got %d items, want 4 (2 key-value pairs)", len(items))
	}
}

func TestEviction(t *testing.T) {
	sl := engine.NewSkipList(16, 0.5)
	lru := eviction.NewLRU(1000)
	tm := ttl.NewManager()
	eng := engine.NewKVEngine(sl, lru, tm, 3) // max 3 keys
	exec := NewExecutor(eng)

	exec.Execute(&protocol.Command{Type: protocol.CmdSet, Key: "a", Value: []byte("1")})
	exec.Execute(&protocol.Command{Type: protocol.CmdSet, Key: "b", Value: []byte("2")})
	exec.Execute(&protocol.Command{Type: protocol.CmdSet, Key: "c", Value: []byte("3")})

	// Access "a" to make it recently used (so "b" is the LRU victim).
	exec.Execute(&protocol.Command{Type: protocol.CmdGet, Key: "a"})

	// Insert a 4th key — should evict "b" (least recently used).
	exec.Execute(&protocol.Command{Type: protocol.CmdSet, Key: "d", Value: []byte("4")})

	resp := exec.Execute(&protocol.Command{Type: protocol.CmdGet, Key: "b"})
	if resp.Status != protocol.StatusNil {
		t.Errorf("key 'b' should have been evicted, got status 0x%02x", resp.Status)
	}

	// "a", "c", "d" should still exist.
	for _, k := range []string{"a", "c", "d"} {
		resp = exec.Execute(&protocol.Command{Type: protocol.CmdGet, Key: k})
		if resp.Status != protocol.StatusValue {
			t.Errorf("key %q should exist after eviction", k)
		}
	}
}
