package engine

import (
	"path/filepath"
	"sync"
	"time"

	"github.com/kache-store/kache/internal/eviction"
	"github.com/kache-store/kache/internal/ttl"
)

// Engine is the public interface for the KV storage layer.
// The Raft layer and Command executor depend on this interface, not the concrete KVEngine.
type Engine interface {
	Get(key string) ([]byte, error)
	Set(key string, value []byte) error
	SetWithTTL(key string, value []byte, d time.Duration) error
	Delete(key string) error
	Expire(key string, d time.Duration) error
	Keys(pattern string) []string
	Range(start, end string) []KeyValue
}

// KVEngine is the orchestrator for the storage layer.
// It coordinates the SkipList, eviction policy, and TTL manager.
type KVEngine struct {
	store    *SkipList
	eviction eviction.Policy
	ttl      *ttl.Manager
	maxKeys  int
	mu       sync.RWMutex
}

// NewKVEngine creates a new KVEngine with the given components.
func NewKVEngine(store *SkipList, evictionPolicy eviction.Policy, ttlManager *ttl.Manager, maxKeys int) *KVEngine {
	return &KVEngine{
		store:    store,
		eviction: evictionPolicy,
		ttl:      ttlManager,
		maxKeys:  maxKeys,
	}
}

// Get retrieves the value for a key. Returns (nil, nil) if the key doesn't exist.
// Performs lazy TTL expiration: if the key is expired, it's deleted and nil is returned.
func (e *KVEngine) Get(key string) ([]byte, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Lazy expiration check.
	if e.ttl.IsExpired(key) {
		e.store.Delete(key)
		e.eviction.Remove(key)
		e.ttl.RemoveTTL(key)
		return nil, nil
	}

	val, found := e.store.Search(key)
	if !found {
		return nil, nil
	}

	e.eviction.RecordAccess(key)
	return val, nil
}

// Set stores a key-value pair. Triggers eviction if the store exceeds maxKeys.
func (e *KVEngine) Set(key string, value []byte) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.store.Insert(key, value)
	e.eviction.RecordAccess(key)

	// Evict if over capacity.
	for e.store.Len() > e.maxKeys {
		victim, ok := e.eviction.Evict()
		if !ok {
			break
		}
		e.store.Delete(victim)
		e.ttl.RemoveTTL(victim)
	}

	return nil
}

// SetWithTTL stores a key-value pair with an expiration time.
func (e *KVEngine) SetWithTTL(key string, value []byte, d time.Duration) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.store.Insert(key, value)
	e.eviction.RecordAccess(key)
	e.ttl.SetTTL(key, d)

	for e.store.Len() > e.maxKeys {
		victim, ok := e.eviction.Evict()
		if !ok {
			break
		}
		e.store.Delete(victim)
		e.ttl.RemoveTTL(victim)
	}

	return nil
}

// Delete removes a key from the store.
func (e *KVEngine) Delete(key string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.store.Delete(key)
	e.eviction.Remove(key)
	e.ttl.RemoveTTL(key)
	return nil
}

// Expire sets a TTL on an existing key.
func (e *KVEngine) Expire(key string, d time.Duration) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	_, found := e.store.Search(key)
	if !found {
		return nil
	}
	e.ttl.SetTTL(key, d)
	return nil
}

// Keys returns all keys matching the given glob pattern.
// An empty pattern or "*" matches all keys.
func (e *KVEngine) Keys(pattern string) []string {
	e.mu.RLock()
	defer e.mu.RUnlock()

	allKeys := e.store.Keys()
	if pattern == "" || pattern == "*" {
		return allKeys
	}

	var matched []string
	for _, k := range allKeys {
		if ok, _ := filepath.Match(pattern, k); ok {
			matched = append(matched, k)
		}
	}
	return matched
}

// Range returns key-value pairs where start <= key <= end (lexicographic).
func (e *KVEngine) Range(start, end string) []KeyValue {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.store.Range(start, end)
}
