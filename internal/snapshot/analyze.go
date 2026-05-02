package snapshot

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// AnalysisResult is the structured form of an analysis report. Used for
// JSON output via `--output json`.
type AnalysisResult struct {
	Metadata       AnalysisMetadata `json:"metadata"`
	Incident       IncidentScore    `json:"incident"`
	Filtered       bool             `json:"filtered,omitempty"`
	FilterReason   string           `json:"filterReason,omitempty"`
	PodIssues      []string         `json:"podIssues"`
	NodeIssues     []string         `json:"nodeIssues"`
	WorkloadIssues []string         `json:"workloadIssues"`
	StorageIssues  []string         `json:"storageIssues"`
	WarningEvents  []string         `json:"warningEvents,omitempty"`
	ResourceCounts map[string]int   `json:"resourceCounts,omitempty"`
}

type AnalysisMetadata struct {
	CapturedAt        time.Time `json:"capturedAt"`
	ClusterContext    string    `json:"clusterContext,omitempty"`
	TotalRecords      int       `json:"totalRecords"`
	TotalRestarts     int       `json:"totalRestarts"`
	WarningEventCount int       `json:"warningEventCount"`
	NonNormalEvents   int       `json:"nonNormalEvents,omitempty"`
}

type IncidentScore struct {
	Score    int    `json:"score"`
	Severity string `json:"severity"`
}

type analysis struct {
	podIssues          []string
	nodeIssues         []string
	warningEvents      []string
	workloadIssues     []string
	storageIssues      []string
	totalRestarts      int
	unknownEventLevels int
	resourceCounts     map[string]int
	serviceSet         map[string]bool // namespace/name → true, for ingress backend lookup
	nsHasIngressNetpol map[string]bool // namespace → true if any NetworkPolicy applies to Ingress
	nsActivePodCount   map[string]int  // namespace → count of non-job, non-succeeded pods
	eventCutoff        time.Time       // events with timestamps older than this are skipped (zero = no filter)
}

func Analyze(bundle *Bundle) (string, error) {
	return AnalyzeWithOptions(bundle, AnalyzeOptions{})
}

type AnalyzeOptions struct {
	MaxItems          int
	MinSeverity       string
	HideResourceMix   bool
	HideWarningEvents bool
	OutputFormat      string        // "text" (default) or "json"
	Since             time.Duration // when >0, drop events older than (CapturedAt - Since)
	Namespace         string        // when non-empty, only analyse records in this namespace (cluster-scoped records are always included)
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
		resourceCounts:     make(map[string]int),
		serviceSet:         make(map[string]bool),
		nsHasIngressNetpol: make(map[string]bool),
		nsActivePodCount:   make(map[string]int),
	}

	if opts.Since > 0 {
		a.eventCutoff = bundle.Metadata.CapturedAt.Add(-opts.Since)
	}

	// Apply namespace filter: keep cluster-scoped records (Namespace=="") and
	// records whose namespace matches opts.Namespace.
	records := bundle.Records
	if opts.Namespace != "" {
		filtered := make([]Record, 0, len(records))
		for _, r := range records {
			if r.Namespace == "" || r.Namespace == opts.Namespace {
				filtered = append(filtered, r)
			}
		}
		records = filtered
	}

	// Pre-pass: build cross-reference indexes used by inspectors that need
	// to look across record types (e.g. ingress→service, pod→networkpolicy).
	for _, r := range records {
		obj, ok := r.Object.(map[string]any)
		if !ok {
			continue
		}
		switch r.Resource {
		case "services":
			a.serviceSet[namespacedName(r.Namespace, r.Name)] = true
		case "networkpolicies":
			if hasIngressPolicyType(obj) {
				a.nsHasIngressNetpol[r.Namespace] = true
			}
		case "pods":
			if isJobPod(obj) {
				continue
			}
			if getString(getMap(obj, "status"), "phase") == "Succeeded" {
				continue
			}
			a.nsActivePodCount[r.Namespace]++
		}
	}

	for _, r := range records {
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
		case "jobs":
			a.inspectJob(r, obj)
		case "cronjobs":
			a.inspectCronJob(r, obj)
		case "ingresses":
			a.inspectIngress(r, obj)
		}
	}

	a.detectNetworkPolicyGaps()

	sort.Strings(a.podIssues)
	sort.Strings(a.nodeIssues)
	sort.Strings(a.warningEvents)
	sort.Strings(a.workloadIssues)
	sort.Strings(a.storageIssues)

	cappedRestarts := a.totalRestarts
	if cappedRestarts > 50 {
		cappedRestarts = 50
	}
	score, severity := computeIncidentScoreAndSeverity(
		len(a.podIssues), len(a.nodeIssues), len(a.warningEvents),
		cappedRestarts, len(a.workloadIssues), len(a.storageIssues),
	)

	if strings.ToLower(strings.TrimSpace(opts.OutputFormat)) == "json" {
		return renderJSON(bundle, a, score, severity, len(records), opts)
	}

	var sb strings.Builder
	sb.WriteString(clr(ansiBold, "📸 Snapshot Incident Analysis"))
	sb.WriteByte('\n')
	sb.WriteString(clr(ansiDim, "═════════════════════════════════"))
	sb.WriteByte('\n')
	sb.WriteString(fmt.Sprintf("Captured at:        %s\n", bundle.Metadata.CapturedAt.Format("2006-01-02 15:04:05 MST")))
	sb.WriteString(fmt.Sprintf("Cluster context:    %s\n", defaultIfEmpty(bundle.Metadata.ClusterHint, "(unknown)")))
	sb.WriteString(fmt.Sprintf("Total records:      %d\n", len(records)))
	sb.WriteString(fmt.Sprintf("Total restarts:     %d\n", a.totalRestarts))
	sb.WriteString(fmt.Sprintf("Warning events:     %d\n", len(a.warningEvents)))
	sb.WriteString(fmt.Sprintf("Non-normal events:  %d\n\n", a.unknownEventLevels))

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

