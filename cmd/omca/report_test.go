package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wangzitian0/oh-my-code-agent/internal/report"
)

// writeCodexMCPCollision writes a real, colliding "shared" MCP server
// definition into both codex's user-scope config.toml (env.HomeDir/.codex,
// since setupManagedTestEnv leaves CODEX_HOME unset) and the project's
// workspace-scope .codex/config.toml — a genuine multi-source collision
// internal/effective's real resolver (against the real, committed codex
// Knowledge Pack, whose resolve capability for mcp_server is honestly
// UNKNOWN) must leave as a Conflict, giving every drift/explain/matrix CLI
// test in this package real end-to-end SOURCE_DRIFT data to query, not a
// hand-built report.Artifact fixture.
func writeCodexMCPCollision(t *testing.T, env managedTestEnv) {
	t.Helper()
	userDir := filepath.Join(env.HomeDir, ".codex")
	if err := os.MkdirAll(userDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(userDir, "config.toml"), []byte("[mcp_servers.shared]\ncommand = \"user-variant\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	projectDir := filepath.Join(env.WorktreeRoot, ".codex")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "config.toml"), []byte("[mcp_servers.shared]\ncommand = \"project-variant\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRunReport_Human_Smoke(t *testing.T) {
	setupManagedTestEnv(t, true, true)

	var stdout, stderr bytes.Buffer
	code := runReport(&stdout, &stderr, nil)
	if code != 0 {
		t.Fatalf("runReport = %d; stderr:\n%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Overview") {
		t.Errorf("human report output missing 'Overview':\n%s", stdout.String())
	}
}

func TestRunReport_JSON_IsStableAndValid(t *testing.T) {
	setupManagedTestEnv(t, true, true)

	var stdout1, stderr bytes.Buffer
	if code := runReport(&stdout1, &stderr, []string{"--json"}); code != 0 {
		t.Fatalf("runReport --json = %d; stderr:\n%s", code, stderr.String())
	}
	var a1 report.Artifact
	if err := json.Unmarshal(stdout1.Bytes(), &a1); err != nil {
		t.Fatalf("json.Unmarshal: %v\noutput:\n%s", err, stdout1.String())
	}
	if a1.Report.Spec.Fingerprint == "" {
		t.Error("Spec.Fingerprint is empty")
	}
	if len(a1.Hosts) != 2 {
		t.Errorf("len(Hosts) = %d, want 2 (both codex and claude-code installed)", len(a1.Hosts))
	}

	var stdout2 bytes.Buffer
	if code := runReport(&stdout2, &stderr, []string{"--json"}); code != 0 {
		t.Fatalf("runReport --json (2nd) = %d", code)
	}
	var a2 report.Artifact
	if err := json.Unmarshal(stdout2.Bytes(), &a2); err != nil {
		t.Fatalf("json.Unmarshal (2nd): %v", err)
	}
	if a1.Report.Spec.Fingerprint != a2.Report.Spec.Fingerprint {
		t.Errorf("fingerprint not stable across two runReport --json calls: %q vs %q", a1.Report.Spec.Fingerprint, a2.Report.Spec.Fingerprint)
	}
}

func TestRunReport_UnrecognizedArgument(t *testing.T) {
	setupManagedTestEnv(t, true, true)
	var stdout, stderr bytes.Buffer
	code := runReport(&stdout, &stderr, []string{"--bogus"})
	if code != 2 {
		t.Fatalf("runReport --bogus = %d, want 2", code)
	}
}

func TestRunReport_RealCollision_SurfacesSourceDrift(t *testing.T) {
	env := setupManagedTestEnv(t, true, false)
	writeCodexMCPCollision(t, env)

	var stdout, stderr bytes.Buffer
	if code := runReport(&stdout, &stderr, []string{"--json"}); code != 0 {
		t.Fatalf("runReport --json = %d; stderr:\n%s", code, stderr.String())
	}
	var a report.Artifact
	if err := json.Unmarshal(stdout.Bytes(), &a); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if len(a.ActionCards) == 0 {
		t.Fatalf("expected at least one drift ActionCard from the real config collision, got none; stderr:\n%s", stderr.String())
	}
	found := false
	for _, c := range a.ActionCards {
		if c.Category == "SOURCE_DRIFT" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a SOURCE_DRIFT card, got categories: %+v", a.ActionCards)
	}
}
