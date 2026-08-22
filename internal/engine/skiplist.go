package engine

import (
	"math/rand"
	"sync"
)

const defaultMaxLevel = 16
const defaultP = 0.5

// KeyValue represents a key-value pair returned by range queries.
type KeyValue struct {
	Key   string
	Value []byte
}

type skipListNode struct {
	key     string
	value   []byte
	forward []*skipListNode
}

// SkipList is an ordered map supporting O(log N) insert, search, delete, and range queries.
type SkipList struct {
	head     *skipListNode
	maxLevel int
	p        float64
	level    int // current highest level in use
	length   int
	mu       sync.RWMutex
	rng      *rand.Rand
}

// NewSkipList creates a new SkipList with the given max level and probability.
func NewSkipList(maxLevel int, p float64) *SkipList {
	if maxLevel <= 0 {
		maxLevel = defaultMaxLevel
	}
	if p <= 0 || p >= 1 {
		p = defaultP
	}
	head := &skipListNode{
		forward: make([]*skipListNode, maxLevel),
	}
	return &SkipList{
		head:     head,
		maxLevel: maxLevel,
		p:        p,
		level:    0,
		rng:      rand.New(rand.NewSource(rand.Int63())),
	}
}

// randomLevel generates a random level for a new node using coin-flip probability.
func (sl *SkipList) randomLevel() int {
	lvl := 1
	for lvl < sl.maxLevel && sl.rng.Float64() < sl.p {
		lvl++
	}
	return lvl
}

// Insert adds or updates a key-value pair in the skip list.
func (sl *SkipList) Insert(key string, value []byte) {
	sl.mu.Lock()
	defer sl.mu.Unlock()

	update := make([]*skipListNode, sl.maxLevel)
	current := sl.head

	// Traverse from the highest level down to find the insertion point.
	for i := sl.level - 1; i >= 0; i-- {
		for current.forward[i] != nil && current.forward[i].key < key {
			current = current.forward[i]
		}
		update[i] = current
	}

	// Check if key already exists — update value if so.
	next := current.forward[0]
	if next != nil && next.key == key {
		next.value = make([]byte, len(value))
		copy(next.value, value)
		return
	}

	// Generate a random level for the new node.
	newLevel := sl.randomLevel()

	// If the new level is higher than the current level, update the
	// update slice to point the head at these new levels.
	if newLevel > sl.level {
		for i := sl.level; i < newLevel; i++ {
			update[i] = sl.head
		}
		sl.level = newLevel
	}

	// Create the new node and link it into each level.
	newNode := &skipListNode{
		key:     key,
		value:   make([]byte, len(value)),
		forward: make([]*skipListNode, newLevel),
	}
	copy(newNode.value, value)

	for i := 0; i < newLevel; i++ {
		newNode.forward[i] = update[i].forward[i]
		update[i].forward[i] = newNode
	}

	sl.length++
}

// Search looks up a key and returns its value. Returns (nil, false) if not found.
func (sl *SkipList) Search(key string) ([]byte, bool) {
	sl.mu.RLock()
	defer sl.mu.RUnlock()

	current := sl.head
	for i := sl.level - 1; i >= 0; i-- {
		for current.forward[i] != nil && current.forward[i].key < key {
			current = current.forward[i]
		}
	}

	next := current.forward[0]
	if next != nil && next.key == key {
		result := make([]byte, len(next.value))
		copy(result, next.value)
		return result, true
	}
	return nil, false
}

// Delete removes a key from the skip list. Returns true if the key was found and removed.
func (sl *SkipList) Delete(key string) bool {
	sl.mu.Lock()
	defer sl.mu.Unlock()

	update := make([]*skipListNode, sl.maxLevel)
	current := sl.head

	for i := sl.level - 1; i >= 0; i-- {
		for current.forward[i] != nil && current.forward[i].key < key {
			current = current.forward[i]
		}
		update[i] = current
	}

	target := current.forward[0]
	if target == nil || target.key != key {
		return false
	}

	// Unlink the target node from each level.
	for i := 0; i < sl.level; i++ {
		if update[i].forward[i] != target {
			break
		}
		update[i].forward[i] = target.forward[i]
	}

	// Reduce the level if the highest levels are now empty.
	for sl.level > 0 && sl.head.forward[sl.level-1] == nil {
		sl.level--
	}

	sl.length--
	return true
}

// Range returns all key-value pairs where start <= key <= end (lexicographic, both inclusive).
func (sl *SkipList) Range(start, end string) []KeyValue {
	sl.mu.RLock()
	defer sl.mu.RUnlock()

	var result []KeyValue

	// Find the first node >= start.
	current := sl.head
	for i := sl.level - 1; i >= 0; i-- {
		for current.forward[i] != nil && current.forward[i].key < start {
			current = current.forward[i]
		}
	}

	// Walk forward from the first match, collecting until > end.
	current = current.forward[0]
	for current != nil && current.key <= end {
		val := make([]byte, len(current.value))
		copy(val, current.value)
		result = append(result, KeyValue{Key: current.key, Value: val})
		current = current.forward[0]
	}

	return result
}

// Len returns the number of entries in the skip list.
func (sl *SkipList) Len() int {
	sl.mu.RLock()
	defer sl.mu.RUnlock()
	return sl.length
}

// Keys returns all keys in sorted order. Used for KEYS command with pattern matching.
func (sl *SkipList) Keys() []string {
	sl.mu.RLock()
	defer sl.mu.RUnlock()

	keys := make([]string, 0, sl.length)
	current := sl.head.forward[0]
	for current != nil {
		keys = append(keys, current.key)
		current = current.forward[0]
	}
	return keys
}
