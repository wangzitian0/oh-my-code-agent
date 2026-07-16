package qualify

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRealHomeCheckDisabledByDefault(t *testing.T) {
	t.Setenv(RealHomeCheckEnvVar, "")
	if RealHomeCheckEnabled() {
		t.Error("RealHomeCheckEnabled() = true with the env var unset, want false")
	}
}

func TestRealHomeCheckEnabledWhenSet(t *testing.T) {
	t.Setenv(RealHomeCheckEnvVar, "1")
	if !RealHomeCheckEnabled() {
		t.Error("RealHomeCheckEnabled() = false with the env var set, want true")
	}
}

func TestRealHomePathsPerHost(t *testing.T) {
	codexPaths := RealHomePaths("codex", "/home/x")
	if len(codexPaths) == 0 {
		t.Error("RealHomePaths(codex) is empty")
	}
	claudePaths := RealHomePaths("claude-code", "/home/x")
	if len(claudePaths) == 0 {
		t.Error("RealHomePaths(claude-code) is empty")
	}
	if got := RealHomePaths("unknown-host", "/home/x"); got != nil {
		t.Errorf("RealHomePaths(unknown) = %v, want nil", got)
	}
}

func TestSnapshotAndDiffRealHomeCleanWhenUntouched(t *testing.T) {
	fakeRealHome := t.TempDir()
	if err := os.WriteFile(filepath.Join(fakeRealHome, "settings.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	paths := []string{filepath.Join(fakeRealHome, "settings.json"), filepath.Join(fakeRealHome, "does-not-exist")}

	before, err := SnapshotRealHome(paths)
	if err != nil {
		t.Fatalf("SnapshotRealHome: %v", err)
	}
	after, err := SnapshotRealHome(paths)
	if err != nil {
		t.Fatalf("SnapshotRealHome: %v", err)
	}
	if diffs := DiffRealHomeSnapshots(before, after); len(diffs) != 0 {
		t.Errorf("DiffRealHomeSnapshots(untouched) = %v, want empty", diffs)
	}
}

func TestSnapshotAndDiffRealHomeDetectsChange(t *testing.T) {
	fakeRealHome := t.TempDir()
	target := filepath.Join(fakeRealHome, "settings.json")
	if err := os.WriteFile(target, []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	paths := []string{target}

	before, err := SnapshotRealHome(paths)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte(`{"changed":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	after, err := SnapshotRealHome(paths)
	if err != nil {
		t.Fatal(err)
	}
	diffs := DiffRealHomeSnapshots(before, after)
	if len(diffs) != 1 {
		t.Fatalf("DiffRealHomeSnapshots(changed) = %v, want one diff", diffs)
	}
}
