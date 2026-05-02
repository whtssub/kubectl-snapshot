package snapshot

import (
	"testing"
)

// --- resolveResources: default (empty) ---

func TestResolveResources_EmptyOptions_ReturnsAllResources(t *testing.T) {
	got, unknown := resolveResources(CaptureOptions{})
	if len(unknown) != 0 {
		t.Errorf("expected no unknowns, got %v", unknown)
	}
	if len(got) != len(allResources) {
		t.Errorf("expected %d resources (all), got %d", len(allResources), len(got))
	}
}

// --- resolveResources: plural names ---

func TestResolveResources_PluralName_Pods(t *testing.T) {
	got, unknown := resolveResources(CaptureOptions{Resources: []string{"pods"}})
	if len(unknown) != 0 {
		t.Fatalf("unexpected unknowns: %v", unknown)
	}
	if len(got) != 1 || got[0].GVR.Resource != "pods" {
		t.Errorf("expected pods descriptor, got %+v", got)
	}
}

func TestResolveResources_MultiplePluralNames(t *testing.T) {
	got, unknown := resolveResources(CaptureOptions{Resources: []string{"pods", "deployments", "services"}})
	if len(unknown) != 0 {
		t.Fatalf("unexpected unknowns: %v", unknown)
	}
	if len(got) != 3 {
		t.Errorf("expected 3 descriptors, got %d", len(got))
	}
	resources := make(map[string]bool)
	for _, rd := range got {
		resources[rd.GVR.Resource] = true
	}
	for _, r := range []string{"pods", "deployments", "services"} {
		if !resources[r] {
			t.Errorf("expected %s to be in resolved set", r)
		}
	}
}

// --- resolveResources: short-name aliases ---

func TestResolveResources_ShortAlias_Deploy(t *testing.T) {
	got, unknown := resolveResources(CaptureOptions{Resources: []string{"deploy"}})
	if len(unknown) != 0 {
		t.Fatalf("unexpected unknowns: %v", unknown)
	}
	if len(got) != 1 || got[0].GVR.Resource != "deployments" {
		t.Errorf("expected deployments via 'deploy' alias, got %+v", got)
	}
}

func TestResolveResources_ShortAlias_CM(t *testing.T) {
	got, unknown := resolveResources(CaptureOptions{Resources: []string{"cm"}})
	if len(unknown) != 0 {
		t.Fatalf("unexpected unknowns: %v", unknown)
	}
	if len(got) != 1 || got[0].GVR.Resource != "configmaps" {
		t.Errorf("expected configmaps via 'cm' alias, got %+v", got)
	}
	if !got[0].Redact {
		t.Error("configmaps resolved via alias should have Redact=true")
	}
}

func TestResolveResources_ShortAlias_STS(t *testing.T) {
	got, unknown := resolveResources(CaptureOptions{Resources: []string{"sts"}})
	if len(unknown) != 0 {
		t.Fatalf("unexpected unknowns: %v", unknown)
	}
	if len(got) != 1 || got[0].GVR.Resource != "statefulsets" {
		t.Errorf("expected statefulsets via 'sts' alias, got %+v", got)
	}
}

func TestResolveResources_ShortAlias_PVC(t *testing.T) {
	got, unknown := resolveResources(CaptureOptions{Resources: []string{"pvc"}})
	if len(unknown) != 0 {
		t.Fatalf("unexpected unknowns: %v", unknown)
	}
	if len(got) != 1 || got[0].GVR.Resource != "persistentvolumeclaims" {
		t.Errorf("expected persistentvolumeclaims via 'pvc' alias, got %+v", got)
	}
}

func TestResolveResources_ShortAlias_HPA(t *testing.T) {
	got, unknown := resolveResources(CaptureOptions{Resources: []string{"hpa"}})
	if len(unknown) != 0 {
		t.Fatalf("unexpected unknowns: %v", unknown)
	}
	if len(got) != 1 || got[0].GVR.Resource != "horizontalpodautoscalers" {
		t.Errorf("expected horizontalpodautoscalers via 'hpa' alias, got %+v", got)
	}
}

// --- resolveResources: case insensitivity ---

func TestResolveResources_CaseInsensitive(t *testing.T) {
	got, unknown := resolveResources(CaptureOptions{Resources: []string{"Pods", "DEPLOYMENTS", "SVC"}})
	if len(unknown) != 0 {
		t.Fatalf("unexpected unknowns: %v", unknown)
	}
	if len(got) != 3 {
		t.Errorf("expected 3 results, got %d: %+v", len(got), got)
	}
}

// --- resolveResources: deduplication ---

func TestResolveResources_Deduplication(t *testing.T) {
	// "pods" and "po" both resolve to the same resource
	got, unknown := resolveResources(CaptureOptions{Resources: []string{"pods", "po", "pods"}})
	if len(unknown) != 0 {
		t.Fatalf("unexpected unknowns: %v", unknown)
	}
	if len(got) != 1 {
		t.Errorf("expected 1 deduplicated result, got %d", len(got))
	}
}

// --- resolveResources: unknown names ---

func TestResolveResources_UnknownName_ReportedAsUnknown(t *testing.T) {
	got, unknown := resolveResources(CaptureOptions{Resources: []string{"pods", "doesnotexist"}})
	if len(unknown) != 1 || unknown[0] != "doesnotexist" {
		t.Errorf("expected ['doesnotexist'] as unknown, got %v", unknown)
	}
	if len(got) != 1 || got[0].GVR.Resource != "pods" {
		t.Errorf("known resource should still be included, got %+v", got)
	}
}

