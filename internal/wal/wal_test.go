package wal

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func tempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "kache-wal-test-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

// --- Entry tests ---

func TestEntryRoundTrip(t *testing.T) {
	original := &Entry{
		Timestamp: uint64(time.Now().UnixNano()),
		CmdType:   0x02,
		Data:      []byte("SET foo bar"),
	}

	serialized := SerializeEntry(original)
	restored, err := DeserializeEntry(serialized)
	if err != nil {
		t.Fatalf("DeserializeEntry: %v", err)
	}

	if restored.CRC != original.CRC {
		t.Errorf("CRC mismatch: got %d, want %d", restored.CRC, original.CRC)
	}
	if restored.Timestamp != original.Timestamp {
		t.Errorf("Timestamp mismatch: got %d, want %d", restored.Timestamp, original.Timestamp)
	}
	if restored.CmdType != original.CmdType {
		t.Errorf("CmdType mismatch: got 0x%02x, want 0x%02x", restored.CmdType, original.CmdType)
	}
	if !bytes.Equal(restored.Data, original.Data) {
		t.Errorf("Data mismatch: got %q, want %q", restored.Data, original.Data)
	}
}

func TestEntryEmptyData(t *testing.T) {
	original := &Entry{
		Timestamp: 12345,
		CmdType:   0x05, // PING
		Data:      []byte{},
	}

	serialized := SerializeEntry(original)
	if len(serialized) != HeaderSize {
		t.Errorf("empty data entry should be %d bytes, got %d", HeaderSize, len(serialized))
	}

	restored, err := DeserializeEntry(serialized)
	if err != nil {
		t.Fatalf("DeserializeEntry: %v", err)
	}
	if restored.CmdType != 0x05 {
		t.Errorf("CmdType mismatch: got 0x%02x, want 0x05", restored.CmdType)
	}
}

func TestEntryCRCCorruption(t *testing.T) {
	original := &Entry{
		Timestamp: uint64(time.Now().UnixNano()),
		CmdType:   0x02,
		Data:      []byte("important data"),
	}

	serialized := SerializeEntry(original)

	// Corrupt one byte in the data section.
	serialized[HeaderSize] ^= 0xFF

	_, err := DeserializeEntry(serialized)
	if err != ErrInvalidCRC {
		t.Errorf("expected ErrInvalidCRC, got %v", err)
	}
}

func TestEntryTooShort(t *testing.T) {
	_, err := DeserializeEntry([]byte{0x01, 0x02, 0x03})
	if err != ErrTooShort {
		t.Errorf("expected ErrTooShort, got %v", err)
	}
}

// --- WAL tests ---

func TestWALCreateAndFlush(t *testing.T) {
	dir := tempDir(t)
	w, err := NewWAL(dir)
	if err != nil {
		t.Fatalf("NewWAL: %v", err)
	}

	for i := 0; i < 100; i++ {
		if err := w.Append(0x02, []byte("SET key value")); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}

	if err := w.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Verify the segment file exists and has non-zero size.
	segments, _ := filepath.Glob(filepath.Join(dir, "wal-*.log"))
	if len(segments) != 1 {
		t.Fatalf("expected 1 segment file, got %d", len(segments))
	}

	info, _ := os.Stat(segments[0])
	expectedMin := int64(100 * (HeaderSize + len("SET key value")))
	if info.Size() < expectedMin {
		t.Errorf("segment too small: %d bytes, expected at least %d", info.Size(), expectedMin)
	}
}

func TestWALReadAllEntries(t *testing.T) {
	dir := tempDir(t)
	w, err := NewWAL(dir)
	if err != nil {
		t.Fatalf("NewWAL: %v", err)
	}

	// Write 50 entries.
	for i := 0; i < 50; i++ {
		w.Append(0x02, []byte("hello"))
	}
	w.Flush()
	w.Close()

	// Re-open and read.
	w2, err := NewWAL(dir)
	if err != nil {
		t.Fatalf("NewWAL (reopen): %v", err)
	}
	defer w2.Close()

	entries, err := w2.ReadAllEntries()
	if err != nil {
		t.Fatalf("ReadAllEntries: %v", err)
	}
	if len(entries) != 50 {
		t.Errorf("expected 50 entries, got %d", len(entries))
	}
	for _, e := range entries {
		if e.CmdType != 0x02 {
			t.Errorf("unexpected CmdType: 0x%02x", e.CmdType)
		}
		if !bytes.Equal(e.Data, []byte("hello")) {
			t.Errorf("unexpected data: %q", e.Data)
		}
	}
}

