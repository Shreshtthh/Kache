package wal

import (
	"context"
	"encoding/binary"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	// MaxSegmentSize is the maximum size of a single WAL segment file (64MB).
	MaxSegmentSize = 64 * 1024 * 1024
	// segmentPrefix is the filename prefix for WAL segment files.
	segmentPrefix = "wal-"
	// segmentSuffix is the filename extension for WAL segment files.
	segmentSuffix = ".log"
)

// WAL is an append-only write-ahead log.
// It buffers entries in memory and flushes them to disk periodically or on demand.
type WAL struct {
	dir        string
	segmentSeq int
	file       *os.File
	fileSize   int64
	mu         sync.Mutex
	buf        []byte
}

// NewWAL opens (or creates) a WAL in the given directory.
// It picks up where the last segment left off.
func NewWAL(dir string) (*WAL, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("creating WAL directory: %w", err)
	}

	w := &WAL{dir: dir}

	// Find existing segments and continue from the latest.
	segments, err := w.listSegments()
	if err != nil {
		return nil, err
	}

	if len(segments) > 0 {
		last := segments[len(segments)-1]
		// Parse sequence number from filename.
		name := filepath.Base(last)
		name = strings.TrimPrefix(name, segmentPrefix)
		name = strings.TrimSuffix(name, segmentSuffix)
		fmt.Sscanf(name, "%06d", &w.segmentSeq)

		f, err := os.OpenFile(last, os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return nil, fmt.Errorf("opening existing segment: %w", err)
		}
		info, err := f.Stat()
		if err != nil {
			f.Close()
			return nil, err
		}
		w.file = f
		w.fileSize = info.Size()
	} else {
		if err := w.rotate(); err != nil {
			return nil, err
		}
	}

	return w, nil
}

// Append adds an entry to the WAL buffer.
// The entry is not durable until Flush is called.
func (w *WAL) Append(cmdType byte, data []byte) error {
	entry := &Entry{
		Timestamp: uint64(time.Now().UnixNano()),
		CmdType:   cmdType,
		Data:      data,
	}
	serialized := SerializeEntry(entry)

	w.mu.Lock()
	defer w.mu.Unlock()

	w.buf = append(w.buf, serialized...)
	return nil
}

// Flush writes the in-memory buffer to disk and calls fsync.
func (w *WAL) Flush() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if len(w.buf) == 0 {
		return nil
	}

	// Check if we need to rotate before writing.
	if w.fileSize+int64(len(w.buf)) > MaxSegmentSize {
		if err := w.rotate(); err != nil {
			return err
		}
	}

	n, err := w.file.Write(w.buf)
	if err != nil {
		return fmt.Errorf("writing WAL buffer: %w", err)
	}
	w.fileSize += int64(n)

	if err := w.file.Sync(); err != nil {
		return fmt.Errorf("fsync WAL: %w", err)
	}

	w.buf = w.buf[:0]
	return nil
}

// BackgroundFlush starts a goroutine that periodically flushes the WAL.
// It stops when the context is cancelled.
func (w *WAL) BackgroundFlush(ctx context.Context, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				// Final flush on shutdown.
				if err := w.Flush(); err != nil {
					log.Printf("WAL final flush error: %v", err)
				}
				return
			case <-ticker.C:
				if err := w.Flush(); err != nil {
					log.Printf("WAL flush error: %v", err)
				}
			}
		}
	}()
}

// Close performs a final flush and closes the current segment file.
func (w *WAL) Close() error {
	if err := w.Flush(); err != nil {
		return err
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	if w.file != nil {
		return w.file.Close()
	}
	return nil
}

// Dir returns the WAL directory path.
func (w *WAL) Dir() string {
	return w.dir
}

// rotate closes the current segment and opens a new one.
// Must be called with w.mu held.
func (w *WAL) rotate() error {
	if w.file != nil {
		if err := w.file.Sync(); err != nil {
			return err
		}
		if err := w.file.Close(); err != nil {
			return err
		}
	}

	w.segmentSeq++
	name := fmt.Sprintf("%s%06d%s", segmentPrefix, w.segmentSeq, segmentSuffix)
	path := filepath.Join(w.dir, name)

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("creating WAL segment %s: %w", name, err)
	}

	w.file = f
	w.fileSize = 0
	return nil
}

// listSegments returns sorted paths of all WAL segment files in the directory.
func (w *WAL) listSegments() ([]string, error) {
	entries, err := os.ReadDir(w.dir)
	if err != nil {
		return nil, fmt.Errorf("reading WAL directory: %w", err)
	}

	var segments []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), segmentPrefix) && strings.HasSuffix(e.Name(), segmentSuffix) {
			segments = append(segments, filepath.Join(w.dir, e.Name()))
		}
	}
	sort.Strings(segments)
	return segments, nil
}

// ReadAllEntries reads every entry from all WAL segments in order.
// Entries with invalid CRCs are skipped (logged as warnings).
func (w *WAL) ReadAllEntries() ([]*Entry, error) {
	segments, err := w.listSegments()
	if err != nil {
		return nil, err
	}

	var entries []*Entry
	for _, seg := range segments {
		segEntries, err := readSegment(seg)
		if err != nil {
			return nil, fmt.Errorf("reading segment %s: %w", seg, err)
		}
		entries = append(entries, segEntries...)
	}
	return entries, nil
}

// readSegment reads all entries from a single segment file.
func readSegment(path string) ([]*Entry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var entries []*Entry
	off := 0
	for off < len(data) {
		// Need at least 8 bytes to read CRC + Length.
		if off+8 > len(data) {
			log.Printf("WAL: truncated entry at offset %d in %s, skipping rest", off, filepath.Base(path))
			break
		}

		length := binary.BigEndian.Uint32(data[off+4 : off+8])
		if length < HeaderSize {
			log.Printf("WAL: invalid entry length %d at offset %d in %s, skipping rest", length, off, filepath.Base(path))
			break
		}

		end := off + int(length)
		if end > len(data) {
			log.Printf("WAL: truncated entry at offset %d in %s, skipping rest", off, filepath.Base(path))
			break
		}

		entry, err := DeserializeEntry(data[off:end])
		if err != nil {
			log.Printf("WAL: skipping corrupt entry at offset %d in %s: %v", off, filepath.Base(path), err)
			off = end
			continue
		}

		entries = append(entries, entry)
		off = end
	}
	return entries, nil
}
