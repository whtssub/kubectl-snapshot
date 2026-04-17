package snapshot

import (
	"testing"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

// --- canonicalizeMetadata ---

func TestCanonicalizeMetadata_KeepsAllowedFields(t *testing.T) {
	in := map[string]any{
		"name":              "my-pod",
		"namespace":         "default",
		"uid":               "abc-123",
		"creationTimestamp": "2024-01-01T00:00:00Z",
		"labels":            map[string]any{"app": "web"},
		"annotations":       map[string]any{"note": "val"},
	}
	out := canonicalizeMetadata(in)

	for _, key := range []string{"name", "namespace", "uid", "creationTimestamp", "labels", "annotations"} {
		if _, ok := out[key]; !ok {
			t.Errorf("expected key %q to be preserved, but it was missing", key)
		}
	}
}

func TestCanonicalizeMetadata_DropsDisallowedFields(t *testing.T) {
	in := map[string]any{
		"name":            "my-pod",
		"resourceVersion": "12345",
		"generation":      int64(3),
		"managedFields":   []any{"foo"},
		"ownerReferences": []any{"bar"},
		"finalizers":      []any{"baz"},
	}
	out := canonicalizeMetadata(in)

	for _, key := range []string{"resourceVersion", "generation", "managedFields", "ownerReferences", "finalizers"} {
		if _, ok := out[key]; ok {
			t.Errorf("expected key %q to be stripped, but it was present", key)
		}
	}
	if out["name"] != "my-pod" {
		t.Errorf("expected name to be preserved")
	}
}

func TestCanonicalizeMetadata_EmptyInput(t *testing.T) {
	out := canonicalizeMetadata(map[string]any{})
	if len(out) != 0 {
		t.Errorf("expected empty output for empty input, got %v", out)
	}
}

// --- canonicalize ---

func TestCanonicalize_RecursivelyProcessesNested(t *testing.T) {
	in := map[string]any{
		"metadata": map[string]any{
			"name":            "test",
			"resourceVersion": "999",
		},
		"spec": map[string]any{
			"containers": []any{
				map[string]any{"name": "c1", "image": "nginx"},
			},
		},
	}
	out := canonicalize(in)

	meta, ok := out["metadata"].(map[string]any)
	if !ok {
		t.Fatal("metadata should be a map")
	}
	if _, present := meta["resourceVersion"]; present {
		t.Error("resourceVersion should have been stripped from metadata")
	}
	if meta["name"] != "test" {
		t.Error("name should be preserved in metadata")
	}

	// spec is not metadata, so it should be preserved as-is (recursively)
	spec, ok := out["spec"].(map[string]any)
	if !ok {
		t.Fatal("spec should be a map")
	}
	containers, ok := spec["containers"].([]any)
	if !ok || len(containers) != 1 {
		t.Error("containers slice should be preserved")
	}
}

// --- redactSensitive ---

func TestRedactSensitive_RemovesDataAndBinaryData(t *testing.T) {
	obj := map[string]any{
		"metadata": map[string]any{"name": "my-secret"},
		"type":     "Opaque",
		"data": map[string]any{
			"password": "c2VjcmV0",
		},
		"binaryData": map[string]any{
			"cert": "AAAA",
		},
	}
	out := redactSensitive(obj)

	if _, ok := out["data"]; ok {
		t.Error("data should be removed by redactSensitive")
	}
	if _, ok := out["binaryData"]; ok {
		t.Error("binaryData should be removed by redactSensitive")
	}
	if out["type"] != "Opaque" {
		t.Error("type should be preserved")
	}
	if out["metadata"] == nil {
		t.Error("metadata should be preserved")
	}
}

func TestRedactSensitive_NoDataField_Passthrough(t *testing.T) {
	obj := map[string]any{
		"metadata": map[string]any{"name": "cm"},
		"type":     "kubernetes.io/service-account-token",
	}
	out := redactSensitive(obj)
	if len(out) != 2 {
		t.Errorf("expected 2 keys, got %d", len(out))
	}
}

// --- gvrKey ---

func TestGVRKey_CoreResource(t *testing.T) {
	gvr := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}
	if got := gvrKey(gvr); got != "v1/pods" {
		t.Errorf("expected v1/pods, got %q", got)
	}
}