func TestWALCorruptEntrySkipped(t *testing.T) {
	dir := tempDir(t)
	w, err := NewWAL(dir)
	if err != nil {
		t.Fatalf("NewWAL: %v", err)
	}

	// Write 3 entries.
	w.Append(0x02, []byte("entry1"))
	w.Append(0x02, []byte("entry2"))
	w.Append(0x02, []byte("entry3"))
	w.Flush()
	w.Close()

	// Corrupt the middle entry.
	segments, _ := filepath.Glob(filepath.Join(dir, "wal-*.log"))
	data, _ := os.ReadFile(segments[0])

	// First entry size.
	entrySize := HeaderSize + len("entry1")
	// Corrupt a byte in the second entry's data.
	data[entrySize+HeaderSize] ^= 0xFF
	os.WriteFile(segments[0], data, 0644)

	// Re-open and read — should get 2 entries (1st and 3rd).
	w2, err := NewWAL(dir)
	if err != nil {
		t.Fatalf("NewWAL: %v", err)
	}
	defer w2.Close()

	entries, err := w2.ReadAllEntries()
	if err != nil {
		t.Fatalf("ReadAllEntries: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries (skipping corrupt), got %d", len(entries))
	}
	if !bytes.Equal(entries[0].Data, []byte("entry1")) {
		t.Errorf("first entry data: got %q, want %q", entries[0].Data, "entry1")
	}
	if !bytes.Equal(entries[1].Data, []byte("entry3")) {
		t.Errorf("second entry data: got %q, want %q", entries[1].Data, "entry3")
	}
}

func TestWALSegmentRotation(t *testing.T) {
	dir := tempDir(t)
	w, err := NewWAL(dir)
	if err != nil {
		t.Fatalf("NewWAL: %v", err)
	}

	// Write enough data to trigger at least one rotation.
	// Use a large value to fill up 64MB quickly.
	bigData := make([]byte, 1024*1024) // 1MB per entry
	for i := 0; i < 70; i++ {
		w.Append(0x02, bigData)
		w.Flush() // Flush each to trigger size check.
	}
	w.Close()

	segments, _ := filepath.Glob(filepath.Join(dir, "wal-*.log"))
	if len(segments) < 2 {
		t.Errorf("expected at least 2 segment files after rotation, got %d", len(segments))
	}
}

func TestWALConcurrentAppends(t *testing.T) {
	dir := tempDir(t)
	w, err := NewWAL(dir)
	if err != nil {
		t.Fatalf("NewWAL: %v", err)
	}

	const numGoroutines = 10
	const appendsPerGoroutine = 100

	var wg sync.WaitGroup
	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < appendsPerGoroutine; i++ {
				w.Append(0x02, []byte("concurrent"))
			}
		}()
	}
	wg.Wait()
	w.Flush()
	w.Close()

	// Verify all entries are readable.
	w2, err := NewWAL(dir)
	if err != nil {
		t.Fatalf("NewWAL: %v", err)
	}
	defer w2.Close()

	entries, err := w2.ReadAllEntries()
	if err != nil {
		t.Fatalf("ReadAllEntries: %v", err)
	}
	expected := numGoroutines * appendsPerGoroutine
	if len(entries) != expected {
		t.Errorf("expected %d entries, got %d", expected, len(entries))
	}
}

func TestWALBackgroundFlush(t *testing.T) {
	dir := tempDir(t)
	w, err := NewWAL(dir)
	if err != nil {
		t.Fatalf("NewWAL: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	w.BackgroundFlush(ctx, 50*time.Millisecond)

	// Append without manually flushing.
	w.Append(0x02, []byte("background"))

	// Wait for at least one background flush cycle.
	time.Sleep(150 * time.Millisecond)
	cancel()
	time.Sleep(100 * time.Millisecond) // Let final flush complete.

	// Verify the entry was flushed to disk.
	w2, err := NewWAL(dir)
	if err != nil {
		t.Fatalf("NewWAL: %v", err)
	}
	defer w2.Close()

	entries, err := w2.ReadAllEntries()
	if err != nil {
		t.Fatalf("ReadAllEntries: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry from background flush, got %d", len(entries))
	}
	if !bytes.Equal(entries[0].Data, []byte("background")) {
		t.Errorf("data mismatch: got %q", entries[0].Data)
	}
}

func TestWALReopenAndAppend(t *testing.T) {
	dir := tempDir(t)

	// First session: write 5 entries.
	w1, err := NewWAL(dir)
	if err != nil {
		t.Fatalf("NewWAL: %v", err)
	}
	for i := 0; i < 5; i++ {
		w1.Append(0x02, []byte("session1"))
	}
	w1.Close()

	// Second session: write 5 more.
	w2, err := NewWAL(dir)
	if err != nil {
		t.Fatalf("NewWAL: %v", err)
	}
	for i := 0; i < 5; i++ {
		w2.Append(0x03, []byte("session2"))
	}
	w2.Close()

	// Read all — should be 10 entries.
	w3, err := NewWAL(dir)
	if err != nil {
		t.Fatalf("NewWAL: %v", err)
	}
	defer w3.Close()

	entries, err := w3.ReadAllEntries()
	if err != nil {
		t.Fatalf("ReadAllEntries: %v", err)
	}
	if len(entries) != 10 {
		t.Fatalf("expected 10 entries, got %d", len(entries))
	}
	for i := 0; i < 5; i++ {
		if entries[i].CmdType != 0x02 {
			t.Errorf("entry %d: CmdType = 0x%02x, want 0x02", i, entries[i].CmdType)
		}
	}
	for i := 5; i < 10; i++ {
		if entries[i].CmdType != 0x03 {
			t.Errorf("entry %d: CmdType = 0x%02x, want 0x03", i, entries[i].CmdType)
		}
	}
}
