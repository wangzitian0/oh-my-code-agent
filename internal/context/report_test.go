package context

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// fakeDetectionEnvironment builds a synthetic worktree (with a .git marker)
// and a synthetic PATH carrying fake codex/claude binaries, entirely under
// t.TempDir() — no dependency on this machine's real git worktree, real
// PATH, or real installed hosts.
func fakeDetectionEnvironment(t *testing.T) (startDir string, env Environment) {
	t.Helper()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	binDir := t.TempDir()
	writeFakeBinary(t, binDir, "codex", "codex-cli 0.144.5")
	writeFakeBinary(t, binDir, "claude", "2.1.211 (Claude Code)")
	home := t.TempDir()
	env = Environment{Vars: []string{"HOME=" + home, "PATH=" + binDir}}
	return root, env
}

func TestDetect_WorktreeAndHostsPopulated(t *testing.T) {
	startDir, env := fakeDetectionEnvironment(t)

	report, err := Detect(context.Background(), startDir, env)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if report.Worktree.ID == "" {
		t.Error("Worktree.ID is empty")
	}
	if len(report.Hosts) != 2 {
		t.Fatalf("len(Hosts) = %d, want 2", len(report.Hosts))
	}
	if report.Hosts[0].Host != "codex" {
		t.Errorf("Hosts[0].Host = %q, want %q", report.Hosts[0].Host, "codex")
	}
	if report.Hosts[1].Host != "claude-code" {
		t.Errorf("Hosts[1].Host = %q, want %q", report.Hosts[1].Host, "claude-code")
	}
	if !report.Hosts[0].Installed || report.Hosts[0].Version != "0.144.5" {
		t.Errorf("Hosts[0] = %+v, want Installed=true Version=0.144.5", report.Hosts[0])
	}
	if !report.Hosts[1].Installed || report.Hosts[1].Version != "2.1.211" {
		t.Errorf("Hosts[1] = %+v, want Installed=true Version=2.1.211", report.Hosts[1])
	}
}

// TestDetect_StableJSON is issue #11's acceptance criterion: "stable JSON
// output." Two Detect calls against byte-identical inputs must marshal to
// byte-identical JSON, and the shape must not depend on map iteration order
// anywhere.
func TestDetect_StableJSON(t *testing.T) {
	startDir, env := fakeDetectionEnvironment(t)

	first, err := Detect(context.Background(), startDir, env)
	if err != nil {
		t.Fatalf("Detect (first): %v", err)
	}
	second, err := Detect(context.Background(), startDir, env)
	if err != nil {
		t.Fatalf("Detect (second): %v", err)
	}

	firstJSON, err := json.Marshal(first)
	if err != nil {
		t.Fatalf("Marshal(first): %v", err)
	}
	secondJSON, err := json.Marshal(second)
	if err != nil {
		t.Fatalf("Marshal(second): %v", err)
	}
	if string(firstJSON) != string(secondJSON) {
		t.Errorf("JSON differs across repeated Detect calls on identical input:\nfirst:  %s\nsecond: %s", firstJSON, secondJSON)
	}

	// Repeat several more times to make a would-be nondeterministic map
	// iteration order fail reliably rather than by chance.
	for i := 0; i < 5; i++ {
		r, err := Detect(context.Background(), startDir, env)
		if err != nil {
			t.Fatalf("Detect (repeat %d): %v", i, err)
		}
		j, err := json.Marshal(r)
		if err != nil {
			t.Fatalf("Marshal (repeat %d): %v", i, err)
		}
		if string(j) != string(firstJSON) {
			t.Errorf("repeat %d: JSON differs:\nfirst: %s\ngot:   %s", i, firstJSON, j)
		}
	}
}

func TestDetect_JSONShape(t *testing.T) {
	startDir, env := fakeDetectionEnvironment(t)
	report, err := Detect(context.Background(), startDir, env)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var generic map[string]any
	if err := json.Unmarshal(raw, &generic); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	for _, key := range []string{"worktree", "hosts"} {
		if _, ok := generic[key]; !ok {
			t.Errorf("top-level JSON is missing key %q: %s", key, raw)
		}
	}
	worktree, ok := generic["worktree"].(map[string]any)
	if !ok {
		t.Fatalf("worktree is not a JSON object: %s", raw)
	}
	for _, key := range []string{"id", "root"} {
		if _, ok := worktree[key]; !ok {
			t.Errorf("worktree JSON is missing key %q: %s", key, raw)
		}
	}
	hosts, ok := generic["hosts"].([]any)
	if !ok || len(hosts) != 2 {
		t.Fatalf("hosts is not a 2-element JSON array: %s", raw)
	}
	firstHost, ok := hosts[0].(map[string]any)
	if !ok {
		t.Fatalf("hosts[0] is not a JSON object: %s", raw)
	}
	for _, key := range []string{"host", "surface", "platform", "installed"} {
		if _, ok := firstHost[key]; !ok {
			t.Errorf("hosts[0] JSON is missing key %q: %s", key, raw)
		}
	}
}

func TestDetect_PropagatesWorktreeError(t *testing.T) {
	_, env := fakeDetectionEnvironment(t)
	// A directory with no .git anywhere above it (a fresh TempDir, not the
	// one fakeDetectionEnvironment planted a .git marker in).
	noGitDir := t.TempDir()
	if _, err := Detect(context.Background(), noGitDir, env); err == nil {
		t.Fatal("Detect: want error when startDir is outside any worktree")
	}
}
