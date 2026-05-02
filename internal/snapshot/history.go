package snapshot

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// HistoryEntry records metadata about a captured snapshot in the local index.
type HistoryEntry struct {
	Path         string    `json:"path"`
	CapturedAt   time.Time `json:"capturedAt"`
	Cluster      string    `json:"cluster,omitempty"`
	TotalRecords int       `json:"totalRecords"`
	SizeBytes    int64     `json:"sizeBytes,omitempty"`
}

// HistoryIndex is the on-disk structure of the snapshot history file.
type HistoryIndex struct {
	Entries []HistoryEntry `json:"entries"`
}

// DefaultIndexPath returns the canonical path to the local history index.
func DefaultIndexPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".kubectl-snapshot", "history.json"), nil
}

// LoadIndex reads the history index from path. Returns an empty index if the
// file does not exist yet (i.e. first capture).
func LoadIndex(path string) (*HistoryIndex, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &HistoryIndex{Entries: []HistoryEntry{}}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read history index: %w", err)
	}
	var idx HistoryIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, fmt.Errorf("decode history index: %w", err)
	}
	if idx.Entries == nil {
		idx.Entries = []HistoryEntry{}
	}
	return &idx, nil
}

// SaveIndex writes the history index to path, creating parent directories as
// needed. The file is written atomically via a temp file + rename.
func SaveIndex(path string, idx *HistoryIndex) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create index directory: %w", err)
	}
	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal history index: %w", err)
	}
	// Write to a temp file in the same directory, then rename for atomicity.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write history index: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("finalize history index: %w", err)
	}
	return nil
}

// AddToIndex appends entry to the history index at indexPath and saves it.
// If an entry with the same path already exists it is replaced (re-capture).
func AddToIndex(indexPath string, entry HistoryEntry) error {
	idx, err := LoadIndex(indexPath)
	if err != nil {
		return err
	}
	// Deduplicate by path: remove any prior entry for this file.
	out := idx.Entries[:0]
	for _, e := range idx.Entries {
		if e.Path != entry.Path {
			out = append(out, e)
		}
	}
	idx.Entries = append(out, entry)
	return SaveIndex(indexPath, idx)
}
