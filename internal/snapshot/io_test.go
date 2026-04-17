package snapshot

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func sampleBundle() *Bundle {
	return &Bundle{
		Metadata: Metadata{
			ToolVersion: "0.2.0",
			CapturedAt:  time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
			ClusterHint: "test-cluster",
			CapturedResources: []string{"v1/pods", "apps/v1/deployments"},
			SkippedResources:  []string{"autoscaling.k8s.io/v1/verticalpodautoscalers"},
		},
		Records: []Record{
			{
				Group:     "",
				Version:   "v1",
				Resource:  "pods",
				Namespace: "default",
				Name:      "test-pod",
				Object: map[string]any{
					"metadata": map[string]any{
						"name":      "test-pod",
						"namespace": "default",
					},
					"status": map[string]any{
						"phase": "Running",
					},
				},
			},
		},
	}
}

func TestWriteAndReadBundle_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "snap.json")

	original := sampleBundle()
	if err := WriteBundle(path, original); err != nil {
		t.Fatalf("WriteBundle failed: %v", err)
	}

	restored, err := ReadBundle(path)
	if err != nil {
		t.Fatalf("ReadBundle failed: %v", err)
	}

	if restored.Metadata.ToolVersion != original.Metadata.ToolVersion {
		t.Errorf("ToolVersion mismatch: got %q, want %q", restored.Metadata.ToolVersion, original.Metadata.ToolVersion)
	}
	if restored.Metadata.ClusterHint != original.Metadata.ClusterHint {
		t.Errorf("ClusterHint mismatch: got %q, want %q", restored.Metadata.ClusterHint, original.Metadata.ClusterHint)
	}
	if len(restored.Records) != len(original.Records) {
		t.Errorf("record count mismatch: got %d, want %d", len(restored.Records), len(original.Records))
	}
	if restored.Records[0].Name != "test-pod" {
		t.Errorf("record name mismatch: got %q", restored.Records[0].Name)
	}
}

func TestWriteBundle_CreatesParentDirectories(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "deep", "snap.json")

	if err := WriteBundle(path, sampleBundle()); err != nil {
		t.Fatalf("WriteBundle should create parent dirs: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected file to exist at %s: %v", path, err)
	}
}

func TestReadBundle_NonExistentFile_ReturnsError(t *testing.T) {
	_, err := ReadBundle("/does/not/exist/snap.json")
	if err == nil {
		t.Error("expected error for non-existent file")
	}
}

func TestReadBundle_MalformedJSON_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte("{not valid json"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := ReadBundle(path)
	if err == nil {
		t.Error("expected error for malformed JSON")
	}
}

func TestWriteBundle_ProducesValidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "snap.json")

	if err := WriteBundle(path, sampleBundle()); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	// Re-reading should succeed (validates it's valid JSON)
	b, err := ReadBundle(path)
	if err != nil {
		t.Fatalf("output file contains invalid JSON: %v\n%s", err, data)
	}
	if b == nil {
		t.Error("decoded bundle should not be nil")
	}
}

func TestWriteAndReadBundle_PreservesCapturedResources(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "snap.json")

	original := sampleBundle()
	if err := WriteBundle(path, original); err != nil {
		t.Fatal(err)
	}
	restored, err := ReadBundle(path)
	if err != nil {
		t.Fatal(err)
	}

	if len(restored.Metadata.CapturedResources) != len(original.Metadata.CapturedResources) {
		t.Errorf("CapturedResources mismatch: got %v, want %v",
			restored.Metadata.CapturedResources, original.Metadata.CapturedResources)
	}
	if len(restored.Metadata.SkippedResources) != len(original.Metadata.SkippedResources) {
		t.Errorf("SkippedResources mismatch: got %v, want %v",
			restored.Metadata.SkippedResources, original.Metadata.SkippedResources)
	}
}

// --- Compression round-trips ---

