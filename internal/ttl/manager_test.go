package ttl

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestSetTTLAndIsExpired(t *testing.T) {
	m := NewManager()
	m.SetTTL("key1", 100*time.Millisecond)

	if m.IsExpired("key1") {
		t.Error("key1 should not be expired immediately after SetTTL")
	}

	time.Sleep(150 * time.Millisecond)

	if !m.IsExpired("key1") {
		t.Error("key1 should be expired after 150ms (TTL was 100ms)")
	}
}

func TestNoTTLNotExpired(t *testing.T) {
	m := NewManager()
	if m.IsExpired("no-ttl-key") {
		t.Error("key without TTL should never be considered expired")
	}
}

func TestRemoveTTL(t *testing.T) {
	m := NewManager()
	m.SetTTL("key1", 100*time.Millisecond)
	m.RemoveTTL("key1")

	time.Sleep(150 * time.Millisecond)

	if m.IsExpired("key1") {
		t.Error("key1 should not be expired after RemoveTTL")
	}
}

func TestHasTTL(t *testing.T) {
	m := NewManager()
	if m.HasTTL("x") {
		t.Error("HasTTL should return false for unknown key")
	}
	m.SetTTL("x", time.Second)
	if !m.HasTTL("x") {
		t.Error("HasTTL should return true after SetTTL")
	}
}

func TestCleanExpired(t *testing.T) {
	m := NewManager()
	m.SetTTL("a", 50*time.Millisecond)
	m.SetTTL("b", 50*time.Millisecond)
	m.SetTTL("c", 50*time.Millisecond)
	m.SetTTL("keep", 10*time.Second) // should NOT be cleaned

	time.Sleep(100 * time.Millisecond)

	cleaned := make(map[string]bool)
	m.CleanExpired(func(key string) {
		cleaned[key] = true
	})

	for _, k := range []string{"a", "b", "c"} {
		if !cleaned[k] {
			t.Errorf("key %q should have been cleaned", k)
		}
	}
	if cleaned["keep"] {
		t.Error("key 'keep' should NOT have been cleaned")
	}
}

func TestActiveSweep(t *testing.T) {
	m := NewManager()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var swept int64
	m.StartActiveSweep(ctx, 50*time.Millisecond, func(key string) {
		atomic.AddInt64(&swept, 1)
	})

	m.SetTTL("expire-me", 30*time.Millisecond)

	// Wait for the sweep to pick it up.
	time.Sleep(200 * time.Millisecond)

	if atomic.LoadInt64(&swept) == 0 {
		t.Error("active sweep should have cleaned the expired key")
	}
}

func TestActiveSweepStopsOnCancel(t *testing.T) {
	m := NewManager()
	ctx, cancel := context.WithCancel(context.Background())

	var swept int64
	m.StartActiveSweep(ctx, 50*time.Millisecond, func(key string) {
		atomic.AddInt64(&swept, 1)
	})

	cancel()
	time.Sleep(100 * time.Millisecond)

	// Set a key that would expire — sweep should NOT clean it because ctx is cancelled.
	m.SetTTL("after-cancel", 10*time.Millisecond)
	time.Sleep(100 * time.Millisecond)

	if atomic.LoadInt64(&swept) != 0 {
		t.Error("active sweep should not fire after context is cancelled")
	}
}
