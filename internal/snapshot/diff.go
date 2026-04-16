package snapshot

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

func Diff(before, after *Bundle) (string, error) {
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
	sb.WriteString("====================\n")
	sb.WriteString(fmt.Sprintf("Added:   %d\n", len(added)))
	sb.WriteString(fmt.Sprintf("Removed: %d\n", len(removed)))
	sb.WriteString(fmt.Sprintf("Changed: %d\n\n", len(changed)))

	writeSection(&sb, "ADDED", added)
	writeSection(&sb, "REMOVED", removed)
	writeSection(&sb, "CHANGED", changed)
	return sb.String(), nil
}

func writeSection(sb *strings.Builder, title string, entries []string) {
	sb.WriteString(title)
	sb.WriteString(":\n")
	if len(entries) == 0 {
		sb.WriteString("  (none)\n\n")
		return
	}
	for _, e := range entries {
		sb.WriteString("  - ")
		sb.WriteString(e)
		sb.WriteByte('\n')
	}
	sb.WriteByte('\n')
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
