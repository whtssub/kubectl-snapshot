package snapshot

// SARIF 2.1.0 output for `analyze --output sarif`.
// GitHub Code Scanning ingests this format to surface cluster issues as
// security alerts on the repository that holds the snapshot file.
//
// Spec: https://docs.oasis-open.org/sarif/sarif/v2.1.0/sarif-v2.1.0.html

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ---- SARIF type definitions (minimal subset needed for Code Scanning) ----

type sarifLog struct {
	Schema  string     `json:"$schema"`
	Version string     `json:"version"`
	Runs    []sarifRun `json:"runs"`
}

type sarifRun struct {
	Tool    sarifTool     `json:"tool"`
	Results []sarifResult `json:"results"`
}

type sarifTool struct {
	Driver sarifDriver `json:"driver"`
}

type sarifDriver struct {
	Name           string      `json:"name"`
	Version        string      `json:"version"`
	InformationURI string      `json:"informationUri"`
	Rules          []sarifRule `json:"rules"`
}

type sarifRule struct {
	ID               string              `json:"id"`
	Name             string              `json:"name"`
	ShortDescription sarifMessage        `json:"shortDescription"`
	FullDescription  sarifMessage        `json:"fullDescription"`
	DefaultConfig    sarifDefaultConfig  `json:"defaultConfiguration"`
	Properties       sarifRuleProperties `json:"properties"`
}

type sarifDefaultConfig struct {
	Level string `json:"level"` // "error" | "warning" | "note"
}

type sarifRuleProperties struct {
	Tags []string `json:"tags"`
}

type sarifResult struct {
	RuleID    string          `json:"ruleId"`
	Level     string          `json:"level"`
	Message   sarifMessage    `json:"message"`
	Locations []sarifLocation `json:"locations"`
}

type sarifMessage struct {
	Text string `json:"text"`
}

type sarifLocation struct {
	PhysicalLocation sarifPhysicalLocation `json:"physicalLocation"`
	LogicalLocations []sarifLogicalLocation `json:"logicalLocations,omitempty"`
}

type sarifPhysicalLocation struct {
	ArtifactLocation sarifArtifactLocation `json:"artifactLocation"`
}

type sarifArtifactLocation struct {
	URI       string `json:"uri"`
	URIBaseID string `json:"uriBaseId"`
}

type sarifLogicalLocation struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
}

// ---- Rule catalogue ----

var sarifRules = []sarifRule{
	{
		ID:               "snapshot/pod-issue",
		Name:             "PodIssue",
		ShortDescription: sarifMessage{Text: "Pod health issue detected"},
		FullDescription:  sarifMessage{Text: "A pod is in a failed, pending, crash-looping, or OOMKilled state."},
		DefaultConfig:    sarifDefaultConfig{Level: "error"},
		Properties:       sarifRuleProperties{Tags: []string{"kubernetes", "pod"}},
	},
	{
		ID:               "snapshot/node-issue",
		Name:             "NodeIssue",
		ShortDescription: sarifMessage{Text: "Node pressure or unavailability detected"},
		FullDescription:  sarifMessage{Text: "A node is reporting MemoryPressure, DiskPressure, PIDPressure, NetworkUnavailable, or is not Ready."},
		DefaultConfig:    sarifDefaultConfig{Level: "error"},
		Properties:       sarifRuleProperties{Tags: []string{"kubernetes", "node"}},
	},
	{
		ID:               "snapshot/workload-issue",
		Name:             "WorkloadIssue",
		ShortDescription: sarifMessage{Text: "Workload issue detected"},
		FullDescription:  sarifMessage{Text: "A Deployment, StatefulSet, DaemonSet, ReplicaSet, HPA, Job, CronJob, Ingress, or NetworkPolicy has a configuration or runtime problem."},
		DefaultConfig:    sarifDefaultConfig{Level: "warning"},
		Properties:       sarifRuleProperties{Tags: []string{"kubernetes", "workload"}},
	},
	{
		ID:               "snapshot/storage-issue",
		Name:             "StorageIssue",
		ShortDescription: sarifMessage{Text: "Storage issue detected"},
		FullDescription:  sarifMessage{Text: "A PersistentVolumeClaim or PersistentVolume is not in the expected bound state."},
		DefaultConfig:    sarifDefaultConfig{Level: "warning"},
		Properties:       sarifRuleProperties{Tags: []string{"kubernetes", "storage"}},
	},
	{
		ID:               "snapshot/warning-event",
		Name:             "WarningEvent",
		ShortDescription: sarifMessage{Text: "Kubernetes warning event"},
		FullDescription:  sarifMessage{Text: "The cluster emitted a Warning-level event for a resource."},
		DefaultConfig:    sarifDefaultConfig{Level: "note"},
		Properties:       sarifRuleProperties{Tags: []string{"kubernetes", "event"}},
	},
}

