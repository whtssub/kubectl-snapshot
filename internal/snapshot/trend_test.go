package snapshot

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func makeBundleWithMetrics(capturedAt time.Time, cluster string, pods, nodes, restarts, warnings int) *Bundle {
	records := make([]Record, 0)

	for i := 0; i < pods; i++ {
		cs := make([]any, 0)
		if restarts > 0 && i == 0 {
			cs = append(cs, map[string]any{"name": "app", "restartCount": float64(restarts)})
		}
		records = append(records, Record{
			Version:   "v1",
			Resource:  "pods",
			Namespace: "default",
			Name:      fmt.Sprintf("pod-%d", i),
			Object: map[string]any{
				"status": map[string]any{
					"containerStatuses": cs,
				},
			},
		})
	}
	for i := 0; i < nodes; i++ {
		records = append(records, Record{
			Version:  "v1",
			Resource: "nodes",
			Name:     fmt.Sprintf("node-%d", i),
			Object:   map[string]any{},
		})
	}
	for i := 0; i < warnings; i++ {
		records = append(records, Record{
			Version:   "v1",
			Resource:  "events",
			Namespace: "default",
			Name:      fmt.Sprintf("ev-%d", i),
			Object: map[string]any{
				"type":    "Warning",
				"reason":  "BackOff",
				"message": "test warning",
			},
		})
	}
	return &Bundle{
		Metadata: Metadata{
			ToolVersion: "test",
			CapturedAt:  capturedAt,
			ClusterHint: cluster,
		},
		Records: records,
	}
}

func TestComputeTrend_CountsResourcesCorrectly(t *testing.T) {
	now := time.Now()
	b := makeBundleWithMetrics(now, "prod", 3, 2, 5, 1)
	report := ComputeTrend([]*Bundle{b})
	if len(report.Points) != 1 {
		t.Fatalf("expected 1 point, got %d", len(report.Points))
	}
	p := report.Points[0]
	if p.PodCount != 3 {
		t.Errorf("PodCount: want 3, got %d", p.PodCount)
	}
	if p.NodeCount != 2 {
		t.Errorf("NodeCount: want 2, got %d", p.NodeCount)
	}
	if p.RestartTotal != 5 {
		t.Errorf("RestartTotal: want 5, got %d", p.RestartTotal)
	}
	if p.WarningEventCount != 1 {
		t.Errorf("WarningEventCount: want 1, got %d", p.WarningEventCount)
	}
}

func TestComputeTrend_PreservesOrder(t *testing.T) {
	t1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := t1.Add(24 * time.Hour)
	b1 := makeBundleWithMetrics(t1, "prod", 5, 3, 0, 0)
	b2 := makeBundleWithMetrics(t2, "prod", 7, 3, 2, 1)
	report := ComputeTrend([]*Bundle{b1, b2})
	if report.Points[0].PodCount != 5 {
		t.Errorf("first point pods: want 5, got %d", report.Points[0].PodCount)
	}
	if report.Points[1].PodCount != 7 {
		t.Errorf("second point pods: want 7, got %d", report.Points[1].PodCount)
	}
}

func TestRenderTrend_TwoPoints_ShowsDeltas(t *testing.T) {
	t1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := t1.Add(24 * time.Hour)
	b1 := makeBundleWithMetrics(t1, "prod", 5, 3, 0, 0)
	b2 := makeBundleWithMetrics(t2, "prod", 7, 3, 2, 1)
	report := ComputeTrend([]*Bundle{b1, b2})
	out := RenderTrend(report)
	if !strings.Contains(out, "+2") {
		t.Errorf("expected +2 delta for pods, got: %q", out)
	}
	if !strings.Contains(out, "(=)") {
		t.Errorf("expected (=) for unchanged node count, got: %q", out)
	}
}

func TestRenderTrend_EmptyBundles_ReturnsFallback(t *testing.T) {
	out := RenderTrend(TrendReport{Points: nil})
	if !strings.Contains(out, "no snapshots") {
		t.Errorf("expected fallback message, got: %q", out)
	}
}

func TestRenderTrend_ContainsHeader(t *testing.T) {
	t1 := time.Now()
	t2 := t1.Add(time.Hour)
	b1 := makeBundleWithMetrics(t1, "prod", 1, 1, 0, 0)
	b2 := makeBundleWithMetrics(t2, "prod", 1, 1, 0, 0)
	report := ComputeTrend([]*Bundle{b1, b2})
	out := RenderTrend(report)
	if !strings.Contains(out, "Snapshot Trend") {
		t.Errorf("expected trend header, got: %q", out)
	}
}

func TestFormatDelta_Positive(t *testing.T) {
	got := formatDelta(10, 7)
	if !strings.Contains(got, "+3") {
		t.Errorf("expected +3 delta, got %q", got)
	}
}

func TestFormatDelta_Negative(t *testing.T) {
	got := formatDelta(4, 7)
	if !strings.Contains(got, "-3") {
		t.Errorf("expected -3 delta, got %q", got)
	}
}

func TestFormatDelta_Equal(t *testing.T) {
	got := formatDelta(5, 5)
	if !strings.Contains(got, "(=)") {
		t.Errorf("expected (=), got %q", got)
	}
}
