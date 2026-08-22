package eviction

import "container/list"

// lruEntry is stored as the Value in each list.Element.
type lruEntry struct {
	key string
}

// LRU implements the Policy interface using a doubly-linked list + hash map.
// On access: move to front. On eviction: remove from back. All operations O(1).
type LRU struct {
	capacity int
	items    map[string]*list.Element
	order    *list.List // front = most recent, back = least recent
}

// NewLRU creates an LRU policy with the given capacity.
func NewLRU(capacity int) *LRU {
	return &LRU{
		capacity: capacity,
		items:    make(map[string]*list.Element),
		order:    list.New(),
	}
}

// RecordAccess moves the key to the front (most recently used).
// If the key is new, it's added to the front.
func (l *LRU) RecordAccess(key string) {
	if elem, ok := l.items[key]; ok {
		l.order.MoveToFront(elem)
		return
	}
	elem := l.order.PushFront(&lruEntry{key: key})
	l.items[key] = elem
}

// Evict removes and returns the least recently used key.
func (l *LRU) Evict() (string, bool) {
	back := l.order.Back()
	if back == nil {
		return "", false
	}
	entry := back.Value.(*lruEntry)
	l.order.Remove(back)
	delete(l.items, entry.key)
	return entry.key, true
}

// Remove explicitly removes a key from the eviction tracker.
func (l *LRU) Remove(key string) {
	if elem, ok := l.items[key]; ok {
		l.order.Remove(elem)
		delete(l.items, key)
	}
}

// Len returns the number of tracked keys.
func (l *LRU) Len() int {
	return l.order.Len()
}