// ---- Renderer ----

func renderSARIF(bundle *Bundle, a *analysis, severity string, opts AnalyzeOptions) (string, error) {
	// Map overall severity to SARIF result levels.
	defaultLevel := sarifLevelForSeverity(severity)

	results := make([]sarifResult, 0, len(a.podIssues)+len(a.nodeIssues)+len(a.workloadIssues)+len(a.storageIssues)+len(a.warningEvents))

	artifactURI := defaultIfEmpty(bundle.Metadata.ClusterHint, "cluster")

	appendResults := func(issues []string, ruleID, level string) {
		for _, msg := range issues {
			resource := extractResourceFromIssue(msg)
			loc := sarifLocation{
				PhysicalLocation: sarifPhysicalLocation{
					ArtifactLocation: sarifArtifactLocation{
						URI:       "snapshot.json",
						URIBaseID: "%SRCROOT%",
					},
				},
			}
			if resource != "" {
				loc.LogicalLocations = []sarifLogicalLocation{
					{Name: fmt.Sprintf("%s/%s", artifactURI, resource), Kind: "resource"},
				}
			}
			results = append(results, sarifResult{
				RuleID:    ruleID,
				Level:     level,
				Message:   sarifMessage{Text: msg},
				Locations: []sarifLocation{loc},
			})
		}
	}

	appendResults(a.podIssues, "snapshot/pod-issue", "error")
	appendResults(a.nodeIssues, "snapshot/node-issue", "error")
	appendResults(a.workloadIssues, "snapshot/workload-issue", defaultLevel)
	appendResults(a.storageIssues, "snapshot/storage-issue", defaultLevel)
	if !opts.HideWarningEvents {
		appendResults(a.warningEvents, "snapshot/warning-event", "note")
	}

	log := sarifLog{
		Schema:  "https://schemastore.schemastore.org/schemas/json/sarif-2.1.0.json",
		Version: "2.1.0",
		Runs: []sarifRun{
			{
				Tool: sarifTool{
					Driver: sarifDriver{
						Name:           "kubectl-snapshot",
						Version:        bundle.Metadata.ToolVersion,
						InformationURI: "https://github.com/whtssub/kubectl-snapshot",
						Rules:          sarifRules,
					},
				},
				Results: results,
			},
		},
	}

	b, err := json.MarshalIndent(log, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal SARIF: %w", err)
	}
	return string(b) + "\n", nil
}

// sarifLevelForSeverity maps an incident severity to a SARIF level for
// workload/storage issues (pod/node issues are always "error").
func sarifLevelForSeverity(severity string) string {
	switch strings.ToUpper(severity) {
	case "HIGH":
		return "error"
	case "MEDIUM":
		return "warning"
	default:
		return "note"
	}
}

// extractResourceFromIssue attempts to pull the namespace/name portion out of
// a human-readable issue string like "[CRASHLOOP] sre-lab/api container=app".
// Returns an empty string when the format is not recognisable.
func extractResourceFromIssue(msg string) string {
	// Strip leading bracketed tag like "[CRASHLOOP] "
	s := msg
	if idx := strings.Index(s, "] "); idx != -1 {
		s = strings.TrimSpace(s[idx+2:])
	}
	// The resource is the first whitespace-delimited token
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}
