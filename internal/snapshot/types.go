package snapshot

import "time"

type Metadata struct {
	ToolVersion       string    `json:"toolVersion"`
	CapturedAt        time.Time `json:"capturedAt"`
	ClusterHint       string    `json:"clusterHint,omitempty"`
	CapturedResources []string  `json:"capturedResources,omitempty"`
	SkippedResources  []string  `json:"skippedResources,omitempty"`
}

type Record struct {
	Group     string `json:"group,omitempty"`
	Version   string `json:"version"`
	Resource  string `json:"resource"`
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name"`
	Object    any    `json:"object"`
}

type Bundle struct {
	Metadata Metadata `json:"metadata"`
	Records  []Record `json:"records"`
}
