package eviction

import (
	"testing"
)

// --- Shared interface tests ---
// These tests work with any Policy implementation.

func testPolicyBasicEviction(t *testing.T, p Policy, name string) {
	t.Helper()

	// Track 3 keys.
	p.RecordAccess("a")
	p.RecordAccess("b")
	p.RecordAccess("c")

	if p.Len() != 3 {
		t.Errorf("[%s] Len() = %d, want 3", name, p.Len())
	}

	// Evict one — should succeed.
	key, ok := p.Evict()
	if !ok {
		t.Errorf("[%s] Evict() ok = false, want true", name)
	}
	if key == "" {
		t.Errorf("[%s] Evict() returned empty key", name)
	}
	if p.Len() != 2 {
		t.Errorf("[%s] After evict, Len() = %d, want 2", name, p.Len())
	}
}

func testPolicyEvictEmpty(t *testing.T, p Policy, name string) {
	t.Helper()
	_, ok := p.Evict()
	if ok {
		t.Errorf("[%s] Evict() on empty policy should return false", name)
	}
}

func testPolicyRemove(t *testing.T, p Policy, name string) {
	t.Helper()
	p.RecordAccess("a")
	p.RecordAccess("b")
	p.Remove("a")
	if p.Len() != 1 {
		t.Errorf("[%s] After Remove, Len() = %d, want 1", name, p.Len())
	}

	// Evicting should return "b", not "a".
	key, ok := p.Evict()
	if !ok || key != "b" {
		t.Errorf("[%s] After removing 'a', Evict() = (%q, %v), want ('b', true)", name, key, ok)
	}
}

func TestPolicyInterfaceLRU(t *testing.T) {
	t.Run("BasicEviction", func(t *testing.T) { testPolicyBasicEviction(t, NewLRU(100), "LRU") })
	t.Run("EvictEmpty", func(t *testing.T) { testPolicyEvictEmpty(t, NewLRU(100), "LRU") })
	t.Run("Remove", func(t *testing.T) { testPolicyRemove(t, NewLRU(100), "LRU") })
}

func TestPolicyInterfaceLFU(t *testing.T) {
	t.Run("BasicEviction", func(t *testing.T) { testPolicyBasicEviction(t, NewLFU(100), "LFU") })
	t.Run("EvictEmpty", func(t *testing.T) { testPolicyEvictEmpty(t, NewLFU(100), "LFU") })
	t.Run("Remove", func(t *testing.T) { testPolicyRemove(t, NewLFU(100), "LFU") })
}

// --- LRU-specific tests ---

func TestLRUEvictionOrder(t *testing.T) {
	lru := NewLRU(100)

	// Insert in order: a, b, c, d, e
	lru.RecordAccess("a")
	lru.RecordAccess("b")
	lru.RecordAccess("c")
	lru.RecordAccess("d")
	lru.RecordAccess("e")

	// Eviction should be FIFO (a first, since it was accessed longest ago).
	key, _ := lru.Evict()
	if key != "a" {
		t.Errorf("First eviction: got %q, want 'a'", key)
	}
	key, _ = lru.Evict()
	if key != "b" {
		t.Errorf("Second eviction: got %q, want 'b'", key)
	}
}

func TestLRUAccessMovesToFront(t *testing.T) {
	lru := NewLRU(100)

	lru.RecordAccess("a")
	lru.RecordAccess("b")
	lru.RecordAccess("c")

	// Access "a" again — moves it to front.
	lru.RecordAccess("a")

	// Now eviction order should be b, c, a.
	key, _ := lru.Evict()
	if key != "b" {
		t.Errorf("After re-access, first eviction: got %q, want 'b'", key)
	}
	key, _ = lru.Evict()
	if key != "c" {
		t.Errorf("After re-access, second eviction: got %q, want 'c'", key)
	}
	key, _ = lru.Evict()
	if key != "a" {
		t.Errorf("After re-access, third eviction: got %q, want 'a'", key)
	}
}

// --- LFU-specific tests ---

func TestLFUEvictsLeastFrequent(t *testing.T) {
	lfu := NewLFU(100)

	// "a" accessed 3 times, "b" accessed 1 time, "c" accessed 2 times.
	lfu.RecordAccess("a")
	lfu.RecordAccess("a")
	lfu.RecordAccess("a")
	lfu.RecordAccess("b")
	lfu.RecordAccess("c")
	lfu.RecordAccess("c")

	// Evict should pick "b" (frequency 1).
	key, _ := lfu.Evict()
	if key != "b" {
		t.Errorf("First eviction: got %q, want 'b' (freq=1)", key)
	}

	// Next eviction should pick "c" (frequency 2).
	key, _ = lfu.Evict()
	if key != "c" {
		t.Errorf("Second eviction: got %q, want 'c' (freq=2)", key)
	}
}

func TestLFUTieBrokenByRecency(t *testing.T) {
	lfu := NewLFU(100)

	// All keys accessed once — tie at frequency 1.
	lfu.RecordAccess("a") // oldest among freq=1
	lfu.RecordAccess("b")
	lfu.RecordAccess("c") // newest among freq=1

	// Evict should pick "a" (oldest in the freq=1 bucket).
	key, _ := lfu.Evict()
	if key != "a" {
		t.Errorf("Tie-breaking eviction: got %q, want 'a' (oldest in freq=1)", key)
	}
}

func TestLFUMinFreqUpdates(t *testing.T) {
	lfu := NewLFU(100)

	lfu.RecordAccess("a") // freq=1
	lfu.RecordAccess("b") // freq=1
	lfu.RecordAccess("a") // freq=2

	// Evict "b" (freq=1), removing the only item at freq=1.
	key, _ := lfu.Evict()
	if key != "b" {
		t.Errorf("Eviction: got %q, want 'b'", key)
	}

	// Now only "a" remains at freq=2. minFreq should be 2.
	key, _ = lfu.Evict()
	if key != "a" {
		t.Errorf("Second eviction: got %q, want 'a'", key)
	}
}
