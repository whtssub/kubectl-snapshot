package snapshot

import (
	"strings"
	"testing"
	"time"
)

func makePodRecord(namespace, name string, obj map[string]any) Record {
	return Record{
		Group:     "",
		Version:   "v1",
		Resource:  "pods",
		Namespace: namespace,
		Name:      name,
		Object:    obj,
	}
}

func makeNodeRecord(name string, obj map[string]any) Record {
	return Record{
		Group:    "",
		Version:  "v1",
		Resource: "nodes",
		Name:     name,
		Object:   obj,
	}
}

func makeEventRecord(namespace, name, eventType, reason, message string) Record {
	return Record{
		Group:     "",
		Version:   "v1",
		Resource:  "events",
		Namespace: namespace,
		Name:      name,
		Object: map[string]any{
			"type":    eventType,
			"reason":  reason,
			"message": message,
		},
	}
}

func bundleFromRecords(records []Record) *Bundle {
	return &Bundle{
		Metadata: Metadata{
			ToolVersion: "0.2.0",
			CapturedAt:  time.Now().UTC(),
			ClusterHint: "test-cluster",
		},
		Records: records,
	}
}

// --- Pod analysis ---

func TestAnalyze_FailedPod_Detected(t *testing.T) {
	pod := makePodRecord("default", "bad-pod", map[string]any{
		"status": map[string]any{
			"phase": "Failed",
		},
	})
	out, err := Analyze(bundleFromRecords([]Record{pod}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "bad-pod") {
		t.Error("expected bad-pod to appear in analysis")
	}
	if !strings.Contains(out, "phase=Failed") {
		t.Error("expected phase=Failed in analysis")
	}
}

func TestAnalyze_PendingPod_Detected(t *testing.T) {
	pod := makePodRecord("default", "stuck-pod", map[string]any{
		"status": map[string]any{"phase": "Pending"},
	})
	out, err := Analyze(bundleFromRecords([]Record{pod}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "phase=Pending") {
		t.Error("expected phase=Pending in analysis")
	}
}

func TestAnalyze_RunningPod_NotReported(t *testing.T) {
	pod := makePodRecord("default", "ok-pod", map[string]any{
		"status": map[string]any{
			"phase": "Running",
			"conditions": []any{
				map[string]any{"type": "Ready", "status": "True"},
			},
		},
	})
	out, err := Analyze(bundleFromRecords([]Record{pod}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "ok-pod") {
		t.Error("healthy pod should not appear in issues")
	}
}

func TestAnalyze_PodNotReady_ReportsReason(t *testing.T) {
	pod := makePodRecord("default", "not-ready-pod", map[string]any{
		"status": map[string]any{
			"phase": "Running",
			"conditions": []any{
				map[string]any{
					"type":    "Ready",
					"status":  "False",
					"reason":  "ContainersNotReady",
					"message": "containers with unready status",
				},
			},
		},
	})
	out, err := Analyze(bundleFromRecords([]Record{pod}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "not-ready") {
		t.Error("expected not-ready in analysis")
	}
	if !strings.Contains(out, "ContainersNotReady") {
		t.Error("expected reason ContainersNotReady in analysis")
	}
}

func TestAnalyze_CrashLoopBackOff_Detected(t *testing.T) {
	pod := makePodRecord("sre-lab", "crasher", map[string]any{
		"status": map[string]any{
			"containerStatuses": []any{
				map[string]any{
					"name":         "app",
					"restartCount": float64(5),
					"state": map[string]any{
						"waiting": map[string]any{
							"reason":  "CrashLoopBackOff",
							"message": "back-off 5m0s restarting failed container",
						},
					},
				},
			},
		},
	})
	out, err := Analyze(bundleFromRecords([]Record{pod}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "[CRASHLOOP]") {
		t.Error("expected [CRASHLOOP] prefix in analysis")
	}
	if !strings.Contains(out, "restarts=5") {
		t.Error("expected restart count in analysis")
	}
}

func TestAnalyze_ContainerWaiting_Detected(t *testing.T) {
	pod := makePodRecord("default", "img-err", map[string]any{
		"status": map[string]any{
			"containerStatuses": []any{
				map[string]any{
					"name":         "app",
					"restartCount": float64(0),
					"state": map[string]any{
						"waiting": map[string]any{
							"reason": "ImagePullBackOff",
						},
					},
				},
			},
		},
	})
	out, err := Analyze(bundleFromRecords([]Record{pod}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "ImagePullBackOff") {
		t.Error("expected ImagePullBackOff in analysis")
	}
}

func TestAnalyze_TotalRestarts_Counted(t *testing.T) {
	pod := makePodRecord("default", "restarter", map[string]any{
		"status": map[string]any{
			"containerStatuses": []any{
				map[string]any{
					"name":         "a",
					"restartCount": float64(3),
					"state":        map[string]any{},
				},
				map[string]any{
					"name":         "b",
					"restartCount": float64(2),
					"state":        map[string]any{},
				},
			},
		},
	})
	out, err := Analyze(bundleFromRecords([]Record{pod}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Total restarts:     5") {
		t.Errorf("expected total restarts=5, got:\n%s", out)
	}
}

func TestAnalyze_RestartsCapped_ScoreNotInflated(t *testing.T) {
	pod := makePodRecord("default", "crasher", map[string]any{
		"status": map[string]any{
			"containerStatuses": []any{
				map[string]any{"name": "app", "restartCount": float64(200), "state": map[string]any{}},
			},
		},
	})
	out, err := Analyze(bundleFromRecords([]Record{pod}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Total restarts:     200") {
		t.Error("raw restart count should display uncapped")
	}
	// 1 pod issue (restarts>0) * 3 + capped restarts 50 = 53
	if !strings.Contains(out, "score:    53") {
		t.Errorf("score should be 53 (capped restarts), got:\n%s", out)
	}
}

func TestAnalyze_SucceededPod_NotReported(t *testing.T) {
	pod := makePodRecord("default", "completed-job-abc", map[string]any{
		"status": map[string]any{
			"phase": "Succeeded",
			"containerStatuses": []any{
				map[string]any{"name": "worker", "restartCount": float64(2), "state": map[string]any{}},
			},
		},
	})
	out, err := Analyze(bundleFromRecords([]Record{pod}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "completed-job-abc") {
		t.Error("Succeeded pod should not appear in any issue section")
	}
}

func TestAnalyze_CronJobPod_NotReported(t *testing.T) {
	pod := makePodRecord("default", "cleanup-cronjob-xyz", map[string]any{
		"metadata": map[string]any{
			"ownerReferences": []any{
				map[string]any{"kind": "CronJob", "name": "hourly-cleanup"},
			},
		},
		"status": map[string]any{
			"phase": "Failed",
			"containerStatuses": []any{
				map[string]any{"name": "worker", "restartCount": float64(3), "state": map[string]any{}},
			},
		},
	})
	out, err := Analyze(bundleFromRecords([]Record{pod}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "cleanup-cronjob-xyz") {
		t.Error("CronJob-owned pod should not appear in any issue section")
	}
}

func TestAnalyze_JobPod_NotReported(t *testing.T) {
	pod := makePodRecord("default", "batch-abc", map[string]any{
		"metadata": map[string]any{
			"ownerReferences": []any{
				map[string]any{"kind": "Job", "name": "batch-job"},
			},
		},
		"status": map[string]any{
			"phase": "Failed",
			"containerStatuses": []any{
				map[string]any{"name": "worker", "restartCount": float64(5), "state": map[string]any{}},
			},
		},
	})
	out, err := Analyze(bundleFromRecords([]Record{pod}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "batch-abc") {
		t.Error("Job-owned pod should not appear in any issue section")
	}
}

// --- Node analysis ---

func TestAnalyze_MemoryPressureNode_Detected(t *testing.T) {
	node := makeNodeRecord("node1", map[string]any{
		"status": map[string]any{
			"conditions": []any{
				map[string]any{
					"type":   "MemoryPressure",
					"status": "True",
					"reason": "KubeletHasSufficientMemory",
				},
			},
		},
	})
	out, err := Analyze(bundleFromRecords([]Record{node}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "MemoryPressure") {
		t.Error("expected MemoryPressure in node issues")
	}
}

func TestAnalyze_DiskPressureNode_Detected(t *testing.T) {
	node := makeNodeRecord("node2", map[string]any{
		"status": map[string]any{
			"conditions": []any{
				map[string]any{
					"type":   "DiskPressure",
					"status": "True",
					"reason": "KubeletHasDiskPressure",
				},
			},
		},
	})
	out, err := Analyze(bundleFromRecords([]Record{node}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "DiskPressure") {
		t.Error("expected DiskPressure in node issues")
	}
}

func TestAnalyze_NodeNotReady_Detected(t *testing.T) {
	node := makeNodeRecord("node3", map[string]any{
		"status": map[string]any{
			"conditions": []any{
				map[string]any{
					"type":   "Ready",
					"status": "False",
					"reason": "KubeletNotReady",
				},
			},
		},
	})
	out, err := Analyze(bundleFromRecords([]Record{node}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "node3") {
		t.Error("expected node3 in node issues")
	}
}

func TestAnalyze_HealthyNode_NotReported(t *testing.T) {
	node := makeNodeRecord("healthy-node", map[string]any{
		"status": map[string]any{
			"conditions": []any{
				map[string]any{"type": "Ready", "status": "True"},
				map[string]any{"type": "MemoryPressure", "status": "False"},
				map[string]any{"type": "DiskPressure", "status": "False"},
			},
		},
	})
	out, err := Analyze(bundleFromRecords([]Record{node}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "healthy-node") {
		t.Error("healthy node should not appear in issues")
	}
}

// --- Event analysis ---

func TestAnalyze_WarningEvent_Detected(t *testing.T) {
	ev := makeEventRecord("default", "ev1", "Warning", "BackOff", "back-off restarting failed container")
	out, err := Analyze(bundleFromRecords([]Record{ev}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "BackOff") {
		t.Error("expected BackOff reason in warning events")
	}
	if !strings.Contains(out, "Warning events:     1") {
		t.Error("expected warning count=1")
	}
}

func TestAnalyze_NormalEvent_NotInWarnings(t *testing.T) {
	ev := makeEventRecord("default", "ev2", "Normal", "Pulled", "image pulled successfully")
	out, err := Analyze(bundleFromRecords([]Record{ev}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Warning events:     0") {
		t.Error("normal event should not be counted as warning")
	}
}

// --- Incident scoring ---

func TestComputeIncidentScore_LowSeverity(t *testing.T) {
	score, severity := computeIncidentScoreAndSeverity(0, 0, 5, 2, 0, 0)
	// 0*3 + 0*4 + 5 + 2 = 7
	if score != 7 {
		t.Errorf("expected score=7, got %d", score)
	}
	if severity != "LOW" {
		t.Errorf("expected LOW, got %s", severity)
	}
}

func TestComputeIncidentScore_MediumBoundary(t *testing.T) {
	// Exactly at boundary: score=15 -> MEDIUM
	score, severity := computeIncidentScoreAndSeverity(5, 0, 0, 0, 0, 0)
	// 5*3 = 15
	if score != 15 {
		t.Errorf("expected score=15, got %d", score)
	}
	if severity != "MEDIUM" {
		t.Errorf("expected MEDIUM at score=15, got %s", severity)
	}
}

func TestComputeIncidentScore_JustBelowMedium(t *testing.T) {
	// score=14 -> LOW
	score, severity := computeIncidentScoreAndSeverity(4, 0, 0, 2, 0, 0)
	// 4*3 + 2 = 14
	if score != 14 {
		t.Errorf("expected score=14, got %d", score)
	}
	if severity != "LOW" {
		t.Errorf("expected LOW at score=14, got %s", severity)
	}
}

func TestComputeIncidentScore_HighBoundary(t *testing.T) {
	// Exactly at boundary: score=40 -> HIGH
	score, severity := computeIncidentScoreAndSeverity(0, 10, 0, 0, 0, 0)
	// 0 + 10*4 + 0 + 0 = 40
	if score != 40 {
		t.Errorf("expected score=40, got %d", score)
	}
	if severity != "HIGH" {
		t.Errorf("expected HIGH at score=40, got %s", severity)
	}
}

func TestComputeIncidentScore_JustBelowHigh(t *testing.T) {
	// score=39 -> MEDIUM
	score, severity := computeIncidentScoreAndSeverity(13, 0, 0, 0, 0, 0)
	// 13*3 = 39
	if score != 39 {
		t.Errorf("expected score=39, got %d", score)
	}
	if severity != "MEDIUM" {
		t.Errorf("expected MEDIUM at score=39, got %s", severity)
	}
}

// --- Severity threshold filtering ---

func TestBelowSeverityThreshold_EmptyThreshold_AlwaysFalse(t *testing.T) {
	if belowSeverityThreshold("LOW", "") {
		t.Error("empty threshold should never filter")
	}
	if belowSeverityThreshold("HIGH", "") {
		t.Error("empty threshold should never filter")
	}
}

func TestBelowSeverityThreshold_SameLevel_NotFiltered(t *testing.T) {
	if belowSeverityThreshold("MEDIUM", "MEDIUM") {
		t.Error("same severity should not be filtered")
	}
}

func TestBelowSeverityThreshold_AboveThreshold_NotFiltered(t *testing.T) {
	if belowSeverityThreshold("HIGH", "MEDIUM") {
		t.Error("HIGH above MEDIUM threshold should not be filtered")
	}
}

func TestBelowSeverityThreshold_BelowThreshold_Filtered(t *testing.T) {
	if !belowSeverityThreshold("LOW", "MEDIUM") {
		t.Error("LOW below MEDIUM threshold should be filtered")
	}
}

func TestBelowSeverityThreshold_InvalidThreshold_NotFiltered(t *testing.T) {
	if belowSeverityThreshold("LOW", "CRITICAL") {
		t.Error("invalid threshold should not filter")
	}
}

// --- HideWarningEvents option ---

func TestAnalyze_HideWarningEvents(t *testing.T) {
	ev := makeEventRecord("default", "ev1", "Warning", "BackOff", "restarting failed container")
	out, err := AnalyzeWithOptions(bundleFromRecords([]Record{ev}), AnalyzeOptions{
		HideWarningEvents: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "WARNING EVENTS") {
		t.Error("warning events section should be hidden")
	}
}

// --- HideResourceMix option ---

func TestAnalyze_HideResourceMix(t *testing.T) {
	pod := makePodRecord("default", "p", map[string]any{"status": map[string]any{"phase": "Running"}})
	out, err := AnalyzeWithOptions(bundleFromRecords([]Record{pod}), AnalyzeOptions{
		HideResourceMix: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "RESOURCE MIX") {
		t.Error("resource mix section should be hidden")
	}
}

// --- Resource mix counts ---

func TestAnalyze_ResourceMix_CountsCorrectly(t *testing.T) {
	records := []Record{
		makePodRecord("default", "p1", map[string]any{}),
		makePodRecord("default", "p2", map[string]any{}),
		makeNodeRecord("n1", map[string]any{}),
	}
	out, err := Analyze(bundleFromRecords(records))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "pods") || !strings.Contains(out, "   2") {
		t.Errorf("expected pods count=2 in resource mix, got:\n%s", out)
	}
	if !strings.Contains(out, "nodes") || !strings.Contains(out, "   1") {
		t.Errorf("expected nodes count=1 in resource mix, got:\n%s", out)
	}
}

// --- namespacedName ---

func TestNamespacedName_WithNamespace(t *testing.T) {
	got := namespacedName("kube-system", "coredns")
	if got != "kube-system/coredns" {
		t.Errorf("expected kube-system/coredns, got %q", got)
	}
}

func TestNamespacedName_NoNamespace(t *testing.T) {
	got := namespacedName("", "node1")
	if got != "node1" {
		t.Errorf("expected node1 (no namespace prefix), got %q", got)
	}
}

// ---- helpers for new signal tests ----

func makeDeployRecord(namespace, name string, obj map[string]any) Record {
	return Record{Group: "apps", Version: "v1", Resource: "deployments", Namespace: namespace, Name: name, Object: obj}
}

func makeSTSRecord(namespace, name string, obj map[string]any) Record {
	return Record{Group: "apps", Version: "v1", Resource: "statefulsets", Namespace: namespace, Name: name, Object: obj}
}

func makeDSRecord(namespace, name string, obj map[string]any) Record {
	return Record{Group: "apps", Version: "v1", Resource: "daemonsets", Namespace: namespace, Name: name, Object: obj}
}

func makePVCRecord(namespace, name string, obj map[string]any) Record {
	return Record{Group: "", Version: "v1", Resource: "persistentvolumeclaims", Namespace: namespace, Name: name, Object: obj}
}

func makePVRecord(name string, obj map[string]any) Record {
	return Record{Group: "", Version: "v1", Resource: "persistentvolumes", Name: name, Object: obj}
}

func makeRSRecord(namespace, name string, obj map[string]any) Record {
	return Record{Group: "apps", Version: "v1", Resource: "replicasets", Namespace: namespace, Name: name, Object: obj}
}

func makeHPARecord(namespace, name string, obj map[string]any) Record {
	return Record{Group: "autoscaling", Version: "v2", Resource: "horizontalpodautoscalers", Namespace: namespace, Name: name, Object: obj}
}

// --- Deployment analysis ---

func TestAnalyze_Deployment_ZeroAvailableReplicas(t *testing.T) {
	deploy := makeDeployRecord("default", "api", map[string]any{
		"spec":   map[string]any{"replicas": float64(3)},
		"status": map[string]any{"availableReplicas": float64(0)},
	})
	out, err := Analyze(bundleFromRecords([]Record{deploy}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "[DEPLOY]") {
		t.Error("expected [DEPLOY] in workload issues")
	}
	if !strings.Contains(out, "available=0 desired=3") {
		t.Error("expected available=0 desired=3 signal")
	}
}

func TestAnalyze_Deployment_Unavailable(t *testing.T) {
	deploy := makeDeployRecord("default", "web", map[string]any{
		"spec": map[string]any{"replicas": float64(2)},
		"status": map[string]any{
			"availableReplicas": float64(1),
			"conditions": []any{
				map[string]any{"type": "Available", "status": "False", "reason": "MinimumReplicasUnavailable"},
			},
		},
	})
	out, err := Analyze(bundleFromRecords([]Record{deploy}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "available=1 desired=2") {
		t.Error("expected available=1 desired=2 signal in workload issues")
	}
}

func TestAnalyze_Deployment_RolloutFailed(t *testing.T) {
	deploy := makeDeployRecord("default", "frontend", map[string]any{
		"spec": map[string]any{"replicas": float64(1)},
		"status": map[string]any{
			"availableReplicas": float64(1),
			"conditions": []any{
				map[string]any{
					"type":   "Progressing",
					"status": "False",
					"reason": "ProgressDeadlineExceeded",
				},
			},
		},
	})
	out, err := Analyze(bundleFromRecords([]Record{deploy}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "rollout-stalled") {
		t.Error("expected rollout-stalled signal")
	}
	if !strings.Contains(out, "ProgressDeadlineExceeded") {
		t.Error("expected ProgressDeadlineExceeded reason")
	}
}

func TestAnalyze_Deployment_Healthy_NotReported(t *testing.T) {
	deploy := makeDeployRecord("default", "healthy", map[string]any{
		"spec": map[string]any{"replicas": float64(3)},
		"status": map[string]any{
			"availableReplicas": float64(3),
			"conditions": []any{
				map[string]any{"type": "Available", "status": "True"},
			},
		},
	})
	out, err := Analyze(bundleFromRecords([]Record{deploy}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "[DEPLOY]") {
		t.Error("healthy deployment should not appear in workload issues")
	}
}

// --- ReplicaSet analysis ---

func TestAnalyze_ReplicaSet_PartiallyReady_Reported(t *testing.T) {
	rs := makeRSRecord("default", "api-rs", map[string]any{
		"spec":   map[string]any{"replicas": float64(3)},
		"status": map[string]any{"readyReplicas": float64(1)},
	})
	out, err := Analyze(bundleFromRecords([]Record{rs}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "[RS]") {
		t.Error("expected [RS] in workload issues")
	}
	if !strings.Contains(out, "ready=1") || !strings.Contains(out, "desired=3") {
		t.Errorf("expected ready/desired counts in output, got:\n%s", out)
	}
}

func TestAnalyze_ReplicaSet_FullyReady_NotReported(t *testing.T) {
	rs := makeRSRecord("default", "api-rs-ok", map[string]any{
		"spec":   map[string]any{"replicas": float64(3)},
		"status": map[string]any{"readyReplicas": float64(3)},
	})
	out, err := Analyze(bundleFromRecords([]Record{rs}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "[RS]") {
		t.Error("fully ready ReplicaSet should not appear in workload issues")
	}
}

// --- StatefulSet analysis ---

func TestAnalyze_StatefulSet_PartiallyReady(t *testing.T) {
	sts := makeSTSRecord("default", "db", map[string]any{
		"spec":   map[string]any{"replicas": float64(3)},
		"status": map[string]any{"readyReplicas": float64(1)},
	})
	out, err := Analyze(bundleFromRecords([]Record{sts}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "[STS]") {
		t.Error("expected [STS] in workload issues")
	}
	if !strings.Contains(out, "ready=1") {
		t.Error("expected ready=1 in output")
	}
	if !strings.Contains(out, "desired=3") {
		t.Error("expected desired=3 in output")
	}
}

func TestAnalyze_StatefulSet_Healthy_NotReported(t *testing.T) {
	sts := makeSTSRecord("default", "db-ok", map[string]any{
		"spec":   map[string]any{"replicas": float64(3)},
		"status": map[string]any{"readyReplicas": float64(3)},
	})
	out, err := Analyze(bundleFromRecords([]Record{sts}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "[STS]") {
		t.Error("healthy StatefulSet should not appear in workload issues")
	}
}

// --- DaemonSet analysis ---

func TestAnalyze_DaemonSet_NotFullyReady(t *testing.T) {
	ds := makeDSRecord("kube-system", "fluentd", map[string]any{
		"status": map[string]any{
			"desiredNumberScheduled": float64(5),
			"numberReady":            float64(3),
		},
	})
	out, err := Analyze(bundleFromRecords([]Record{ds}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "[DS]") {
		t.Error("expected [DS] in workload issues")
	}
	if !strings.Contains(out, "ready=3") || !strings.Contains(out, "desired=5") {
		t.Errorf("expected ready/desired counts in output, got:\n%s", out)
	}
}

func TestAnalyze_DaemonSet_FullyReady_NotReported(t *testing.T) {
	ds := makeDSRecord("kube-system", "node-agent", map[string]any{
		"status": map[string]any{
			"desiredNumberScheduled": float64(3),
			"numberReady":            float64(3),
		},
	})
	out, err := Analyze(bundleFromRecords([]Record{ds}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "[DS]") {
		t.Error("fully ready DaemonSet should not appear in workload issues")
	}
}

// --- PVC analysis ---

func TestAnalyze_PVC_Pending(t *testing.T) {
	pvc := makePVCRecord("default", "data-pvc", map[string]any{
		"status": map[string]any{"phase": "Pending"},
	})
	out, err := Analyze(bundleFromRecords([]Record{pvc}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "[PVC]") {
		t.Error("expected [PVC] in storage issues")
	}
	if !strings.Contains(out, "phase=Pending") {
		t.Error("expected phase=Pending in storage issues")
	}
}

func TestAnalyze_PVC_Lost(t *testing.T) {
	pvc := makePVCRecord("default", "lost-pvc", map[string]any{
		"status": map[string]any{"phase": "Lost"},
	})
	out, err := Analyze(bundleFromRecords([]Record{pvc}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "phase=Lost") {
		t.Error("expected phase=Lost in storage issues")
	}
}

func TestAnalyze_PVC_Bound_NotReported(t *testing.T) {
	pvc := makePVCRecord("default", "ok-pvc", map[string]any{
		"status": map[string]any{"phase": "Bound"},
	})
	out, err := Analyze(bundleFromRecords([]Record{pvc}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "[PVC]") {
		t.Error("Bound PVC should not appear in storage issues")
	}
}

// --- PV analysis ---

func TestAnalyze_PV_Failed(t *testing.T) {
	pv := makePVRecord("pv-001", map[string]any{
		"status": map[string]any{"phase": "Failed"},
	})
	out, err := Analyze(bundleFromRecords([]Record{pv}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "[PV]") {
		t.Error("expected [PV] in storage issues")
	}
}

func TestAnalyze_PV_Released(t *testing.T) {
	pv := makePVRecord("pv-002", map[string]any{
		"status": map[string]any{"phase": "Released"},
	})
	out, err := Analyze(bundleFromRecords([]Record{pv}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "phase=Released") {
		t.Error("expected phase=Released in storage issues")
	}
}

func TestAnalyze_PV_Bound_NotReported(t *testing.T) {
	pv := makePVRecord("pv-ok", map[string]any{
		"status": map[string]any{"phase": "Bound"},
	})
	out, err := Analyze(bundleFromRecords([]Record{pv}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "[PV]") {
		t.Error("Bound PV should not appear in storage issues")
	}
}

// --- HPA analysis ---

func TestAnalyze_HPA_AtMaxReplicas(t *testing.T) {
	hpa := makeHPARecord("default", "web-hpa", map[string]any{
		"spec":   map[string]any{"maxReplicas": float64(10)},
		"status": map[string]any{"currentReplicas": float64(10)},
	})
	out, err := Analyze(bundleFromRecords([]Record{hpa}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "[HPA]") {
		t.Error("expected [HPA] in workload issues")
	}
	if !strings.Contains(out, "at-max-replicas") {
		t.Error("expected at-max-replicas signal")
	}
}

func TestAnalyze_HPA_ScaleBlocked(t *testing.T) {
	hpa := makeHPARecord("default", "api-hpa", map[string]any{
		"spec": map[string]any{"maxReplicas": float64(20)},
		"status": map[string]any{
			"currentReplicas": float64(5),
			"conditions": []any{
				map[string]any{
					"type":   "AbleToScale",
					"status": "False",
					"reason": "FailedGetScale",
				},
			},
		},
	})
	out, err := Analyze(bundleFromRecords([]Record{hpa}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "scale-blocked") {
		t.Error("expected scale-blocked signal")
	}
	if !strings.Contains(out, "FailedGetScale") {
		t.Error("expected FailedGetScale reason")
	}
}

func TestAnalyze_HPA_Healthy_NotReported(t *testing.T) {
	hpa := makeHPARecord("default", "ok-hpa", map[string]any{
		"spec":   map[string]any{"maxReplicas": float64(10)},
		"status": map[string]any{"currentReplicas": float64(3)},
	})
	out, err := Analyze(bundleFromRecords([]Record{hpa}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "[HPA]") {
		t.Error("healthy HPA should not appear in workload issues")
	}
}

func makeJobRecord(namespace, name string, obj map[string]any) Record {
	return Record{Group: "batch", Version: "v1", Resource: "jobs", Namespace: namespace, Name: name, Object: obj}
}

func makeCronJobRecord(namespace, name string, obj map[string]any) Record {
	return Record{Group: "batch", Version: "v1", Resource: "cronjobs", Namespace: namespace, Name: name, Object: obj}
}

// --- Job analysis ---

func TestAnalyze_Job_Suspended_Flagged(t *testing.T) {
	job := makeJobRecord("default", "import-job", map[string]any{
		"spec":   map[string]any{"suspend": true},
		"status": map[string]any{},
	})
	out, err := Analyze(bundleFromRecords([]Record{job}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "[JOB]") {
		t.Error("expected [JOB] in workload issues")
	}
	if !strings.Contains(out, "suspended") {
		t.Error("expected 'suspended' signal for suspended job")
	}
}

func TestAnalyze_Job_FailedCondition_Flagged(t *testing.T) {
	job := makeJobRecord("default", "etl-job", map[string]any{
		"spec": map[string]any{},
		"status": map[string]any{
			"conditions": []any{
				map[string]any{"type": "Failed", "status": "True", "reason": "BackoffLimitExceeded"},
			},
		},
	})
	out, err := Analyze(bundleFromRecords([]Record{job}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "[JOB]") {
		t.Error("expected [JOB] in workload issues")
	}
	if !strings.Contains(out, "BackoffLimitExceeded") {
		t.Error("expected BackoffLimitExceeded reason in output")
	}
}

func TestAnalyze_Job_FailedAttempts_NoCompleteCondition_Flagged(t *testing.T) {
	job := makeJobRecord("default", "retry-job", map[string]any{
		"spec":   map[string]any{},
		"status": map[string]any{"failed": float64(2)},
	})
	out, err := Analyze(bundleFromRecords([]Record{job}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "failed-attempts=2") {
		t.Errorf("expected failed-attempts=2 signal, got:\n%s", out)
	}
}

func TestAnalyze_Job_CompletedDespiteRetries_NotFlagged(t *testing.T) {
	job := makeJobRecord("default", "eventual-job", map[string]any{
		"spec": map[string]any{},
		"status": map[string]any{
			"failed": float64(1),
			"conditions": []any{
				map[string]any{"type": "Complete", "status": "True"},
			},
		},
	})
	out, err := Analyze(bundleFromRecords([]Record{job}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "[JOB]") {
		t.Error("job that eventually completed should not appear in workload issues")
	}
}

func TestAnalyze_Job_Healthy_NotReported(t *testing.T) {
	job := makeJobRecord("default", "ok-job", map[string]any{
		"spec": map[string]any{},
		"status": map[string]any{
			"succeeded": float64(1),
			"conditions": []any{
				map[string]any{"type": "Complete", "status": "True"},
			},
		},
	})
	out, err := Analyze(bundleFromRecords([]Record{job}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "[JOB]") {
		t.Error("healthy completed job should not appear in workload issues")
	}
}

// --- CronJob analysis ---

func TestAnalyze_CronJob_Suspended_Flagged(t *testing.T) {
	cj := makeCronJobRecord("default", "nightly-report", map[string]any{
		"spec":   map[string]any{"suspend": true},
		"status": map[string]any{},
	})
	out, err := Analyze(bundleFromRecords([]Record{cj}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "[CRONJOB]") {
		t.Error("expected [CRONJOB] in workload issues")
	}
	if !strings.Contains(out, "suspended") {
		t.Error("expected 'suspended' signal for suspended CronJob")
	}
}

func TestAnalyze_CronJob_NeverSucceeded_Flagged(t *testing.T) {
	cj := makeCronJobRecord("default", "hourly-cleanup", map[string]any{
		"spec": map[string]any{},
		"status": map[string]any{
			"lastScheduleTime": "2026-04-27T10:00:00Z",
		},
	})
	out, err := Analyze(bundleFromRecords([]Record{cj}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "[CRONJOB]") {
		t.Error("expected [CRONJOB] in workload issues")
	}
	if !strings.Contains(out, "never-succeeded") {
		t.Error("expected 'never-succeeded' signal")
	}
}

func TestAnalyze_CronJob_HasSucceeded_NotFlagged(t *testing.T) {
	cj := makeCronJobRecord("default", "healthy-cj", map[string]any{
		"spec": map[string]any{},
		"status": map[string]any{
			"lastScheduleTime":   "2026-04-27T10:00:00Z",
			"lastSuccessfulTime": "2026-04-27T10:00:30Z",
		},
	})
	out, err := Analyze(bundleFromRecords([]Record{cj}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "[CRONJOB]") {
		t.Error("CronJob with recent success should not appear in workload issues")
	}
}

// --- OOMKilled prefix ---

func TestAnalyze_OOMKilled_Prefixed(t *testing.T) {
	pod := makePodRecord("default", "mem-hog", map[string]any{
		"status": map[string]any{
			"containerStatuses": []any{
				map[string]any{
					"name":         "app",
					"restartCount": float64(2),
					"state":        map[string]any{},
					"lastState": map[string]any{
						"terminated": map[string]any{
							"reason": "OOMKilled",
						},
					},
				},
			},
		},
	})
	out, err := Analyze(bundleFromRecords([]Record{pod}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "[OOMKILLED]") {
		t.Error("expected [OOMKILLED] prefix in pod issues")
	}
}

// --- Incident score includes workload and storage issues ---

func TestComputeIncidentScore_WorkloadAndStorageContribute(t *testing.T) {
	score, severity := computeIncidentScoreAndSeverity(0, 0, 0, 0, 3, 2)
	// 3*3 + 2*2 = 9 + 4 = 13
	if score != 13 {
		t.Errorf("expected score=13, got %d", score)
	}
	if severity != "LOW" {
		t.Errorf("expected LOW, got %s", severity)
	}
}

func TestComputeIncidentScore_WorkloadPushesToMedium(t *testing.T) {
	score, severity := computeIncidentScoreAndSeverity(0, 0, 0, 0, 5, 0)
	// 5*3 = 15
	if score != 15 {
		t.Errorf("expected score=15, got %d", score)
	}
	if severity != "MEDIUM" {
		t.Errorf("expected MEDIUM, got %s", severity)
	}
}

// --- Workload/Storage sections always present in output ---

func TestAnalyze_WorkloadIssuesSection_AlwaysPresent(t *testing.T) {
	out, err := Analyze(bundleFromRecords([]Record{}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "WORKLOAD ISSUES") {
		t.Error("WORKLOAD ISSUES section should always be present")
	}
}

func TestAnalyze_StorageIssuesSection_AlwaysPresent(t *testing.T) {
	out, err := Analyze(bundleFromRecords([]Record{}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "STORAGE ISSUES") {
		t.Error("STORAGE ISSUES section should always be present")
	}
}
