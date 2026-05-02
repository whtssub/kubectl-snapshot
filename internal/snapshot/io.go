package snapshot

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// gzip magic bytes: 0x1f 0x8b
var gzipMagic = []byte{0x1f, 0x8b}

// WriteOptions controls how a bundle is serialised to disk.
type WriteOptions struct {
	// Compress selects the compression algorithm. Supported values: "", "gzip".
	// An empty string writes plain JSON.
	Compress string
}

// WriteBundle writes bundle to path as plain JSON.
// Use WriteBundleWithOptions for compression.
func WriteBundle(path string, bundle *Bundle) error {
	return WriteBundleWithOptions(path, bundle, WriteOptions{})
}

// WriteBundleWithOptions writes bundle to path, optionally compressed.
// When opts.Compress is "gzip" and path does not already end in ".gz",
// ".gz" is appended automatically.
func WriteBundleWithOptions(path string, bundle *Bundle, opts WriteOptions) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}

	data, err := MarshalDeterministic(bundle)
	if err != nil {
		return fmt.Errorf("marshal bundle: %w", err)
	}

	switch strings.ToLower(opts.Compress) {
	case "gzip", "gz":
		var buf bytes.Buffer
		gz := gzip.NewWriter(&buf)
		if _, err := gz.Write(data); err != nil {
			return fmt.Errorf("gzip compress: %w", err)
		}
		if err := gz.Close(); err != nil {
			return fmt.Errorf("gzip close: %w", err)
		}
		data = buf.Bytes()
	case "":
		// plain JSON — no-op
	default:
		return fmt.Errorf("unsupported compression format %q (supported: gzip)", opts.Compress)
	}

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write bundle: %w", err)
	}
	return nil
}

// ReadBundle reads a bundle from path. It transparently decompresses gzip
// content detected by magic bytes, so callers need not know the format.
func ReadBundle(path string) (*Bundle, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read bundle: %w", err)
	}

	if isGzip(data) {
		data, err = gunzip(data)
		if err != nil {
			return nil, fmt.Errorf("decompress bundle: %w", err)
		}
	}

	var bundle Bundle
	if err := json.Unmarshal(data, &bundle); err != nil {
		return nil, fmt.Errorf("decode bundle: %w", err)
	}
	return &bundle, nil
}

// StatBundle returns the byte size of the file at path. Used when recording
// an entry in the history index.
func StatBundle(path string) (int64, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return fi.Size(), nil
}

func isGzip(data []byte) bool {
	return len(data) >= 2 && bytes.Equal(data[:2], gzipMagic)
}

func gunzip(data []byte) ([]byte, error) {
	r, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(r)
}
