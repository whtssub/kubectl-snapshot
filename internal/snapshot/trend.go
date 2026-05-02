package snapshot

import (
	"fmt"
	"strings"
	"time"
)

// TrendPoint summarises a single snapshot for trend comparison.
type TrendPoint struct {
	Path              string
	CapturedAt        time.Time
	Cluster           string
	PodCount          int
	NodeCount         int
	RestartTotal      int
	WarningEventCount int
}

// TrendReport holds the ordered sequence of data points for rendering.
type TrendReport struct {
	Points []TrendPoint
}

// ComputeTrend extracts a TrendPoint from each bundle in order.
func ComputeTrend(bundles []*Bundle) TrendReport {
	points := make([]TrendPoint, 0, len(bundles))
	for _, b := range bundles {
		tp := TrendPoint{
			CapturedAt: b.Metadata.CapturedAt,
			Cluster:    b.Metadata.ClusterHint,
		}
		for _, r := range b.Records {
			obj, _ := r.Object.(map[string]any)
			switch r.Resource {
			case "pods":
				tp.PodCount++
				if obj != nil {
					status := getMap(obj, "status")
					for _, cs := range getSlice(status, "containerStatuses") {
						c, ok := cs.(map[string]any)
						if ok {
							tp.RestartTotal += getInt(c, "restartCount")
						}
					}
				}
			case "nodes":
				tp.NodeCount++
			case "events":
				if obj != nil && strings.ToUpper(getString(obj, "type")) == "WARNING" {
					tp.WarningEventCount++
				}
			}
		}
		points = append(points, tp)
	}
	return TrendReport{Points: points}
}

// RenderTrend produces a human-readable table showing how key metrics changed
// across the given snapshots. Deltas are shown relative to the previous point.
func RenderTrend(report TrendReport) string {
	if len(report.Points) == 0 {
		return "no snapshots to compare\n"
	}
	if len(report.Points) == 1 {
		return "trend requires at least 2 snapshots; showing summary for the single provided snapshot\n" +
			renderTrendTable(report)
	}
	return renderTrendTable(report)
}

func renderTrendTable(report TrendReport) string {
	var sb strings.Builder
	sb.WriteString(clr(ansiBold, "📈 Snapshot Trend"))
	sb.WriteByte('\n')
	sb.WriteString(clr(ansiDim, "══════════════════════════════════════════════════════════════════════════════════════"))
	sb.WriteByte('\n')
	sb.WriteString(fmt.Sprintf("%-5s  %-22s  %-18s  %10s  %7s  %10s  %10s\n",
		"#", "Captured At", "Cluster", "Pods", "Nodes", "Restarts", "Warnings"))
	sb.WriteString(strings.Repeat("─", 92))
	sb.WriteByte('\n')

	for i, p := range report.Points {
		cluster := p.Cluster
		if cluster == "" {
			cluster = "(unknown)"
		}
		if len(cluster) > 18 {
			cluster = cluster[:15] + "..."
		}
		capturedAt := p.CapturedAt.Format("2006-01-02 15:04:05")

		var podStr, nodeStr, restartStr, warnStr string
		if i == 0 {
			podStr = fmt.Sprintf("%d", p.PodCount)
			nodeStr = fmt.Sprintf("%d", p.NodeCount)
			restartStr = fmt.Sprintf("%d", p.RestartTotal)
			warnStr = fmt.Sprintf("%d", p.WarningEventCount)
		} else {
			prev := report.Points[i-1]
			podStr = formatDelta(p.PodCount, prev.PodCount)
			nodeStr = formatDelta(p.NodeCount, prev.NodeCount)
			restartStr = formatDelta(p.RestartTotal, prev.RestartTotal)
			warnStr = formatDelta(p.WarningEventCount, prev.WarningEventCount)
		}

		sb.WriteString(fmt.Sprintf("%-5d  %-22s  %-18s  %10s  %7s  %10s  %10s\n",
			i+1, capturedAt, cluster, podStr, nodeStr, restartStr, warnStr))
	}
	return sb.String()
}

func formatDelta(current, prev int) string {
	delta := current - prev
	switch {
	case delta > 0:
		return fmt.Sprintf("%d (+%d)", current, delta)
	case delta < 0:
		return fmt.Sprintf("%d (%d)", current, delta)
	default:
		return fmt.Sprintf("%d (=)", current)
	}
}