func renderJSON(bundle *Bundle, a *analysis, score int, severity string, totalRecords int, opts AnalyzeOptions) (string, error) {
	result := AnalysisResult{
		Metadata: AnalysisMetadata{
			CapturedAt:        bundle.Metadata.CapturedAt,
			ClusterContext:    bundle.Metadata.ClusterHint,
			TotalRecords:      totalRecords,
			TotalRestarts:     a.totalRestarts,
			WarningEventCount: len(a.warningEvents),
			NonNormalEvents:   a.unknownEventLevels,
		},
		Incident:       IncidentScore{Score: score, Severity: severity},
		PodIssues:      ensureSlice(a.podIssues),
		NodeIssues:     ensureSlice(a.nodeIssues),
		WorkloadIssues: ensureSlice(a.workloadIssues),
		StorageIssues:  ensureSlice(a.storageIssues),
	}
	if !opts.HideWarningEvents {
		result.WarningEvents = ensureSlice(a.warningEvents)
	}
	if !opts.HideResourceMix {
		result.ResourceCounts = a.resourceCounts
	}
	if belowSeverityThreshold(severity, opts.MinSeverity) {
		result.Filtered = true
		result.FilterReason = fmt.Sprintf("below configured severity threshold %s", strings.ToUpper(strings.TrimSpace(opts.MinSeverity)))
		result.PodIssues = []string{}
		result.NodeIssues = []string{}
		result.WorkloadIssues = []string{}
		result.StorageIssues = []string{}
		result.WarningEvents = nil
		result.ResourceCounts = nil
	}
	b, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal analysis result: %w", err)
	}
	return string(b) + "\n", nil
}

