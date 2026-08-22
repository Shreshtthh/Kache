package eviction

// Policy defines the eviction strategy.
// Implementations are injected into the KV Engine at startup (Strategy Pattern).
type Policy interface {
	// RecordAccess is called on every GET or SET to update access metadata.
	RecordAccess(key string)

	// Evict returns the key that should be evicted, or ("", false) if empty.
	Evict() (key string, ok bool)

	// Remove is called when a key is explicitly deleted (DEL command).
	Remove(key string)

	// Len returns the number of keys tracked by the policy.
	Len() int
}
