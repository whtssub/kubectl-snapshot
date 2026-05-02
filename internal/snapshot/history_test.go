package snapshot

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadIndex_MissingFile_ReturnsEmptyIndex(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.json")
	idx, err := LoadIndex(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(idx.Entries) != 0 {
		t.Errorf("expected empty entries, got %d", len(idx.Entries))
	}
}

func TestSaveAndLoadIndex_RoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.json")
	entry := HistoryEntry{
		Path:         "/tmp/snap.json",
		CapturedAt:   time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC),
		Cluster:      "kind-dev",
		TotalRecords: 42,
	}
	idx := &HistoryIndex{Entries: []HistoryEntry{entry}}
	if err := SaveIndex(path, idx); err != nil {
		t.Fatalf("save: %v", err)
	}
	loaded, err := LoadIndex(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(loaded.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(loaded.Entries))
	}
	if loaded.Entries[0].Path != "/tmp/snap.json" {
		t.Errorf("path mismatch: %q", loaded.Entries[0].Path)
	}
	if loaded.Entries[0].TotalRecords != 42 {
		t.Errorf("totalRecords mismatch: %d", loaded.Entries[0].TotalRecords)
	}
}

func TestAddToIndex_AppendsEntry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.json")
	e1 := HistoryEntry{Path: "/tmp/a.json", CapturedAt: time.Now(), TotalRecords: 10}
	e2 := HistoryEntry{Path: "/tmp/b.json", CapturedAt: time.Now(), TotalRecords: 20}
	if err := AddToIndex(path, e1); err != nil {
		t.Fatal(err)
	}
	if err := AddToIndex(path, e2); err != nil {
		t.Fatal(err)
	}
	idx, err := LoadIndex(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(idx.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(idx.Entries))
	}
}

func TestAddToIndex_DeduplicatesByPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.json")
	first := HistoryEntry{Path: "/tmp/snap.json", CapturedAt: time.Now(), TotalRecords: 5}
	second := HistoryEntry{Path: "/tmp/snap.json", CapturedAt: time.Now(), TotalRecords: 99}
	if err := AddToIndex(path, first); err != nil {
		t.Fatal(err)
	}
	if err := AddToIndex(path, second); err != nil {
		t.Fatal(err)
	}
	idx, err := LoadIndex(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(idx.Entries) != 1 {
		t.Fatalf("expected 1 entry after dedup, got %d", len(idx.Entries))
	}
	if idx.Entries[0].TotalRecords != 99 {
		t.Errorf("expected updated totalRecords=99, got %d", idx.Entries[0].TotalRecords)
	}
}

func TestSaveIndex_CreatesParentDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "subdir")
	path := filepath.Join(dir, "history.json")
	idx := &HistoryIndex{Entries: []HistoryEntry{}}
	if err := SaveIndex(path, idx); err != nil {
		t.Fatalf("expected SaveIndex to create parent dirs: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("index file not created: %v", err)
	}
}
