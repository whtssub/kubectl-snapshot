package snapshot

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

func WriteBundle(path string, bundle *Bundle) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}

	bytes, err := MarshalDeterministic(bundle)
	if err != nil {
		return fmt.Errorf("marshal bundle: %w", err)
	}
	if err := os.WriteFile(path, bytes, 0o644); err != nil {
		return fmt.Errorf("write bundle: %w", err)
	}
	return nil
}

func ReadBundle(path string) (*Bundle, error) {
	bytes, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read bundle: %w", err)
	}

	var bundle Bundle
	if err := json.Unmarshal(bytes, &bundle); err != nil {
		return nil, fmt.Errorf("decode bundle: %w", err)
	}
	return &bundle, nil
}
