package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/wangzitian0/oh-my-code-agent/internal/report"
)

func TestRunExplain_RealCollision_ConflictWithTrace(t *testing.T) {
	env := setupManagedTestEnv(t, true, false)
	writeCodexMCPCollision(t, env)

	var stdout, stderr bytes.Buffer
	code := runExplain(&stdout, &stderr, []string{"mcp_server", "stdio|shared", "--trace", "--json"})
	if code != 0 {
		t.Fatalf("runExplain = %d; stderr:\n%s", code, stderr.String())
	}
	var result report.ExplainResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("json.Unmarshal: %v\noutput:\n%s", err, stdout.String())
	}
	if !result.Found || !result.Conflict {
		t.Fatalf("Found=%v Conflict=%v, want both true; full result: %+v", result.Found, result.Conflict, result)
	}
	if result.Trace == nil {
		t.Fatal("Trace is nil even though --trace was passed")
	}
	if len(result.Trace.PhysicalSources) < 2 {
		t.Errorf("expected at least 2 physical sources for the collision, got %d: %+v", len(result.Trace.PhysicalSources), result.Trace.PhysicalSources)
	}
}

func TestRunExplain_NotFound_Human(t *testing.T) {
	setupManagedTestEnv(t, true, true)
	var stdout, stderr bytes.Buffer
	code := runExplain(&stdout, &stderr, []string{"skill", "does-not-exist"})
	if code != 1 {
		t.Fatalf("runExplain (not found) = %d, want 1", code)
	}
	if !strings.Contains(stdout.String(), "not found") {
		t.Errorf("expected 'not found' in human output:\n%s", stdout.String())
	}
}

func TestRunExplain_MissingArgs(t *testing.T) {
	setupManagedTestEnv(t, true, true)
	var stdout, stderr bytes.Buffer
	code := runExplain(&stdout, &stderr, []string{"skill"})
	if code != 2 {
		t.Fatalf("runExplain (missing logical-id) = %d, want 2", code)
	}
}

func TestRunExplain_ExplicitHost(t *testing.T) {
	env := setupManagedTestEnv(t, true, false)
	writeCodexMCPCollision(t, env)

	var stdout, stderr bytes.Buffer
	code := runExplain(&stdout, &stderr, []string{"--host", "codex", "mcp_server", "stdio|shared", "--json"})
	if code != 0 {
		t.Fatalf("runExplain --host codex = %d; stderr:\n%s", code, stderr.String())
	}

	var stdout2, stderr2 bytes.Buffer
	code2 := runExplain(&stdout2, &stderr2, []string{"--host", "claude-code", "mcp_server", "stdio|shared", "--json"})
	if code2 != 1 {
		t.Fatalf("runExplain --host claude-code (not built, not installed) = %d, want 1; stderr:\n%s", code2, stderr2.String())
	}
}
