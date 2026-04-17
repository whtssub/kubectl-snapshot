package snapshot

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

// ResourceDescriptor describes a Kubernetes resource type to capture.
type ResourceDescriptor struct {
	GVR           schema.GroupVersionResource
	ClusterScoped bool // true for nodes, PVs, ClusterRoles, etc.
	Redact        bool // strip .data/.binaryData before storing (Secrets, ConfigMaps)
	Optional      bool // skip gracefully if API server returns 404/403 (CRDs, optional APIs)
}

// allResources is the canonical registry of resource types to capture.
var allResources = []ResourceDescriptor{
	// Core workloads
	{GVR: schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}},
	{GVR: schema.GroupVersionResource{Group: "", Version: "v1", Resource: "events"}},
	{GVR: schema.GroupVersionResource{Group: "", Version: "v1", Resource: "nodes"}, ClusterScoped: true},
	{GVR: schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}},
	{GVR: schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "replicasets"}},
	{GVR: schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "statefulsets"}},
	{GVR: schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "daemonsets"}},
	{GVR: schema.GroupVersionResource{Group: "batch", Version: "v1", Resource: "jobs"}},
	{GVR: schema.GroupVersionResource{Group: "batch", Version: "v1", Resource: "cronjobs"}},

	// Networking
	{GVR: schema.GroupVersionResource{Group: "", Version: "v1", Resource: "services"}},
	{GVR: schema.GroupVersionResource{Group: "", Version: "v1", Resource: "endpoints"}},
	{GVR: schema.GroupVersionResource{Group: "networking.k8s.io", Version: "v1", Resource: "ingresses"}},
	{GVR: schema.GroupVersionResource{Group: "networking.k8s.io", Version: "v1", Resource: "networkpolicies"}},

	// Storage
	{GVR: schema.GroupVersionResource{Group: "", Version: "v1", Resource: "persistentvolumeclaims"}},
	{GVR: schema.GroupVersionResource{Group: "", Version: "v1", Resource: "persistentvolumes"}, ClusterScoped: true},

	// Config (redacted — metadata only, no values)
	{GVR: schema.GroupVersionResource{Group: "", Version: "v1", Resource: "configmaps"}, Redact: true},
	{GVR: schema.GroupVersionResource{Group: "", Version: "v1", Resource: "secrets"}, Redact: true},

	// RBAC
	{GVR: schema.GroupVersionResource{Group: "", Version: "v1", Resource: "serviceaccounts"}},
	{GVR: schema.GroupVersionResource{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "roles"}},
	{GVR: schema.GroupVersionResource{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "rolebindings"}},
	{GVR: schema.GroupVersionResource{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "clusterroles"}, ClusterScoped: true},
	{GVR: schema.GroupVersionResource{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "clusterrolebindings"}, ClusterScoped: true},

	// Autoscaling
	{GVR: schema.GroupVersionResource{Group: "autoscaling", Version: "v2", Resource: "horizontalpodautoscalers"}},
	// VPA is optional — only present if the VPA operator is installed
	{GVR: schema.GroupVersionResource{Group: "autoscaling.k8s.io", Version: "v1", Resource: "verticalpodautoscalers"}, Optional: true},
}

// shortNames maps common kubectl-style aliases to their ResourceDescriptor.
// Keys are lowercase; lookup is always lowercased.
var shortNames = func() map[string]ResourceDescriptor {
	m := make(map[string]ResourceDescriptor, len(allResources)*2)
	for _, rd := range allResources {
		m[rd.GVR.Resource] = rd
	}
	// Common aliases
	aliases := map[string]string{
		"po":     "pods",
		"ev":     "events",
		"no":     "nodes",
		"deploy": "deployments",
		"rs":     "replicasets",
		"sts":    "statefulsets",
		"ds":     "daemonsets",
		"job":    "jobs",
		"cj":     "cronjobs",
		"svc":    "services",
		"ep":     "endpoints",
		"ing":    "ingresses",
		"netpol": "networkpolicies",
		"pvc":    "persistentvolumeclaims",
		"pv":     "persistentvolumes",
		"cm":     "configmaps",
		"sa":     "serviceaccounts",
		"hpa":    "horizontalpodautoscalers",
		"vpa":    "verticalpodautoscalers",
	}
	for alias, resource := range aliases {
		if rd, ok := m[resource]; ok {
			m[alias] = rd
		}
	}
	return m
}()

// CaptureOptions controls which resources are captured.
type CaptureOptions struct {
	// Resources is an optional list of resource selectors. Each entry is either:
	//   - a short name or plural name  ("pods", "deploy", "cm")
	//   - a fully-qualified group/version/resource triple  ("apps/v1/deployments")
	// When empty, all resources in allResources are captured (default behaviour).
	Resources []string
}

// resolveResources returns the set of ResourceDescriptors to capture based on opts.
// Unknown short names are skipped with a warning string returned alongside.
func resolveResources(opts CaptureOptions) ([]ResourceDescriptor, []string) {
	if len(opts.Resources) == 0 {
		return allResources, nil
	}

	// Track by resolved GVR key so aliases that resolve to the same resource
	// (e.g. "pods" and "po") are deduplicated correctly.
	seen := make(map[string]bool)
	out := make([]ResourceDescriptor, 0, len(opts.Resources))
	var unknown []string

	for _, raw := range opts.Resources {
		key := strings.ToLower(strings.TrimSpace(raw))
		if key == "" {
			continue
		}

		// Try registry lookup first (short names and full plural names)
		if rd, ok := shortNames[key]; ok {
			resolved := gvrKey(rd.GVR)
			if seen[resolved] {
				continue
			}
			seen[resolved] = true
			out = append(out, rd)
			continue
		}

		// Try parsing as group/version/resource triple
		if rd, ok := parseGVRTriple(key); ok {
			resolved := gvrKey(rd.GVR)
			if seen[resolved] {
				continue
			}
			seen[resolved] = true
			out = append(out, rd)
			continue
		}

		unknown = append(unknown, raw)
	}
	return out, unknown
}