// ensureSlice returns an empty slice instead of nil so JSON output is
// always `[]` rather than `null` for empty issue categories.
func ensureSlice(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
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
	sb.WriteString(clr(ansiBold, "⚠️ INCIDENT SCORE"))
	sb.WriteByte('\n')
	sb.WriteString(fmt.Sprintf("- severity: %s\n", colorizedSeverity(severity)))
	sb.WriteString(fmt.Sprintf("- score:    %d\n", score))
	sb.WriteString("- formula:  pods×3 + nodes×4 + workloads×3 + storage×2 + warnings + restarts (cap 50)\n")
	sb.WriteString("- thresholds: LOW <15 · MEDIUM 15–39 · HIGH ≥40\n\n")
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

	sb.WriteString(clr(ansiBold, "📦 RESOURCE MIX"))
	sb.WriteByte('\n')
	for _, item := range items {
		sb.WriteString(fmt.Sprintf("  %-25s %4d\n", item.key, item.val))
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
	if phase == "Succeeded" {
		return
	}
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
	if !a.eventCutoff.IsZero() {
		ts := getEventTimestamp(obj)
		if !ts.IsZero() && ts.Before(a.eventCutoff) {
			return
		}
	}
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

// getEventTimestamp extracts the most relevant timestamp from a Kubernetes
// Event object. Tries lastTimestamp, then eventTime, then firstTimestamp.
// Returns zero time if none parse cleanly.
func getEventTimestamp(obj map[string]any) time.Time {
	for _, field := range []string{"lastTimestamp", "eventTime", "firstTimestamp"} {
		if s := getString(obj, field); s != "" {
			if t, err := time.Parse(time.RFC3339, s); err == nil {
				return t
			}
		}
	}
	return time.Time{}
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

func (a *analysis) detectNetworkPolicyGaps() {
	for ns, count := range a.nsActivePodCount {
		if count == 0 || a.nsHasIngressNetpol[ns] {
			continue
		}
		a.workloadIssues = append(a.workloadIssues,
			fmt.Sprintf("[NETPOL] %s no-ingress-policy %d pods exposed (implicit allow-all)", ns, count))
	}
}

// hasIngressPolicyType reports whether a NetworkPolicy object applies to
// ingress traffic. A policy applies to Ingress when its policyTypes list
// contains "Ingress" or when policyTypes is unset (per spec, the default
// is ["Ingress"]).
func hasIngressPolicyType(policy map[string]any) bool {
	policyTypes := getSlice(getMap(policy, "spec"), "policyTypes")
	if len(policyTypes) == 0 {
		return true
	}
	for _, pt := range policyTypes {
		if s, ok := pt.(string); ok && s == "Ingress" {
			return true
		}
	}
	return false
}

func (a *analysis) inspectIngress(r Record, obj map[string]any) {
	nsName := namespacedName(r.Namespace, r.Name)
	spec := getMap(obj, "spec")
	seen := make(map[string]bool)

	check := func(svcName, location string) {
		if svcName == "" {
			return
		}
		if a.serviceSet[r.Namespace+"/"+svcName] {
			return
		}
		signal := fmt.Sprintf("[INGRESS] %s %s missing-service=%s", nsName, location, svcName)
		if seen[signal] {
			return
		}
		seen[signal] = true
		a.workloadIssues = append(a.workloadIssues, signal)
	}

	if defaultBackend := getMap(spec, "defaultBackend"); len(defaultBackend) > 0 {
		svc := getMap(defaultBackend, "service")
		check(getString(svc, "name"), "default-backend")
	}

	for _, rule := range getSlice(spec, "rules") {
		ruleMap, ok := rule.(map[string]any)
		if !ok {
			continue
		}
		http := getMap(ruleMap, "http")
		for _, p := range getSlice(http, "paths") {
			pathMap, ok := p.(map[string]any)
			if !ok {
				continue
			}
			backend := getMap(pathMap, "backend")
			svc := getMap(backend, "service")
			pathStr := getString(pathMap, "path")
			if pathStr == "" {
				pathStr = "/"
			}
			check(getString(svc, "name"), "path="+pathStr)
		}
	}
}

func (a *analysis) inspectJob(r Record, obj map[string]any) {
	nsName := namespacedName(r.Namespace, r.Name)
	spec := getMap(obj, "spec")
	status := getMap(obj, "status")

	if getBool(spec, "suspend") {
		a.workloadIssues = append(a.workloadIssues,
			fmt.Sprintf("[JOB] %s suspended", nsName))
		return
	}

	for _, c := range getSlice(status, "conditions") {
		cm, ok := c.(map[string]any)
		if !ok {
			continue
		}
		if getString(cm, "type") == "Failed" && getString(cm, "status") == "True" {
			a.workloadIssues = append(a.workloadIssues,
				fmt.Sprintf("[JOB] %s failed reason=%s", nsName, getString(cm, "reason")))
			return
		}
	}

	failed := getInt(status, "failed")
	if failed > 0 {
		for _, c := range getSlice(status, "conditions") {
			cm, ok := c.(map[string]any)
			if !ok {
				continue
			}
			if getString(cm, "type") == "Complete" && getString(cm, "status") == "True" {
				return // completed successfully despite retries
			}
		}
		a.workloadIssues = append(a.workloadIssues,
			fmt.Sprintf("[JOB] %s failed-attempts=%d", nsName, failed))
	}
}

func (a *analysis) inspectCronJob(r Record, obj map[string]any) {
	nsName := namespacedName(r.Namespace, r.Name)
	spec := getMap(obj, "spec")
	status := getMap(obj, "status")

	if getBool(spec, "suspend") {
		a.workloadIssues = append(a.workloadIssues,
			fmt.Sprintf("[CRONJOB] %s suspended", nsName))
		return
	}

	lastSchedule := getString(status, "lastScheduleTime")
	lastSuccess := getString(status, "lastSuccessfulTime")
	if lastSchedule != "" && lastSuccess == "" {
		a.workloadIssues = append(a.workloadIssues,
			fmt.Sprintf("[CRONJOB] %s never-succeeded last-schedule=%s", nsName, lastSchedule))
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

func getBool(m map[string]any, key string) bool {
	v, ok := m[key]
	if !ok {
		return false
	}
	b, ok := v.(bool)
	return ok && b
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
		if !ok {
			continue
		}
		switch getString(refMap, "kind") {
		case "Job", "CronJob":
			return true
		}
	}
	return false
}
