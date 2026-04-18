package snapshot

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

const defaultMaxSectionEntries = 15

type DiffOptions struct {
	MaxItems int
}

func Diff(before, after *Bundle) (string, error) {
	return DiffWithOptions(before, after, DiffOptions{})
}

func DiffWithOptions(before, after *Bundle, opts DiffOptions) (string, error) {
	maxItems := opts.MaxItems
	if maxItems <= 0 {
		maxItems = defaultMaxSectionEntries
	}

	beforeMap, err := recordHashMap(before)
	if err != nil {
		return "", fmt.Errorf("index before bundle: %w", err)
	}
	afterMap, err := recordHashMap(after)
	if err != nil {
		return "", fmt.Errorf("index after bundle: %w", err)
	}

	added := make([]string, 0)
	removed := make([]string, 0)
	changed := make([]string, 0)

	for key, a := range afterMap {
		b, exists := beforeMap[key]
		if !exists {
			added = append(added, key)
			continue
		}
		if a != b {
			changed = append(changed, key)
		}
	}
	for key := range beforeMap {
		if _, exists := afterMap[key]; !exists {
			removed = append(removed, key)
		}
	}

	sort.Strings(added)
	sort.Strings(removed)
	sort.Strings(changed)

	var sb strings.Builder
	sb.WriteString("Snapshot Diff Report\n")
	sb.WriteString("--------------------\n")
	sb.WriteString(fmt.Sprintf("Before records: %d\n", len(before.Records)))
	sb.WriteString(fmt.Sprintf("After records:  %d\n", len(after.Records)))
	sb.WriteString(fmt.Sprintf("Added:          %d\n", len(added)))
	sb.WriteString(fmt.Sprintf("Removed:        %d\n", len(removed)))
	sb.WriteString(fmt.Sprintf("Changed:        %d\n", len(changed)))
	sb.WriteString(fmt.Sprintf("Net delta:      %+d\n\n", len(after.Records)-len(before.Records)))

	writeSection(&sb, "ADDED RESOURCES", added, maxItems)
	writeSection(&sb, "REMOVED RESOURCES", removed, maxItems)
	writeSection(&sb, "CHANGED RESOURCES", changed, maxItems)
	return sb.String(), nil
}

func sectionIcon(title string) string {
	switch {
	case strings.Contains(title, "POD"):
		return "🐳 "
	case strings.Contains(title, "NODE"):
		return "🖥️  "
	case strings.Contains(title, "WORKLOAD"):
		return "⚙️  "
	case strings.Contains(title, "STORAGE"):
		return "💾 "
	case strings.Contains(title, "WARNING"):
		return "⚠️  "
	default:
		return "📋 "
	}
}

func writeSection(sb *strings.Builder, title string, entries []string, maxItems int) {
	sb.WriteString(clr(ansiBold, sectionIcon(title)+title))
	sb.WriteByte('\n')
	sb.WriteString(clr(ansiDim, "─────────────────────────────────"))
	sb.WriteByte('\n')
	if len(entries) == 0 {
		sb.WriteString("  ✓ none\n\n")
		return
	}
	limit := minInt(len(entries), maxItems)
	for i, e := range entries[:limit] {
		sb.WriteString(fmt.Sprintf("  %2d. %s\n", i+1, humanizeRecordKey(e)))
	}
	if len(entries) > limit {
		sb.WriteString(fmt.Sprintf("  ... and %d more\n", len(entries)-limit))
	}
	sb.WriteByte('\n')
}

func humanizeRecordKey(key string) string {
	parts := strings.Split(key, "/")
	if len(parts) != 3 {
		return key
	}
	resource := parts[0]
	namespace := parts[1]
	name := parts[2]
	if namespace == "_cluster" {
		return fmt.Sprintf("%s %s", resource, name)
	}
	return fmt.Sprintf("%s %s/%s", resource, namespace, name)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func recordHashMap(bundle *Bundle) (map[string]string, error) {
	out := make(map[string]string, len(bundle.Records))
	for _, r := range bundle.Records {
		key := recordKey(r)
		hash, err := recordHash(r)
		if err != nil {
			return nil, err
		}
		out[key] = hash
	}
	return out, nil
}

func recordKey(r Record) string {
	if r.Namespace == "" {
		return fmt.Sprintf("%s/%s/%s", r.Resource, "_cluster", r.Name)
	}
	return fmt.Sprintf("%s/%s/%s", r.Resource, r.Namespace, r.Name)
}

func recordHash(r Record) (string, error) {
	bytes, err := MarshalDeterministic(r.Object)
	if err != nil {
		return "", fmt.Errorf("marshal record %s: %w", recordKey(r), err)
	}
	sum := sha256.Sum256(bytes)
	return hex.EncodeToString(sum[:]), nil
}
