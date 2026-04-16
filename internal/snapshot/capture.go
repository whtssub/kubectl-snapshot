package snapshot

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

var defaultGVRs = []schema.GroupVersionResource{
	{Group: "", Version: "v1", Resource: "pods"},
	{Group: "", Version: "v1", Resource: "events"},
	{Group: "", Version: "v1", Resource: "nodes"},
}

func Capture(ctx context.Context, client dynamic.Interface, namespace string, clusterHint string) (*Bundle, error) {
	records := make([]Record, 0, 128)
	for _, gvr := range defaultGVRs {
		list, err := listResources(ctx, client, gvr, namespace)
		if err != nil {
			return nil, fmt.Errorf("list %s.%s/%s: %w", gvr.Resource, gvr.Group, gvr.Version, err)
		}

		for i := range list.Items {
			item := list.Items[i]
			item.Object = canonicalize(item.Object)

			records = append(records, Record{
				Group:     gvr.Group,
				Version:   gvr.Version,
				Resource:  gvr.Resource,
				Namespace: item.GetNamespace(),
				Name:      item.GetName(),
				Object:    item.Object,
			})
		}
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
			ToolVersion: "0.1.0",
			CapturedAt:  time.Now().UTC(),
			ClusterHint: clusterHint,
		},
		Records: records,
	}, nil
}

func listResources(ctx context.Context, client dynamic.Interface, gvr schema.GroupVersionResource, namespace string) (*unstructured.UnstructuredList, error) {
	if namespace == "" || gvr.Resource == "nodes" {
		return client.Resource(gvr).List(ctx, metav1.ListOptions{})
	}
	return client.Resource(gvr).Namespace(namespace).List(ctx, metav1.ListOptions{})
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

func MarshalDeterministic(v any) ([]byte, error) {
	// encoding/json sorts map keys, which is enough for stable outputs here.
	return json.MarshalIndent(v, "", "  ")
}
