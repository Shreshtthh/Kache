package eviction

import "container/list"

// lfuEntry tracks access frequency for a single key.
type lfuEntry struct {
	key  string
	freq int
}

// LFU implements the Policy interface using frequency buckets.
// On access: increment frequency, move to next bucket.
// On eviction: remove from lowest frequency bucket (ties broken by recency — tail of the bucket list).
// All operations O(1) amortized.
type LFU struct {
	capacity    int
	items       map[string]*list.Element  // key → list.Element (containing *lfuEntry)
	freqBuckets map[int]*list.List        // frequency → doubly-linked list of entries at that freq
	minFreq     int
}

// NewLFU creates an LFU policy with the given capacity.
func NewLFU(capacity int) *LFU {
	return &LFU{
		capacity:    capacity,
		items:       make(map[string]*list.Element),
		freqBuckets: make(map[int]*list.List),
		minFreq:     0,
	}
}

// getOrCreateBucket returns (or creates) the doubly-linked list for a frequency.
func (l *LFU) getOrCreateBucket(freq int) *list.List {
	if bucket, ok := l.freqBuckets[freq]; ok {
		return bucket
	}
	bucket := list.New()
	l.freqBuckets[freq] = bucket
	return bucket
}

// RecordAccess increments the frequency of the key.
// New keys start at frequency 1 and are added to the front of the freq=1 bucket.
func (l *LFU) RecordAccess(key string) {
	if elem, ok := l.items[key]; ok {
		entry := elem.Value.(*lfuEntry)
		oldFreq := entry.freq

		// Remove from old frequency bucket.
		oldBucket := l.freqBuckets[oldFreq]
		oldBucket.Remove(elem)
		if oldBucket.Len() == 0 {
			delete(l.freqBuckets, oldFreq)
			if l.minFreq == oldFreq {
				l.minFreq++
			}
		}

		// Move to new frequency bucket.
		entry.freq++
		newBucket := l.getOrCreateBucket(entry.freq)
		newElem := newBucket.PushFront(entry)
		l.items[key] = newElem
		return
	}

	// New key: frequency starts at 1.
	entry := &lfuEntry{key: key, freq: 1}
	bucket := l.getOrCreateBucket(1)
	elem := bucket.PushFront(entry)
	l.items[key] = elem
	l.minFreq = 1
}

// Evict removes and returns the least frequently used key.
// Ties are broken by recency (oldest at the back of the bucket list).
// Evict removes and returns the least frequently used key.
// Ties are broken by recency (oldest at the back of the bucket list).
func (l *LFU) Evict() (string, bool) {
	bucket, ok := l.freqBuckets[l.minFreq]
	if !ok || bucket.Len() == 0 {
		return "", false
	}

	// Evict from the back (least recently added among the least frequent).
	back := bucket.Back()
	entry := back.Value.(*lfuEntry)
	bucket.Remove(back)
	delete(l.items, entry.key)

	if bucket.Len() == 0 {
		delete(l.freqBuckets, l.minFreq)
		// Update minFreq to the next populated bucket.
		if len(l.items) > 0 {
			l.minFreq = l.findMinFreq()
		}
	}

	return entry.key, true
}

// findMinFreq scans freqBuckets for the lowest frequency with entries.
func (l *LFU) findMinFreq() int {
	min := int(^uint(0) >> 1) // MaxInt
	for freq, bucket := range l.freqBuckets {
		if bucket.Len() > 0 && freq < min {
			min = freq
		}
	}
	return min
}


// Remove explicitly removes a key from the eviction tracker.
func (l *LFU) Remove(key string) {
	elem, ok := l.items[key]
	if !ok {
		return
	}
	entry := elem.Value.(*lfuEntry)
	bucket := l.freqBuckets[entry.freq]
	bucket.Remove(elem)
	if bucket.Len() == 0 {
		delete(l.freqBuckets, entry.freq)
	}
	delete(l.items, key)
}

// Len returns the number of tracked keys.
func (l *LFU) Len() int {
	return len(l.items)
}