// parseGVRTriple parses "group/version/resource" or "version/resource" (core API)
// into a ResourceDescriptor. The resulting descriptor is Optional=true since we
// cannot know at parse time whether the resource exists in the target cluster.
func parseGVRTriple(s string) (ResourceDescriptor, bool) {
	parts := strings.Split(s, "/")
	switch len(parts) {
	case 2:
		// version/resource — core API (no group)
		return ResourceDescriptor{
			GVR:      schema.GroupVersionResource{Group: "", Version: parts[0], Resource: parts[1]},
			Optional: true,
		}, true
	case 3:
		// group/version/resource
		return ResourceDescriptor{
			GVR:      schema.GroupVersionResource{Group: parts[0], Version: parts[1], Resource: parts[2]},
			Optional: true,
		}, true
	}
	return ResourceDescriptor{}, false
}

func Capture(ctx context.Context, client dynamic.Interface, namespace string, clusterHint string, opts CaptureOptions) (*Bundle, error) {
	descriptors, unknowns := resolveResources(opts)

	records := make([]Record, 0, 256)
	captured := make([]string, 0, len(descriptors))
	skipped := make([]string, 0)

	for _, rd := range descriptors {
		gvr := rd.GVR
		list, err := listResources(ctx, client, rd, namespace)
		if err != nil {
			if rd.Optional && (kerrors.IsNotFound(err) || kerrors.IsForbidden(err) || kerrors.IsMethodNotSupported(err)) {
				skipped = append(skipped, gvrKey(gvr))
				continue
			}
			return nil, fmt.Errorf("list %s: %w", gvrKey(gvr), err)
		}

		captured = append(captured, gvrKey(gvr))

		for i := range list.Items {
			item := list.Items[i]
			obj := canonicalize(item.Object)
			if rd.Redact {
				obj = redactSensitive(obj)
			}

			records = append(records, Record{
				Group:     gvr.Group,
				Version:   gvr.Version,
				Resource:  gvr.Resource,
				Namespace: item.GetNamespace(),
				Name:      item.GetName(),
				Object:    obj,
			})
		}
	}

	// Append unknown selectors to skipped so the caller can surface them
	for _, u := range unknowns {
		skipped = append(skipped, "unknown:"+u)
	}

	sort.Slice(records, func(i, j int) bool {
		a, b := records[i], records[j]
		if a.Group != b.Group {
			return a.Group < b.Group
		}
		if a.Version != b.Version {
			return a.Version < b.Version
		}
		if a.Resource != b.Resource {
			return a.Resource < b.Resource
		}
		if a.Namespace != b.Namespace {
			return a.Namespace < b.Namespace
		}
		return a.Name < b.Name
	})

	return &Bundle{
		Metadata: Metadata{
			ToolVersion:       "0.2.0",
			CapturedAt:        time.Now().UTC(),
			ClusterHint:       clusterHint,
			CapturedResources: captured,
			SkippedResources:  skipped,
		},
		Records: records,
	}, nil
}

func listResources(ctx context.Context, client dynamic.Interface, rd ResourceDescriptor, namespace string) (*unstructured.UnstructuredList, error) {
	if rd.ClusterScoped || namespace == "" {
		return client.Resource(rd.GVR).List(ctx, metav1.ListOptions{})
	}
	return client.Resource(rd.GVR).Namespace(namespace).List(ctx, metav1.ListOptions{})
}

// gvrKey returns a human-readable key for a GVR, e.g. "apps/v1/deployments".
func gvrKey(gvr schema.GroupVersionResource) string {
	if gvr.Group == "" {
		return fmt.Sprintf("v1/%s", gvr.Resource)
	}
	return fmt.Sprintf("%s/%s/%s", gvr.Group, gvr.Version, gvr.Resource)
}

func canonicalize(in map[string]any) map[string]any {
	clone := make(map[string]any, len(in))
	for k, v := range in {
		if k == "metadata" {
			if meta, ok := v.(map[string]any); ok {
				clone[k] = canonicalizeMetadata(meta)
				continue
			}
		}
		clone[k] = canonicalValue(v)
	}
	return clone
}

func canonicalizeMetadata(meta map[string]any) map[string]any {
	out := map[string]any{}
	keep := []string{"name", "namespace", "labels", "annotations", "uid", "creationTimestamp"}
	for _, key := range keep {
		if val, ok := meta[key]; ok {
			out[key] = canonicalValue(val)
		}
	}
	return out
}

func canonicalValue(v any) any {
	switch tv := v.(type) {
	case map[string]any:
		return canonicalize(tv)
	case []any:
		out := make([]any, 0, len(tv))
		for _, item := range tv {
			out = append(out, canonicalValue(item))
		}
		return out
	default:
		return tv
	}
}

// redactSensitive removes .data and .binaryData from the object so sensitive
// values (Secret contents, ConfigMap values) never reach disk.
func redactSensitive(obj map[string]any) map[string]any {
	out := make(map[string]any, len(obj))
	for k, v := range obj {
		if k == "data" || k == "binaryData" {
			continue
		}
		out[k] = v
	}
	return out
}

func MarshalDeterministic(v any) ([]byte, error) {
	// encoding/json sorts map keys, which is enough for stable outputs here.
	return json.MarshalIndent(v, "", "  ")
}
