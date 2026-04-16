package snapshot

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

type analysis struct {
	podIssues          []string
	nodeIssues         []string
	warningEvents      []string
	totalRestarts      int
	unknownEventLevels int
	resourceCounts     map[string]int
}

func Analyze(bundle *Bundle) (string, error) {
	return AnalyzeWithOptions(bundle, AnalyzeOptions{})
}

type AnalyzeOptions struct {
	MaxItems          int
	MinSeverity       string
	HideResourceMix   bool
	HideWarningEvents bool
}

func AnalyzeWithOptions(bundle *Bundle, opts AnalyzeOptions) (string, error) {
	maxItems := opts.MaxItems
	if maxItems <= 0 {
		maxItems = defaultMaxSectionEntries
	}

	a := &analysis{
		podIssues:     make([]string, 0),
		nodeIssues:    make([]string, 0),
		warningEvents: make([]string, 0),
		resourceCounts: make(map[string]int),
	}

	for _, r := range bundle.Records {
		a.resourceCounts[r.Resource]++
		obj, ok := r.Object.(map[string]any)
		if !ok {
			continue
		}
		switch r.Resource {
		case "pods":
			a.inspectPod(r, obj)
		case "nodes":
			a.inspectNode(r, obj)
		case "events":
			a.inspectEvent(r, obj)
		}
	}

	sort.Strings(a.podIssues)
	sort.Strings(a.nodeIssues)
	sort.Strings(a.warningEvents)

	var sb strings.Builder
	sb.WriteString("Snapshot Incident Analysis\n")
	sb.WriteString("-------------------------\n")
	sb.WriteString(fmt.Sprintf("Captured at:        %s\n", bundle.Metadata.CapturedAt.Format("2006-01-02 15:04:05 MST")))
	sb.WriteString(fmt.Sprintf("Cluster context:    %s\n", defaultIfEmpty(bundle.Metadata.ClusterHint, "(unknown)")))
	sb.WriteString(fmt.Sprintf("Total records:      %d\n", len(bundle.Records)))
	sb.WriteString(fmt.Sprintf("Total restarts:     %d\n", a.totalRestarts))
	sb.WriteString(fmt.Sprintf("Warning events:     %d\n", len(a.warningEvents)))
	sb.WriteString(fmt.Sprintf("Non-normal events:  %d\n\n", a.unknownEventLevels))

	score, severity := computeIncidentScoreAndSeverity(len(a.podIssues), len(a.nodeIssues), len(a.warningEvents), a.totalRestarts)
	if belowSeverityThreshold(severity, opts.MinSeverity) {
		sb.WriteString("Result: below configured severity threshold\n")
		sb.WriteString(fmt.Sprintf("- current severity: %s\n", severity))
		sb.WriteString(fmt.Sprintf("- threshold: %s\n", strings.ToUpper(strings.TrimSpace(opts.MinSeverity))))
		return sb.String(), nil
	}

	writeIncidentScore(&sb, score, severity)
	if !opts.HideResourceMix {
		writeResourceMix(&sb, a.resourceCounts)
	}
	writeSection(&sb, "POD ISSUES", a.podIssues, maxItems)
	writeSection(&sb, "NODE ISSUES", a.nodeIssues, maxItems)
	if !opts.HideWarningEvents {
		writeSection(&sb, "WARNING EVENTS", a.warningEvents, maxItems)
	}
	return sb.String(), nil
}

func computeIncidentScoreAndSeverity(podIssues, nodeIssues, warnings, restarts int) (int, string) {
	score := podIssues*3 + nodeIssues*4 + warnings + restarts
	severity := "LOW"
	switch {
	case score >= 40:
		severity = "HIGH"
	case score >= 15:
		severity = "MEDIUM"
	}
	return score, severity
}

func writeIncidentScore(sb *strings.Builder, score int, severity string) {
	sb.WriteString("## INCIDENT SCORE\n")
	sb.WriteString(fmt.Sprintf("- severity: %s\n", severity))
	sb.WriteString(fmt.Sprintf("- score: %d (pods*3 + nodes*4 + warnings + restarts)\n\n", score))
}

func writeResourceMix(sb *strings.Builder, counts map[string]int) {
	if len(counts) == 0 {
		return
	}
	type kv struct {
		key string
		val int
	}
	items := make([]kv, 0, len(counts))
	for k, v := range counts {
		items = append(items, kv{key: k, val: v})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].val != items[j].val {
			return items[i].val > items[j].val
		}
		return items[i].key < items[j].key
	})

	sb.WriteString("## RESOURCE MIX\n")
	for _, item := range items {
		sb.WriteString(fmt.Sprintf("- %s: %d\n", item.key, item.val))
	}
	sb.WriteByte('\n')
}

