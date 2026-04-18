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
	workloadIssues     []string
	storageIssues      []string
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
		podIssues:      make([]string, 0),
		nodeIssues:     make([]string, 0),
		warningEvents:  make([]string, 0),
		workloadIssues: make([]string, 0),
		storageIssues:  make([]string, 0),
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
		case "deployments":
			a.inspectDeployment(r, obj)
		case "statefulsets":
			a.inspectStatefulSet(r, obj)
		case "daemonsets":
			a.inspectDaemonSet(r, obj)
		case "persistentvolumeclaims":
			a.inspectPVC(r, obj)
		case "persistentvolumes":
			a.inspectPV(r, obj)
		case "replicasets":
			a.inspectReplicaSet(r, obj)
		case "horizontalpodautoscalers":
			a.inspectHPA(r, obj)
		}
	}

	sort.Strings(a.podIssues)
	sort.Strings(a.nodeIssues)
	sort.Strings(a.warningEvents)
	sort.Strings(a.workloadIssues)
	sort.Strings(a.storageIssues)

	var sb strings.Builder
	sb.WriteString("Snapshot Incident Analysis\n")
	sb.WriteString("-------------------------\n")
	sb.WriteString(fmt.Sprintf("Captured at:        %s\n", bundle.Metadata.CapturedAt.Format("2006-01-02 15:04:05 MST")))
	sb.WriteString(fmt.Sprintf("Cluster context:    %s\n", defaultIfEmpty(bundle.Metadata.ClusterHint, "(unknown)")))
	sb.WriteString(fmt.Sprintf("Total records:      %d\n", len(bundle.Records)))
	sb.WriteString(fmt.Sprintf("Total restarts:     %d\n", a.totalRestarts))
	sb.WriteString(fmt.Sprintf("Warning events:     %d\n", len(a.warningEvents)))
	sb.WriteString(fmt.Sprintf("Non-normal events:  %d\n\n", a.unknownEventLevels))

	cappedRestarts := a.totalRestarts
	if cappedRestarts > 50 {
		cappedRestarts = 50
	}
	score, severity := computeIncidentScoreAndSeverity(
		len(a.podIssues), len(a.nodeIssues), len(a.warningEvents),
		cappedRestarts, len(a.workloadIssues), len(a.storageIssues),
	)
	if belowSeverityThreshold(severity, opts.MinSeverity) {
		sb.WriteString("Result: below configured severity threshold\n")
		sb.WriteString(fmt.Sprintf("- current severity: %s\n", severity))
		sb.WriteString(fmt.Sprintf("- threshold: %s\n", strings.ToUpper(strings.TrimSpace(opts.MinSeverity))))
		return sb.String(), nil
	}

	displayLimit := maxItems
	switch strings.ToUpper(strings.TrimSpace(opts.MinSeverity)) {
	case "HIGH":
		displayLimit = 10
	case "MEDIUM":
		displayLimit = 50
	}

	writeIncidentScore(&sb, score, severity)
	if !opts.HideResourceMix {
		writeResourceMix(&sb, a.resourceCounts)
	}
	writeSection(&sb, "POD ISSUES", a.podIssues, displayLimit)
	writeSection(&sb, "NODE ISSUES", a.nodeIssues, displayLimit)
	writeSection(&sb, "WORKLOAD ISSUES", a.workloadIssues, displayLimit)
	writeSection(&sb, "STORAGE ISSUES", a.storageIssues, displayLimit)
	if !opts.HideWarningEvents {
		writeSection(&sb, "WARNING EVENTS", a.warningEvents, displayLimit)
	}
	return sb.String(), nil
}

