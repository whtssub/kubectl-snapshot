package snapshot

import (
	"strings"
	"testing"
	"time"
)

func makeBundle(records []Record) *Bundle {
	return &Bundle{
		Metadata: Metadata{
			ToolVersion: "0.2.0",
			CapturedAt:  time.Now().UTC(),
		},
		Records: records,
	}
}

func makeRecord(resource, namespace, name string, obj map[string]any) Record {
	return Record{
		Group:     "",
		Version:   "v1",
		Resource:  resource,
		Namespace: namespace,
		Name:      name,
		Object:    obj,
	}
}

// --- Diff correctness ---

func TestDiff_IdenticalBundles_NoChanges(t *testing.T) {
	r := makeRecord("pods", "default", "web", map[string]any{"status": "Running"})
	before := makeBundle([]Record{r})
	after := makeBundle([]Record{r})

	out, err := Diff(before, after)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "Added:          0") {
		t.Error("expected Added: 0")
	}
	if !strings.Contains(out, "Removed:        0") {
		t.Error("expected Removed: 0")
	}
	if !strings.Contains(out, "Changed:        0") {
		t.Error("expected Changed: 0")
	}
}

func TestDiff_AddedRecord(t *testing.T) {
	r := makeRecord("pods", "default", "new-pod", map[string]any{"phase": "Running"})
	before := makeBundle([]Record{})
	after := makeBundle([]Record{r})

	out, err := Diff(before, after)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "Added:          1") {
		t.Error("expected Added: 1")
	}
	if !strings.Contains(out, "new-pod") {
		t.Error("expected new-pod in output")
	}
}

func TestDiff_RemovedRecord(t *testing.T) {
	r := makeRecord("pods", "default", "gone-pod", map[string]any{"phase": "Running"})
	before := makeBundle([]Record{r})
	after := makeBundle([]Record{})

	out, err := Diff(before, after)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "Removed:        1") {
		t.Error("expected Removed: 1")
	}
	if !strings.Contains(out, "gone-pod") {
		t.Error("expected gone-pod in output")
	}
}

func TestDiff_ChangedRecord(t *testing.T) {
	before := makeBundle([]Record{
		makeRecord("pods", "default", "p1", map[string]any{"phase": "Pending"}),
	})
	after := makeBundle([]Record{
		makeRecord("pods", "default", "p1", map[string]any{"phase": "Running"}),
	})

	out, err := Diff(before, after)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "Changed:        1") {
		t.Error("expected Changed: 1")
	}
}

func TestDiff_MultipleChanges(t *testing.T) {
	before := makeBundle([]Record{
		makeRecord("pods", "default", "p1", map[string]any{"phase": "Running"}),
		makeRecord("pods", "default", "p2", map[string]any{"phase": "Running"}),
	})
	after := makeBundle([]Record{
		makeRecord("pods", "default", "p1", map[string]any{"phase": "Failed"}), // changed
		makeRecord("pods", "default", "p3", map[string]any{"phase": "Running"}), // added
		// p2 removed
	})

	out, err := Diff(before, after)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "Added:          1") {
		t.Errorf("expected Added: 1, got:\n%s", out)
	}
	if !strings.Contains(out, "Removed:        1") {
		t.Errorf("expected Removed: 1, got:\n%s", out)
	}
	if !strings.Contains(out, "Changed:        1") {
		t.Errorf("expected Changed: 1, got:\n%s", out)
	}
}

func TestDiff_EmptyBundles(t *testing.T) {
	before := makeBundle([]Record{})
	after := makeBundle([]Record{})

	out, err := Diff(before, after)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "Added:          0") {
		t.Error("expected Added: 0 for empty bundles")
	}
}

func TestDiff_MaxItemsLimitsOutput(t *testing.T) {
	records := make([]Record, 20)
	for i := range records {
		records[i] = makeRecord("pods", "default", strings.Repeat("p", i+1), map[string]any{"i": i})
	}
	before := makeBundle([]Record{})
	after := makeBundle(records)

	out, err := DiffWithOptions(before, after, DiffOptions{MaxItems: 5})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "... 15 more") {
		t.Errorf("expected '... 15 more' truncation, got:\n%s", out)
	}
}

// --- recordKey ---

func TestRecordKey_Namespaced(t *testing.T) {
	r := makeRecord("pods", "default", "my-pod", nil)
	key := recordKey(r)
	if key != "pods/default/my-pod" {
		t.Errorf("expected pods/default/my-pod, got %q", key)
	}
}

func TestRecordKey_ClusterScoped(t *testing.T) {
	r := makeRecord("nodes", "", "node1", nil)
	key := recordKey(r)
	if key != "nodes/_cluster/node1" {
		t.Errorf("expected nodes/_cluster/node1, got %q", key)
	}
}

// --- recordHash stability ---

func TestRecordHash_SameObjectSameHash(t *testing.T) {
	r := makeRecord("pods", "default", "p1", map[string]any{"phase": "Running", "ready": true})
	h1, err := recordHash(r)
	if err != nil {
		t.Fatal(err)
	}
	h2, err := recordHash(r)
	if err != nil {
		t.Fatal(err)
	}
	if h1 != h2 {
		t.Error("identical records should produce identical hashes")
	}
}

func TestRecordHash_DifferentObjectDifferentHash(t *testing.T) {
	r1 := makeRecord("pods", "default", "p1", map[string]any{"phase": "Running"})
	r2 := makeRecord("pods", "default", "p1", map[string]any{"phase": "Failed"})
	h1, _ := recordHash(r1)
	h2, _ := recordHash(r2)
	if h1 == h2 {
		t.Error("different objects should produce different hashes")
	}
}

// --- humanizeRecordKey ---

func TestHumanizeRecordKey_Namespaced(t *testing.T) {
	key := "pods/default/my-pod"
	got := humanizeRecordKey(key)
	if got != "pods default/my-pod" {
		t.Errorf("expected 'pods default/my-pod', got %q", got)
	}
}

func TestHumanizeRecordKey_ClusterScoped(t *testing.T) {
	key := "nodes/_cluster/node1"
	got := humanizeRecordKey(key)
	if got != "nodes node1" {
		t.Errorf("expected 'nodes node1', got %q", got)
	}
}

func TestHumanizeRecordKey_MalformedKey_Passthrough(t *testing.T) {
	key := "just-a-string"
	got := humanizeRecordKey(key)
	if got != key {
		t.Errorf("malformed key should be returned as-is, got %q", got)
	}
}

// --- writeSection ---

func TestWriteSection_Empty(t *testing.T) {
	var sb strings.Builder
	writeSection(&sb, "TEST", []string{}, 10)
	out := sb.String()
	if !strings.Contains(out, "## TEST") {
		t.Error("expected section header")
	}
	if !strings.Contains(out, "- none") {
		t.Error("expected '- none' for empty section")
	}
}

func TestWriteSection_WithEntries(t *testing.T) {
	var sb strings.Builder
	writeSection(&sb, "TEST", []string{"item1", "item2"}, 10)
	out := sb.String()
	if !strings.Contains(out, "item1") || !strings.Contains(out, "item2") {
		t.Error("expected both items in output")
	}
}

func TestWriteSection_TruncatesAtMaxItems(t *testing.T) {
	items := []string{"a", "b", "c", "d", "e"}
	var sb strings.Builder
	writeSection(&sb, "TEST", items, 3)
	out := sb.String()
	if !strings.Contains(out, "... 2 more") {
		t.Errorf("expected truncation message, got:\n%s", out)
	}
}
