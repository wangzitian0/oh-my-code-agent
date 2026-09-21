package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	hostcontext "github.com/wangzitian0/oh-my-code-agent/internal/context"
)

// TestRunContext_ProducesStableShapedJSON exercises `omca context` against
// the real process environment (this test binary's own cwd, which go test
// always runs from inside this repository, and the real PATH/HOME). It
// deliberately does not assert whether codex/claude are Installed: CI
// runners have neither installed, while a contributor's own machine may
// have both — the issue #11 acceptance criterion this proves is the JSON
// shape and the presence of a worktree identity and both first-party hosts,
// not any particular installation state.
func TestRunContext_ProducesStableShapedJSON(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"context"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run([context]) = %d, want 0; stderr=%s", code, stderr.String())
	}

	var report map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout=%s", err, stdout.String())
	}

	worktree, ok := report["worktree"].(map[string]any)
	if !ok {
		t.Fatalf("worktree is missing or not an object: %s", stdout.String())
	}
	if id, _ := worktree["id"].(string); id == "" {
		t.Errorf("worktree.id is empty: %s", stdout.String())
	}

	hosts, ok := report["hosts"].([]any)
	if !ok || len(hosts) != 3 {
		t.Fatalf("hosts is not a 3-element array: %s", stdout.String())
	}
	wantHostIDs := []string{"codex", "claude-code", "pi"}
	for i, raw := range hosts {
		h, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("hosts[%d] is not an object: %s", i, stdout.String())
		}
		if got, _ := h["host"].(string); got != wantHostIDs[i] {
			t.Errorf("hosts[%d].host = %q, want %q", i, got, wantHostIDs[i])
		}
		for _, key := range []string{"surface", "platform", "installed", "nativeHomes"} {
			if _, ok := h[key]; !ok {
				t.Errorf("hosts[%d] is missing key %q: %s", i, key, stdout.String())
			}
		}
		// knowledgePack is only present when the host is actually installed
		// with a parseable version (omitempty) — assert its shape only when
		// it appears, so this test passes identically whether or not this
		// machine has codex/claude installed.
		if kp, present := h["knowledgePack"]; present {
			kpMap, ok := kp.(map[string]any)
			if !ok {
				t.Fatalf("hosts[%d].knowledgePack is not an object: %s", i, stdout.String())
			}
			if _, ok := kpMap["qualified"]; !ok {
				t.Errorf("hosts[%d].knowledgePack is missing \"qualified\": %s", i, stdout.String())
			}
		}
	}
}

func TestRunContext_DeterministicAcrossCalls(t *testing.T) {
	var first, firstErr bytes.Buffer
	if code := run([]string{"context"}, &first, &firstErr); code != 0 {
		t.Fatalf("run([context]) (first) = %d; stderr=%s", code, firstErr.String())
	}
	var second, secondErr bytes.Buffer
	if code := run([]string{"context"}, &second, &secondErr); code != 0 {
		t.Fatalf("run([context]) (second) = %d; stderr=%s", code, secondErr.String())
	}
	if first.String() != second.String() {
		t.Errorf("output differs across repeated invocations:\nfirst:  %s\nsecond: %s", first.String(), second.String())
	}
}

// TestRunContext_DetectsThroughFilteredPath_NotTheShim proves `omca context`
// probes the NATIVE binary, not this worktree's shim.
//
// Every other command that detects a host (env, doctor, run, activate,
// rollback, bisect, mcp, tui, reportbuild, qualify) strips the shim
// directory from PATH first. context was the one caller passing the raw
// environment, so inside a managed shell -- the normal case, where direnv
// has put the shim first on PATH -- `codex --version` resolved to the shim
// and exec'd the real binary under the generation's virtualized HOME. For an
// asdf-installed host that is the exit-126 dispatch failure runtime.md
// §7.1.1 documents, and it made `omca context` report codex as installed
// with no version and a probe error on a machine where `omca env` and
// `omca doctor` both detected it fine.
//
// The fixture reproduces that shape directly: a shim-named binary that
// always fails, shadowing a working native one.
func TestRunContext_DetectsThroughFilteredPath_NotTheShim(t *testing.T) {
	env := setupManagedTestEnv(t, true, false)

	var envOut, envErr bytes.Buffer
	if code := runEnv(&envOut, &envErr, nil); code != 0 {
		t.Fatalf("runEnv = %d; stderr:\n%s", code, envErr.String())
	}
	wt, err := hostcontext.DetectWorktree(env.WorktreeRoot)
	if err != nil {
		t.Fatalf("DetectWorktree: %v", err)
	}
	stateRoot, err := realStateRoot()
	if err != nil {
		t.Fatalf("realStateRoot: %v", err)
	}
	shimDir := shimDirPath(worktreeStateDirPath(stateRoot, wt.ID))

	// Stand in for the asdf-under-virtualized-HOME failure: the shim entry
	// exits 126 for any invocation, including --version.
	if err := os.WriteFile(filepath.Join(shimDir, "codex"), []byte("#!/bin/sh\nexit 126\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Managed shell: shim directory first on PATH, native binary behind it.
	t.Setenv("PATH", shimDir+string(os.PathListSeparator)+env.BinDir)

	var stdout, stderr bytes.Buffer
	if code := runContext(&stdout, &stderr); code != 0 {
		t.Fatalf("runContext = %d; stderr:\n%s", code, stderr.String())
	}

	var report struct {
		Hosts []struct {
			Host      string `json:"host"`
			Installed bool   `json:"installed"`
			Version   string `json:"version"`
			Error     string `json:"error"`
		} `json:"hosts"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\n%s", err, stdout.String())
	}
	for _, h := range report.Hosts {
		if h.Host != "codex" {
			continue
		}
		if h.Error != "" {
			t.Errorf("codex carries a probe error, so detection went through the shim instead of the native binary: %s", h.Error)
		}
		if h.Version == "" {
			t.Errorf("codex version is empty; the native binary reports one, only the shim does not")
		}
		return
	}
	t.Fatal("codex is missing from the context report entirely")
}