func computeIncidentScoreAndSeverity(podIssues, nodeIssues, warnings, restarts, workloadIssues, storageIssues int) (int, string) {
	score := podIssues*3 + nodeIssues*4 + warnings + restarts + workloadIssues*3 + storageIssues*2
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
	sb.WriteString(fmt.Sprintf("- severity: %s\n", colorizedSeverity(severity)))
	sb.WriteString(fmt.Sprintf("- score: %d (pods*3 + nodes*4 + workloads*3 + storage*2 + warnings + restarts, capped at 50)\n\n", score))
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
	if isJobPod(obj) {
		return
	}
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
			if reason == "CrashLoopBackOff" {
				a.podIssues = append(a.podIssues, fmt.Sprintf("[CRASHLOOP] %s container=%s msg=%q", nsName, name, msg))
			} else if reason != "" {
				a.podIssues = append(a.podIssues, fmt.Sprintf("%s container=%s waiting=%s msg=%q", nsName, name, reason, msg))
			}
		}

		// Check last terminated state for OOMKill
		lastState := getMap(container, "lastState")
		terminated := getMap(lastState, "terminated")
		if getString(terminated, "reason") == "OOMKilled" {
			a.podIssues = append(a.podIssues, fmt.Sprintf("[OOMKILLED] %s container=%s", nsName, name))
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

func (a *analysis) inspectDeployment(r Record, obj map[string]any) {
	nsName := namespacedName(r.Namespace, r.Name)
	status := getMap(obj, "status")
	spec := getMap(obj, "spec")

	// Zero available replicas when some are desired
	specReplicas := getInt(spec, "replicas")
	availableReplicas := getInt(status, "availableReplicas")
	if specReplicas > 0 && availableReplicas < specReplicas {
		a.workloadIssues = append(a.workloadIssues,
			fmt.Sprintf("[DEPLOY] %s available=%d desired=%d", nsName, availableReplicas, specReplicas))
	}

	for _, c := range getSlice(status, "conditions") {
		cm, ok := c.(map[string]any)
		if !ok {
			continue
		}
		if getString(cm, "type") == "Progressing" && getString(cm, "reason") == "ProgressDeadlineExceeded" {
			a.workloadIssues = append(a.workloadIssues,
				fmt.Sprintf("[DEPLOY] %s rollout-stalled reason=ProgressDeadlineExceeded", nsName))
		}
	}
}

func (a *analysis) inspectStatefulSet(r Record, obj map[string]any) {
	nsName := namespacedName(r.Namespace, r.Name)
	status := getMap(obj, "status")
	spec := getMap(obj, "spec")

	desired := getInt(spec, "replicas")
	ready := getInt(status, "readyReplicas")
	if desired > 0 && ready < desired {
		a.workloadIssues = append(a.workloadIssues,
			fmt.Sprintf("[STS] %s ready=%d desired=%d", nsName, ready, desired))
	}
}

func (a *analysis) inspectReplicaSet(r Record, obj map[string]any) {
	nsName := namespacedName(r.Namespace, r.Name)
	status := getMap(obj, "status")
	spec := getMap(obj, "spec")

	desired := getInt(spec, "replicas")
	ready := getInt(status, "readyReplicas")
	if desired > 0 && ready < desired {
		a.workloadIssues = append(a.workloadIssues,
			fmt.Sprintf("[RS] %s ready=%d desired=%d", nsName, ready, desired))
	}
}

func (a *analysis) inspectDaemonSet(r Record, obj map[string]any) {
	nsName := namespacedName(r.Namespace, r.Name)
	status := getMap(obj, "status")

	desired := getInt(status, "desiredNumberScheduled")
	ready := getInt(status, "numberReady")
	if desired > 0 && ready < desired {
		a.workloadIssues = append(a.workloadIssues,
			fmt.Sprintf("[DS] %s ready=%d desired=%d", nsName, ready, desired))
	}
}

func (a *analysis) inspectPVC(r Record, obj map[string]any) {
	nsName := namespacedName(r.Namespace, r.Name)
	status := getMap(obj, "status")
	phase := getString(status, "phase")
	if phase != "" && phase != "Bound" {
		a.storageIssues = append(a.storageIssues,
			fmt.Sprintf("[PVC] %s phase=%s", nsName, phase))
	}
}

func (a *analysis) inspectPV(r Record, obj map[string]any) {
	status := getMap(obj, "status")
	phase := getString(status, "phase")
	if phase == "Failed" || phase == "Released" {
		a.storageIssues = append(a.storageIssues,
			fmt.Sprintf("[PV] %s phase=%s", r.Name, phase))
	}
}

func (a *analysis) inspectHPA(r Record, obj map[string]any) {
	nsName := namespacedName(r.Namespace, r.Name)
	status := getMap(obj, "status")
	spec := getMap(obj, "spec")

	currentReplicas := getInt(status, "currentReplicas")
	maxReplicas := getInt(spec, "maxReplicas")
	if maxReplicas > 0 && currentReplicas >= maxReplicas {
		a.workloadIssues = append(a.workloadIssues,
			fmt.Sprintf("[HPA] %s at-max-replicas current=%d max=%d", nsName, currentReplicas, maxReplicas))
	}

	for _, c := range getSlice(status, "conditions") {
		cm, ok := c.(map[string]any)
		if !ok {
			continue
		}
		if getString(cm, "type") == "AbleToScale" && getString(cm, "status") == "False" {
			a.workloadIssues = append(a.workloadIssues,
				fmt.Sprintf("[HPA] %s scale-blocked reason=%s", nsName, getString(cm, "reason")))
		}
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

func isJobPod(obj map[string]any) bool {
	metadata := getMap(obj, "metadata")
	for _, ref := range getSlice(metadata, "ownerReferences") {
		refMap, ok := ref.(map[string]any)
		if ok && getString(refMap, "kind") == "Job" {
			return true
		}
	}
	return false
}
