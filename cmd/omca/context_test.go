package main

import (
	"bytes"
	"encoding/json"
	"testing"
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
	if !ok || len(hosts) != 2 {
		t.Fatalf("hosts is not a 2-element array: %s", stdout.String())
	}
	wantHostIDs := []string{"codex", "claude-code"}
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