func (a *analysis) inspectPod(r Record, obj map[string]any) {
	status := getMap(obj, "status")
	if len(status) == 0 {
		return
	}
	nsName := namespacedName(r.Namespace, r.Name)

	phase := getString(status, "phase")
	if phase == "Failed" || phase == "Unknown" || phase == "Pending" {
		a.podIssues = append(a.podIssues, fmt.Sprintf("%s phase=%s", nsName, phase))
	}

	conditions := getSlice(status, "conditions")
	for _, c := range conditions {
		cm, ok := c.(map[string]any)
		if !ok {
			continue
		}
		if getString(cm, "type") == "Ready" && getString(cm, "status") == "False" {
			reason := getString(cm, "reason")
			msg := getString(cm, "message")
			a.podIssues = append(a.podIssues, fmt.Sprintf("%s not-ready reason=%s msg=%q", nsName, reason, trim(msg, 120)))
		}
	}

	for _, cs := range getSlice(status, "containerStatuses") {
		container, ok := cs.(map[string]any)
		if !ok {
			continue
		}
		name := getString(container, "name")
		restarts := getInt(container, "restartCount")
		a.totalRestarts += restarts
		state := getMap(container, "state")
		waiting := getMap(state, "waiting")
		if len(waiting) > 0 {
			reason := getString(waiting, "reason")
			msg := trim(getString(waiting, "message"), 100)
			if reason != "" {
				a.podIssues = append(a.podIssues, fmt.Sprintf("%s container=%s waiting=%s msg=%q", nsName, name, reason, msg))
			}
		}
		if restarts > 0 {
			a.podIssues = append(a.podIssues, fmt.Sprintf("%s container=%s restarts=%d", nsName, name, restarts))
		}
	}
}

func (a *analysis) inspectNode(r Record, obj map[string]any) {
	status := getMap(obj, "status")
	for _, c := range getSlice(status, "conditions") {
		cm, ok := c.(map[string]any)
		if !ok {
			continue
		}
		condType := getString(cm, "type")
		condStatus := getString(cm, "status")
		if condStatus != "True" {
			continue
		}
		if condType == "Ready" {
			continue
		}
		if condType == "MemoryPressure" || condType == "DiskPressure" || condType == "PIDPressure" || condType == "NetworkUnavailable" {
			a.nodeIssues = append(a.nodeIssues, fmt.Sprintf("%s %s=True reason=%s", r.Name, condType, getString(cm, "reason")))
		}
	}
	for _, c := range getSlice(status, "conditions") {
		cm, ok := c.(map[string]any)
		if !ok {
			continue
		}
		if getString(cm, "type") == "Ready" && getString(cm, "status") != "True" {
			a.nodeIssues = append(a.nodeIssues, fmt.Sprintf("%s Ready=%s reason=%s", r.Name, getString(cm, "status"), getString(cm, "reason")))
		}
	}
}

func (a *analysis) inspectEvent(r Record, obj map[string]any) {
	eventType := strings.ToUpper(getString(obj, "type"))
	if eventType == "WARNING" {
		reason := getString(obj, "reason")
		msg := trim(getString(obj, "message"), 120)
		a.warningEvents = append(a.warningEvents, fmt.Sprintf("%s reason=%s msg=%q", namespacedName(r.Namespace, r.Name), reason, msg))
		return
	}
	if eventType != "NORMAL" {
		a.unknownEventLevels++
	}
}

func namespacedName(ns, name string) string {
	if ns == "" {
		return name
	}
	return ns + "/" + name
}

func getMap(m map[string]any, key string) map[string]any {
	v, ok := m[key]
	if !ok {
		return nil
	}
	mv, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	return mv
}

func getSlice(m map[string]any, key string) []any {
	v, ok := m[key]
	if !ok {
		return nil
	}
	s, ok := v.([]any)
	if !ok {
		return nil
	}
	return s
}

func getString(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok {
		return ""
	}
	switch tv := v.(type) {
	case string:
		return tv
	case fmt.Stringer:
		return tv.String()
	default:
		return fmt.Sprintf("%v", tv)
	}
}

func getInt(m map[string]any, key string) int {
	v, ok := m[key]
	if !ok {
		return 0
	}
	switch tv := v.(type) {
	case int:
		return tv
	case int64:
		return int(tv)
	case float64:
		return int(tv)
	case string:
		n, err := strconv.Atoi(tv)
		if err != nil {
			return 0
		}
		return n
	default:
		return 0
	}
}

func trim(in string, max int) string {
	if len(in) <= max {
		return in
	}
	return in[:max] + "..."
}

func defaultIfEmpty(val, fallback string) string {
	if val == "" {
		return fallback
	}
	return val
}

func belowSeverityThreshold(current, threshold string) bool {
	if strings.TrimSpace(threshold) == "" {
		return false
	}
	currentRank := severityRank(current)
	thresholdRank := severityRank(strings.ToUpper(strings.TrimSpace(threshold)))
	if thresholdRank == -1 {
		return false
	}
	return currentRank < thresholdRank
}

func severityRank(s string) int {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "LOW":
		return 1
	case "MEDIUM":
		return 2
	case "HIGH":
		return 3
	default:
		return -1
	}
}