func TestGVRKey_GroupedResource(t *testing.T) {
	gvr := schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}
	if got := gvrKey(gvr); got != "apps/v1/deployments" {
		t.Errorf("expected apps/v1/deployments, got %q", got)
	}
}

// --- allResources registry ---

func TestAllResources_NonEmpty(t *testing.T) {
	if len(allResources) == 0 {
		t.Fatal("allResources registry must not be empty")
	}
}

func TestAllResources_NodesIsClusterScoped(t *testing.T) {
	for _, rd := range allResources {
		if rd.GVR.Resource == "nodes" && !rd.ClusterScoped {
			t.Error("nodes must be ClusterScoped=true")
		}
	}
}

func TestAllResources_PersistentVolumesIsClusterScoped(t *testing.T) {
	found := false
	for _, rd := range allResources {
		if rd.GVR.Resource == "persistentvolumes" {
			found = true
			if !rd.ClusterScoped {
				t.Error("persistentvolumes must be ClusterScoped=true")
			}
		}
	}
	if !found {
		t.Error("persistentvolumes not found in allResources registry")
	}
}

func TestAllResources_ClusterRolesAreClusterScoped(t *testing.T) {
	for _, rd := range allResources {
		if rd.GVR.Resource == "clusterroles" || rd.GVR.Resource == "clusterrolebindings" {
			if !rd.ClusterScoped {
				t.Errorf("%s must be ClusterScoped=true", rd.GVR.Resource)
			}
		}
	}
}

func TestAllResources_SecretsAndConfigMapsAreRedacted(t *testing.T) {
	for _, rd := range allResources {
		if rd.GVR.Resource == "secrets" || rd.GVR.Resource == "configmaps" {
			if !rd.Redact {
				t.Errorf("%s must have Redact=true", rd.GVR.Resource)
			}
		}
	}
}

func TestAllResources_VPAIsOptional(t *testing.T) {
	found := false
	for _, rd := range allResources {
		if rd.GVR.Resource == "verticalpodautoscalers" {
			found = true
			if !rd.Optional {
				t.Error("verticalpodautoscalers must be Optional=true")
			}
		}
	}
	if !found {
		t.Error("verticalpodautoscalers not found in allResources registry")
	}
}

func TestAllResources_CoreWorkloadsPresent(t *testing.T) {
	required := []string{"pods", "deployments", "replicasets", "statefulsets", "daemonsets", "jobs", "cronjobs"}
	present := make(map[string]bool)
	for _, rd := range allResources {
		present[rd.GVR.Resource] = true
	}
	for _, r := range required {
		if !present[r] {
			t.Errorf("required resource %q not found in allResources registry", r)
		}
	}
}

func TestAllResources_NetworkingResourcesPresent(t *testing.T) {
	required := []string{"services", "endpoints", "ingresses", "networkpolicies"}
	present := make(map[string]bool)
	for _, rd := range allResources {
		present[rd.GVR.Resource] = true
	}
	for _, r := range required {
		if !present[r] {
			t.Errorf("required networking resource %q not found in allResources registry", r)
		}
	}
}

func TestAllResources_RBACResourcesPresent(t *testing.T) {
	required := []string{"serviceaccounts", "roles", "rolebindings", "clusterroles", "clusterrolebindings"}
	present := make(map[string]bool)
	for _, rd := range allResources {
		present[rd.GVR.Resource] = true
	}
	for _, r := range required {
		if !present[r] {
			t.Errorf("required RBAC resource %q not found in allResources registry", r)
		}
	}
}