func TestResolveResources_AllUnknown(t *testing.T) {
	got, unknown := resolveResources(CaptureOptions{Resources: []string{"foo", "bar"}})
	if len(got) != 0 {
		t.Errorf("expected empty descriptor list, got %+v", got)
	}
	if len(unknown) != 2 {
		t.Errorf("expected 2 unknowns, got %v", unknown)
	}
}

func TestResolveResources_EmptyStringEntries_Ignored(t *testing.T) {
	got, unknown := resolveResources(CaptureOptions{Resources: []string{"pods", "", "  "}})
	if len(unknown) != 0 {
		t.Errorf("empty strings should not become unknowns, got %v", unknown)
	}
	if len(got) != 1 {
		t.Errorf("expected 1 result, got %d", len(got))
	}
}

// --- parseGVRTriple ---

func TestParseGVRTriple_ThreePart(t *testing.T) {
	rd, ok := parseGVRTriple("apps/v1/deployments")
	if !ok {
		t.Fatal("expected successful parse")
	}
	if rd.GVR.Group != "apps" || rd.GVR.Version != "v1" || rd.GVR.Resource != "deployments" {
		t.Errorf("unexpected GVR: %+v", rd.GVR)
	}
	if !rd.Optional {
		t.Error("parsed GVR triple should be Optional=true")
	}
}

func TestParseGVRTriple_TwoPart_CoreAPI(t *testing.T) {
	rd, ok := parseGVRTriple("v1/pods")
	if !ok {
		t.Fatal("expected successful parse")
	}
	if rd.GVR.Group != "" || rd.GVR.Version != "v1" || rd.GVR.Resource != "pods" {
		t.Errorf("unexpected GVR: %+v", rd.GVR)
	}
	if !rd.Optional {
		t.Error("parsed GVR triple should be Optional=true")
	}
}

func TestParseGVRTriple_CustomCRD(t *testing.T) {
	rd, ok := parseGVRTriple("myapp.io/v1beta1/widgets")
	if !ok {
		t.Fatal("expected successful parse")
	}
	if rd.GVR.Group != "myapp.io" || rd.GVR.Version != "v1beta1" || rd.GVR.Resource != "widgets" {
		t.Errorf("unexpected GVR: %+v", rd.GVR)
	}
}

func TestParseGVRTriple_SingleWord_Fails(t *testing.T) {
	_, ok := parseGVRTriple("pods")
	if ok {
		t.Error("single word should not parse as GVR triple")
	}
}

func TestParseGVRTriple_FourParts_Fails(t *testing.T) {
	_, ok := parseGVRTriple("a/b/c/d")
	if ok {
		t.Error("four-part string should not parse as GVR triple")
	}
}

// --- resolveResources: GVR triple as input ---

func TestResolveResources_GVRTriple_Recognized(t *testing.T) {
	got, unknown := resolveResources(CaptureOptions{Resources: []string{"myapp.io/v1/widgets"}})
	if len(unknown) != 0 {
		t.Fatalf("unexpected unknowns: %v", unknown)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 descriptor, got %d", len(got))
	}
	if got[0].GVR.Group != "myapp.io" || got[0].GVR.Resource != "widgets" {
		t.Errorf("unexpected GVR: %+v", got[0].GVR)
	}
	if !got[0].Optional {
		t.Error("CRD triple should have Optional=true")
	}
}

func TestResolveResources_CoreGVRTriple_Recognized(t *testing.T) {
	got, unknown := resolveResources(CaptureOptions{Resources: []string{"v1/configmaps"}})
	if len(unknown) != 0 {
		t.Fatalf("unexpected unknowns: %v", unknown)
	}
	if len(got) != 1 || got[0].GVR.Resource != "configmaps" {
		t.Errorf("expected configmaps, got %+v", got)
	}
}

// --- shortNames registry completeness ---

func TestShortNames_AllAliasesResolveToKnownResources(t *testing.T) {
	known := make(map[string]bool)
	for _, rd := range allResources {
		known[rd.GVR.Resource] = true
	}
	// Every value in shortNames must resolve to a known resource
	for alias, rd := range shortNames {
		if !known[rd.GVR.Resource] {
			t.Errorf("alias %q resolves to unknown resource %q", alias, rd.GVR.Resource)
		}
	}
}

func TestShortNames_CommonAliasesPresent(t *testing.T) {
	required := []string{"po", "deploy", "sts", "ds", "svc", "ep", "cm", "sa", "pvc", "pv", "hpa", "ing"}
	for _, alias := range required {
		if _, ok := shortNames[alias]; !ok {
			t.Errorf("expected alias %q to be present in shortNames", alias)
		}
	}
}

// --- LabelSelector field ---

func TestCaptureOptions_LabelSelector_Preserved(t *testing.T) {
	opts := CaptureOptions{LabelSelector: "app=frontend"}
	if opts.LabelSelector != "app=frontend" {
		t.Errorf("expected label selector to be preserved, got %q", opts.LabelSelector)
	}
}

func TestCaptureOptions_LabelSelector_EmptyByDefault(t *testing.T) {
	opts := CaptureOptions{}
	if opts.LabelSelector != "" {
		t.Errorf("expected empty label selector by default, got %q", opts.LabelSelector)
	}
}
