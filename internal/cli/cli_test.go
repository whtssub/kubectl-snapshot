package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/whtssub/kubectl-snapshot/internal/snapshot"
)

// writeTempBundle marshals bundle to a temp file and returns its path.
func writeTempBundle(t *testing.T, records []snapshot.Record) string {
	t.Helper()
	b := &snapshot.Bundle{
		Metadata: snapshot.Metadata{
			ToolVersion: "test",
			CapturedAt:  time.Now().UTC(),
			ClusterHint: "test-cluster",
		},
		Records: records,
	}
	data, err := json.Marshal(b)
	if err != nil {
		t.Fatalf("marshal bundle: %v", err)
	}
	f, err := os.CreateTemp(t.TempDir(), "snap-*.json")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	if _, err := f.Write(data); err != nil {
		t.Fatalf("write bundle: %v", err)
	}
	f.Close()
	return f.Name()
}

// runCmd executes the root command with args and returns stdout + error.
func runCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	root := NewRootCommand("0.2.0", "abc1234", "2026-04-27")
	root.SetOut(&out)
	root.SetErr(io.Discard)
	root.SilenceErrors = true
	root.SilenceUsage = true
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}

// --- version ---

func TestVersionCommand_PrintsVPrefix(t *testing.T) {
	out, err := runCmd(t, "version")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, "kubectl-snapshot v") {
		t.Errorf("expected v prefix, got: %q", out)
	}
}

func TestVersionCommand_ContainsCommitAndDate(t *testing.T) {
	out, err := runCmd(t, "version")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "abc1234") {
		t.Errorf("expected commit hash in output, got: %q", out)
	}
	if !strings.Contains(out, "2026-04-27") {
		t.Errorf("expected build date in output, got: %q", out)
	}
}

func TestRootVersionFlag_PrintsVPrefix(t *testing.T) {
	out, err := runCmd(t, "--version")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, "kubectl-snapshot v") {
		t.Errorf("expected v prefix from --version flag, got: %q", out)
	}
}

func TestVersionCommand_BareVersionInput_GetsVPrefixed(t *testing.T) {
	// Version string without leading 'v' should get one added.
	var out bytes.Buffer
	root := NewRootCommand("1.2.3", "deadbeef", "2026-01-01")
	root.SetOut(&out)
	root.SetErr(io.Discard)
	root.SilenceErrors = true
	root.SilenceUsage = true
	root.SetArgs([]string{"version"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "v1.2.3") {
		t.Errorf("expected v1.2.3 in output, got: %q", out.String())
	}
}

// --- capture ---

func TestCaptureCommand_MissingOutputFlag_ReturnsError(t *testing.T) {
	_, err := runCmd(t, "capture")
	if err == nil {
		t.Error("expected error when --output is not provided")
	}
}

// --- diff ---

func TestDiffCommand_WrongArgCount_ReturnsError(t *testing.T) {
	_, err := runCmd(t, "diff", "only-one.json")
	if err == nil {
		t.Error("expected error with wrong arg count for diff")
	}
}

func TestDiffCommand_BadFilePath_ReturnsError(t *testing.T) {
	_, err := runCmd(t, "diff", "/nonexistent/a.json", "/nonexistent/b.json")
	if err == nil {
		t.Error("expected error with nonexistent before-bundle")
	}
}

func TestDiffCommand_ValidBundles_ProducesReport(t *testing.T) {
	before := writeTempBundle(t, nil)
	after := writeTempBundle(t, nil)
	out, err := runCmd(t, "diff", before, after)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Snapshot Diff Report") {
		t.Errorf("expected diff report header, got: %q", out)
	}
}

func TestDiffCommand_MaxItemsFlag_Accepted(t *testing.T) {
	before := writeTempBundle(t, nil)
	after := writeTempBundle(t, nil)
	_, err := runCmd(t, "diff", "--max-items=5", before, after)
	if err != nil {
		t.Fatalf("--max-items flag rejected: %v", err)
	}
}

// --- analyze ---

func TestAnalyzeCommand_MissingArg_ReturnsError(t *testing.T) {
	_, err := runCmd(t, "analyze")
	if err == nil {
		t.Error("expected error when no snapshot path is provided")
	}
}

func TestAnalyzeCommand_BadFilePath_ReturnsError(t *testing.T) {
	_, err := runCmd(t, "analyze", "/nonexistent/snap.json")
	if err == nil {
		t.Error("expected error with nonexistent snapshot")
	}
}

func TestAnalyzeCommand_ValidBundle_ProducesReport(t *testing.T) {
	path := writeTempBundle(t, nil)
	out, err := runCmd(t, "analyze", path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Snapshot Incident Analysis") {
		t.Errorf("expected analysis report header, got: %q", out)
	}
}

func TestAnalyzeCommand_NoResourceMixFlag_HidesSection(t *testing.T) {
	path := writeTempBundle(t, []snapshot.Record{
		{Version: "v1", Resource: "pods", Name: "p", Object: map[string]any{}},
	})
	out, err := runCmd(t, "analyze", "--no-resource-mix", path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "RESOURCE MIX") {
		t.Error("--no-resource-mix should hide RESOURCE MIX section")
	}
}

func TestAnalyzeCommand_NoWarningEventsFlag_HidesSection(t *testing.T) {
	path := writeTempBundle(t, nil)
	out, err := runCmd(t, "analyze", "--no-warning-events", path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "WARNING EVENTS") {
		t.Error("--no-warning-events should hide WARNING EVENTS section")
	}
}

func TestAnalyzeCommand_SeverityThresholdFlag_FiltersLow(t *testing.T) {
	// Empty bundle → LOW severity; threshold=medium → filtered out.
	path := writeTempBundle(t, nil)
	out, err := runCmd(t, "analyze", "--severity-threshold=medium", path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "below configured severity threshold") {
		t.Errorf("expected threshold filter message, got: %q", out)
	}
}

func TestAnalyzeCommand_MaxItemsFlag_Accepted(t *testing.T) {
	path := writeTempBundle(t, nil)
	_, err := runCmd(t, "analyze", "--max-items=5", path)
	if err != nil {
		t.Fatalf("--max-items flag rejected: %v", err)
	}
}