func TestWriteAndReadBundle_GzipRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "snap.json.gz")

	original := sampleBundle()
	if err := WriteBundleWithOptions(path, original, WriteOptions{Compress: "gzip"}); err != nil {
		t.Fatalf("WriteBundleWithOptions(gzip) failed: %v", err)
	}

	restored, err := ReadBundle(path)
	if err != nil {
		t.Fatalf("ReadBundle of gzip file failed: %v", err)
	}
	if restored.Metadata.ToolVersion != original.Metadata.ToolVersion {
		t.Errorf("ToolVersion mismatch after gzip round-trip")
	}
	if len(restored.Records) != len(original.Records) {
		t.Errorf("record count mismatch after gzip round-trip")
	}
}

func TestWriteAndReadBundle_GzipAlias(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "snap.gz")

	if err := WriteBundleWithOptions(path, sampleBundle(), WriteOptions{Compress: "gz"}); err != nil {
		t.Fatalf("compress=gz alias failed: %v", err)
	}
	if _, err := ReadBundle(path); err != nil {
		t.Fatalf("ReadBundle of gz alias failed: %v", err)
	}
}

func TestWriteAndReadBundle_GzipSmallerThanPlain(t *testing.T) {
	dir := t.TempDir()
	plain := filepath.Join(dir, "snap.json")
	compressed := filepath.Join(dir, "snap.json.gz")

	// Build a bundle with enough data to compress meaningfully
	records := make([]Record, 50)
	for i := range records {
		records[i] = makeRecord("pods", "default", fmt.Sprintf("pod-%d", i), map[string]any{
			"status": map[string]any{"phase": "Running"},
			"spec":   map[string]any{"nodeName": "node1", "restartPolicy": "Always"},
		})
	}
	b := makeBundle(records)

	if err := WriteBundle(plain, b); err != nil {
		t.Fatal(err)
	}
	if err := WriteBundleWithOptions(compressed, b, WriteOptions{Compress: "gzip"}); err != nil {
		t.Fatal(err)
	}

	plainStat, _ := os.Stat(plain)
	compStat, _ := os.Stat(compressed)
	if compStat.Size() >= plainStat.Size() {
		t.Errorf("expected gzip (%d bytes) < plain (%d bytes)", compStat.Size(), plainStat.Size())
	}
}

func TestReadBundle_AutoDetectsGzip(t *testing.T) {
	dir := t.TempDir()
	// Write with a plain .json extension but gzip content — ReadBundle should still decompress
	path := filepath.Join(dir, "snap.json")
	if err := WriteBundleWithOptions(path, sampleBundle(), WriteOptions{Compress: "gzip"}); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadBundle(path); err != nil {
		t.Fatalf("ReadBundle should auto-detect gzip by magic bytes, got: %v", err)
	}
}

func TestWriteBundleWithOptions_UnknownCompressor_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "snap.json")
	err := WriteBundleWithOptions(path, sampleBundle(), WriteOptions{Compress: "zstd"})
	if err == nil {
		t.Error("expected error for unsupported compression format")
	}
}

func TestIsGzip_MagicBytes(t *testing.T) {
	if !isGzip([]byte{0x1f, 0x8b, 0x00}) {
		t.Error("expected gzip magic bytes to be detected")
	}
	if isGzip([]byte{0x00, 0x00}) {
		t.Error("non-gzip bytes should not be detected as gzip")
	}
	if isGzip([]byte{0x1f}) {
		t.Error("single byte should not be detected as gzip")
	}
	if isGzip([]byte{}) {
		t.Error("empty slice should not be detected as gzip")
	}
}

func TestMarshalDeterministic_StableOutput(t *testing.T) {
	obj := map[string]any{
		"z": "last",
		"a": "first",
		"m": "middle",
	}
	b1, err := MarshalDeterministic(obj)
	if err != nil {
		t.Fatal(err)
	}
	b2, err := MarshalDeterministic(obj)
	if err != nil {
		t.Fatal(err)
	}
	if string(b1) != string(b2) {
		t.Error("MarshalDeterministic should produce identical output on repeated calls")
	}
}
