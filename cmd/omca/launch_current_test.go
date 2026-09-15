package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wangzitian0/oh-my-code-agent/internal/runtime"
)

func TestLaunchEntriesPreserveActivatedRuntime(t *testing.T) {
	for _, entry := range []string{"env", "run"} {
		t.Run(entry, func(t *testing.T) {
			env := setupManagedTestEnv(t, true, false)
			stateDir, _, selectedDir := buildPendingFixtureForActivate(t, env, mcpServerProfileYAML, nil, time.Now())
			var stdout, stderr bytes.Buffer
			if code := runActivate(&stdout, &stderr, []string{"codex", "--confirm", "enable-mcp-server:internal-docs"}); code != 0 {
				t.Fatalf("activate: %d: %s", code, stderr.String())
			}
			before, err := os.ReadFile(filepath.Join(stateDir, "current", "codex.json"))
			if err != nil {
				t.Fatal(err)
			}
			if entry == "env" {
				if code := runEnv(&stdout, &stderr, nil); code != 0 {
					t.Fatalf("env: %d: %s", code, stderr.String())
				}
			} else {
				_, stderr, code := runOmcaSubprocess(t, env.WorktreeRoot, []string{"run", "codex", "--", "--version"}, os.Environ())
				if code != 0 {
					t.Fatalf("run: %d: %s", code, stderr)
				}
			}
			got, err := runtime.CurrentGenerationDir(stateDir, "codex")
			if err != nil || got != selectedDir {
				t.Fatalf("%s replaced activated runtime: got %q, want %q, err %v", entry, got, selectedDir, err)
			}
			after, err := os.ReadFile(filepath.Join(stateDir, "current", "codex.json"))
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("%s rewrote activation evidence: %v", entry, err)
			}
		})
	}
}

func TestShellEntryDoesNotReplaceBrokenOrIncompatibleSelection(t *testing.T) {
	for _, failure := range []string{"manifest", "record", "host-upgrade"} {
		t.Run(failure, func(t *testing.T) {
			env := setupManagedTestEnv(t, true, false)
			stateDir, _, selectedDir := buildPendingFixtureForActivate(t, env, mcpServerProfileYAML, nil, time.Now())
			var stdout, stderr bytes.Buffer
			if code := runActivate(&stdout, &stderr, []string{"codex", "--confirm", "enable-mcp-server:internal-docs"}); code != 0 {
				t.Fatalf("activate: %d: %s", code, stderr.String())
			}
			switch failure {
			case "manifest":
				restoreWritableSkippingSymlinks(selectedDir)
				if err := os.WriteFile(filepath.Join(selectedDir, "manifest.json"), []byte("corrupt"), 0o644); err != nil {
					t.Fatal(err)
				}
			case "record":
				if err := os.Remove(filepath.Join(stateDir, "current", "codex.json")); err != nil {
					t.Fatal(err)
				}
			case "host-upgrade":
				writeFakeVersionBinary(t, env.BinDir, "codex", "codex-cli 0.153.4\n")
			}
			if code := runEnv(&stdout, &stderr, nil); code != 1 {
				t.Fatalf("%s should fail closed: code=%d, stderr=%s", failure, code, stderr.String())
			}
			got, err := runtime.CurrentGenerationDir(stateDir, "codex")
			if err != nil || got != selectedDir {
				t.Fatalf("%s destroyed selected state: %s (%v)", failure, got, err)
			}
		})
	}
}

func TestShellEntryLeavesUnapprovedPendingRuntimeInactive(t *testing.T) {
	env := setupManagedTestEnv(t, true, false)
	var stdout, stderr bytes.Buffer
	if code := runEnv(&stdout, &stderr, nil); code != 0 {
		t.Fatal(stderr.String())
	}
	stateDir := worktreeStateDirForTest(t, env)
	before, err := runtime.CurrentGenerationDir(stateDir, "codex")
	if err != nil {
		t.Fatal(err)
	}
	_, _, pending := buildPendingFixtureForActivate(t, env, mcpServerProfileYAML, nil, time.Now())
	if code := runEnv(&stdout, &stderr, nil); code != 0 {
		t.Fatal(stderr.String())
	}
	after, err := runtime.CurrentGenerationDir(stateDir, "codex")
	if err != nil || after != before || after == pending {
		t.Fatalf("shell entry activated pending: before=%s after=%s pending=%s err=%v", before, after, pending, err)
	}
	if strings.Contains(stdout.String(), "internal-docs") {
		t.Fatal("unapproved pending server appeared in shell exports")
	}
}
