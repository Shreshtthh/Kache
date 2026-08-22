package ttl

import (
	"context"
	"sync"
	"time"
)

// Manager tracks per-key expiration timestamps.
// Supports lazy expiration (check on GET) and active expiration (background sweep).
type Manager struct {
	expirations map[string]time.Time
	mu          sync.RWMutex
}

// NewManager creates a new TTL Manager.
func NewManager() *Manager {
	return &Manager{
		expirations: make(map[string]time.Time),
	}
}

// SetTTL sets an expiration time for the given key.
func (m *Manager) SetTTL(key string, d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.expirations[key] = time.Now().Add(d)
}

// IsExpired checks whether a key has expired.
// Returns false if the key has no TTL set.
func (m *Manager) IsExpired(key string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	expiry, ok := m.expirations[key]
	if !ok {
		return false
	}
	return time.Now().After(expiry)
}

// RemoveTTL removes the TTL for a key (called on DEL).
func (m *Manager) RemoveTTL(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.expirations, key)
}

// HasTTL returns true if the key has a TTL set (regardless of whether it's expired).
func (m *Manager) HasTTL(key string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.expirations[key]
	return ok
}

// CleanExpired iterates over all keys and calls onExpire for each expired key.
// This is the active expiration sweep.
func (m *Manager) CleanExpired(onExpire func(key string)) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	var expired []string
	for key, expiry := range m.expirations {
		if now.After(expiry) {
			expired = append(expired, key)
		}
	}
	for _, key := range expired {
		delete(m.expirations, key)
		onExpire(key)
	}
}

// StartActiveSweep runs a background goroutine that periodically sweeps expired keys.
// The goroutine stops when the context is cancelled.
func (m *Manager) StartActiveSweep(ctx context.Context, interval time.Duration, onExpire func(key string)) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.CleanExpired(onExpire)
			}
		}
	}()
}
