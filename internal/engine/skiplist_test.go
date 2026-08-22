package engine

import (
	"fmt"
	"testing"
)

func TestSkipListInsertAndSearch(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		value    string
		wantFind bool
	}{
		{"single key", "hello", "world", true},
		{"numeric key", "123", "456", true},
		{"empty value", "key", "", true},
		{"unicode key", "日本語", "value", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sl := NewSkipList(16, 0.5)
			sl.Insert(tt.key, []byte(tt.value))

			got, found := sl.Search(tt.key)
			if found != tt.wantFind {
				t.Errorf("Search(%q) found = %v, want %v", tt.key, found, tt.wantFind)
			}
			if string(got) != tt.value {
				t.Errorf("Search(%q) = %q, want %q", tt.key, got, tt.value)
			}
		})
	}
}

func TestSkipListSearchMiss(t *testing.T) {
	sl := NewSkipList(16, 0.5)
	sl.Insert("exists", []byte("yes"))

	got, found := sl.Search("nonexistent")
	if found {
		t.Errorf("Search(nonexistent) found = true, want false")
	}
	if got != nil {
		t.Errorf("Search(nonexistent) = %v, want nil", got)
	}
}

func TestSkipListEmptySearch(t *testing.T) {
	sl := NewSkipList(16, 0.5)
	_, found := sl.Search("anything")
	if found {
		t.Error("Search on empty list should return false")
	}
}

func TestSkipListUpdate(t *testing.T) {
	sl := NewSkipList(16, 0.5)
	sl.Insert("key", []byte("v1"))
	sl.Insert("key", []byte("v2"))

	got, _ := sl.Search("key")
	if string(got) != "v2" {
		t.Errorf("After update, got %q, want %q", got, "v2")
	}
	if sl.Len() != 1 {
		t.Errorf("After update, Len() = %d, want 1", sl.Len())
	}
}

func TestSkipListDelete(t *testing.T) {
	sl := NewSkipList(16, 0.5)
	sl.Insert("a", []byte("1"))
	sl.Insert("b", []byte("2"))
	sl.Insert("c", []byte("3"))

	if !sl.Delete("b") {
		t.Error("Delete(b) returned false, want true")
	}
	if sl.Len() != 2 {
		t.Errorf("After delete, Len() = %d, want 2", sl.Len())
	}
	if _, found := sl.Search("b"); found {
		t.Error("Search(b) after delete should return false")
	}
	// Other keys should still exist.
	if _, found := sl.Search("a"); !found {
		t.Error("Search(a) should still find the key")
	}
	if _, found := sl.Search("c"); !found {
		t.Error("Search(c) should still find the key")
	}
}

func TestSkipListDeleteNonExistent(t *testing.T) {
	sl := NewSkipList(16, 0.5)
	sl.Insert("a", []byte("1"))
	if sl.Delete("z") {
		t.Error("Delete(z) on non-existent key should return false")
	}
}

func TestSkipListDeleteEmpty(t *testing.T) {
	sl := NewSkipList(16, 0.5)
	if sl.Delete("x") {
		t.Error("Delete on empty list should return false")
	}
}

func TestSkipListRange(t *testing.T) {
	sl := NewSkipList(16, 0.5)
	for _, k := range []string{"a", "b", "c", "d", "e", "f"} {
		sl.Insert(k, []byte(k+"_val"))
	}

	tests := []struct {
		name     string
		start    string
		end      string
		wantKeys []string
	}{
		{"full range", "a", "f", []string{"a", "b", "c", "d", "e", "f"}},
		{"partial range", "b", "d", []string{"b", "c", "d"}},
		{"single element", "c", "c", []string{"c"}},
		{"empty range", "g", "z", nil},
		{"range beyond", "a", "z", []string{"a", "b", "c", "d", "e", "f"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sl.Range(tt.start, tt.end)
			if len(result) != len(tt.wantKeys) {
				t.Errorf("Range(%q, %q) returned %d items, want %d", tt.start, tt.end, len(result), len(tt.wantKeys))
				return
			}
			for i, kv := range result {
				if kv.Key != tt.wantKeys[i] {
					t.Errorf("Range(%q, %q)[%d].Key = %q, want %q", tt.start, tt.end, i, kv.Key, tt.wantKeys[i])
				}
			}
		})
	}
}

func TestSkipListManyKeys(t *testing.T) {
	sl := NewSkipList(16, 0.5)
	n := 10000
	for i := 0; i < n; i++ {
		key := fmt.Sprintf("key-%05d", i)
		sl.Insert(key, []byte(fmt.Sprintf("val-%d", i)))
	}

	if sl.Len() != n {
		t.Errorf("Len() = %d, want %d", sl.Len(), n)
	}

	// Spot-check some keys.
	for _, i := range []int{0, 100, 999, 5000, 9999} {
		key := fmt.Sprintf("key-%05d", i)
		val, found := sl.Search(key)
		if !found {
			t.Errorf("Search(%q) not found", key)
		}
		want := fmt.Sprintf("val-%d", i)
		if string(val) != want {
			t.Errorf("Search(%q) = %q, want %q", key, val, want)
		}
	}
}

func TestSkipListKeys(t *testing.T) {
	sl := NewSkipList(16, 0.5)
	sl.Insert("c", []byte("3"))
	sl.Insert("a", []byte("1"))
	sl.Insert("b", []byte("2"))

	keys := sl.Keys()
	want := []string{"a", "b", "c"}
	if len(keys) != len(want) {
		t.Fatalf("Keys() returned %d keys, want %d", len(keys), len(want))
	}
	for i, k := range keys {
		if k != want[i] {
			t.Errorf("Keys()[%d] = %q, want %q", i, k, want[i])
		}
	}
}

// --- Benchmarks ---

func BenchmarkSkipListInsert(b *testing.B) {
	sl := NewSkipList(16, 0.5)
	keys := make([]string, b.N)
	for i := range keys {
		keys[i] = fmt.Sprintf("key-%010d", i)
	}
	val := []byte("benchmark-value")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sl.Insert(keys[i], val)
	}
}

func BenchmarkSkipListSearch(b *testing.B) {
	sl := NewSkipList(16, 0.5)
	n := 100000
	for i := 0; i < n; i++ {
		sl.Insert(fmt.Sprintf("key-%010d", i), []byte("val"))
	}

	keys := make([]string, b.N)
	for i := range keys {
		keys[i] = fmt.Sprintf("key-%010d", i%n)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sl.Search(keys[i])
	}
}

func BenchmarkMapSearch(b *testing.B) {
	m := make(map[string][]byte)
	n := 100000
	for i := 0; i < n; i++ {
		m[fmt.Sprintf("key-%010d", i)] = []byte("val")
	}

	keys := make([]string, b.N)
	for i := range keys {
		keys[i] = fmt.Sprintf("key-%010d", i%n)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m[keys[i]]
	}
}
