package statereport

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
)

// stateRootWith builds a synthetic state root: for each (worktree, entry)
// pair it writes a file of the requested size inside that worktree's codex
// native home, mirroring the real layout
// <root>/worktrees/<wt>/state/hosts/codex/cli/codex-home/<entry>.
func stateRootWith(t *testing.T, layout map[string]map[string]int) string {
	t.Helper()
	root := t.TempDir()
	for wt, entries := range layout {
		home := filepath.Join(root, "worktrees", wt, "state", "hosts", "codex", "cli", "codex-home")
		if err := os.MkdirAll(home, 0o755); err != nil {
			t.Fatal(err)
		}
		for name, size := range entries {
			dir := filepath.Join(home, name)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "blob"), make([]byte, size), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	return root
}

// TestMeasure_ReportsDuplicationOfASharingClass is the finding this package
// exists for.
//
// `cache` carries a sharing class, so the classification says one copy would
// serve every worktree. When N worktrees each hold their own, the class is
// true on paper and untrue on disk, and nothing else in the system says so.
// WastedBytes is everything beyond the single copy -- a measured cost, not
// an estimate.
func TestMeasure_ReportsDuplicationOfASharingClass(t *testing.T) {
	root := stateRootWith(t, map[string]map[string]int{
		"worktree-sha256-aaa": {"cache": 3000},
		"worktree-sha256-bbb": {"cache": 1000},
		"worktree-sha256-ccc": {"cache": 1000},
	})

	got, err := Measure(root)
	if err != nil {
		t.Fatalf("Measure: %v", err)
	}
	if got.Worktrees != 3 {
		t.Errorf("Worktrees = %d, want 3", got.Worktrees)
	}
	if len(got.Duplication) != 1 {
		t.Fatalf("Duplication has %d entries, want 1: %+v", len(got.Duplication), got.Duplication)
	}
	d := got.Duplication[0]
	if d.Name != "cache" || d.Class != domain.MutableStateWorktreeShared {
		t.Errorf("Duplication = %+v, want cache/worktree-shared", d)
	}
	if d.Copies != 3 {
		t.Errorf("Copies = %d, want 3", d.Copies)
	}
	// 5000 total, largest single copy 3000 -> 2000 is beyond what one copy needs.
	if d.WastedBytes != 2000 {
		t.Errorf("WastedBytes = %d, want 2000 (total minus the largest single copy)", d.WastedBytes)
	}
}

// TestMeasure_GenerationLocalIsNotDuplication is the negative control.
//
// generation-local state is per-scope by definition, so several copies are
// the design working. Reporting them as waste would train a reader to ignore
// the section that matters.
func TestMeasure_GenerationLocalIsNotDuplication(t *testing.T) {
	root := stateRootWith(t, map[string]map[string]int{
		"worktree-sha256-aaa": {"sessions": 2000},
		"worktree-sha256-bbb": {"sessions": 2000},
	})

	got, err := Measure(root)
	if err != nil {
		t.Fatalf("Measure: %v", err)
	}
	for _, d := range got.Duplication {
		if d.Name == "sessions" {
			t.Errorf("sessions reported as duplication (%+v); generation-local state is per-scope by design", d)
		}
	}
}

// TestMeasure_UnclassifiedIsReportedNotGuessed guards the honest default.
//
// On the machine this was written against, 88% of held state had no row in
// internal/auth's table at all -- `.tmp`, `plugins`, host log databases.
// Defaulting those to some class would have made the table look complete
// while hiding exactly the gap worth acting on.
func TestMeasure_UnclassifiedIsReportedNotGuessed(t *testing.T) {
	root := stateRootWith(t, map[string]map[string]int{
		"worktree-sha256-aaa": {"cache": 1000, "definitely-not-in-the-table": 4000},
	})

	got, err := Measure(root)
	if err != nil {
		t.Fatalf("Measure: %v", err)
	}
	var found bool
	for _, e := range got.Entries {
		if e.Name != "definitely-not-in-the-table" {
			continue
		}
		found = true
		if e.Classified {
			t.Error("an entry with no row in the table was reported as classified")
		}
		if e.Class != "" {
			t.Errorf("unclassified entry carries class %q; it must stay empty rather than be guessed", e.Class)
		}
	}
	if !found {
		t.Fatal("unclassified entry was dropped from the report entirely; a gap in the table must stay visible")
	}
}

// TestMeasure_SymlinkedShareCostsNothing proves the measurement can tell a
// real share from a copy.
//
// Once the sharing allowlist is actually applied, a shared entry becomes a
// symlink. If this counted the link's target it would report the same bytes
// N times and the fix would look like it changed nothing.
func TestMeasure_SymlinkedShareCostsNothing(t *testing.T) {
	root := stateRootWith(t, map[string]map[string]int{
		"worktree-sha256-aaa": {"cache": 5000},
	})
	realCache := filepath.Join(root, "worktrees", "worktree-sha256-aaa", "state", "hosts", "codex", "cli", "codex-home", "cache")

	shared := filepath.Join(root, "worktrees", "worktree-sha256-bbb", "state", "hosts", "codex", "cli", "codex-home")
	if err := os.MkdirAll(shared, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realCache, filepath.Join(shared, "cache")); err != nil {
		t.Fatal(err)
	}

	got, err := Measure(root)
	if err != nil {
		t.Fatalf("Measure: %v", err)
	}
	for _, e := range got.Entries {
		if e.Worktree == "worktree-sha256-bbb" && e.Bytes != 0 {
			t.Errorf("a symlinked share was counted as %d bytes; it must cost nothing, or applying the allowlist would look like it changed nothing", e.Bytes)
		}
	}
	for _, d := range got.Duplication {
		if d.WastedBytes != 0 {
			t.Errorf("a symlinked share was reported as %d wasted bytes: %+v", d.WastedBytes, d)
		}
	}
}

// TestMeasure_EmptyStateRootIsNotAnError keeps a fresh install quiet.
func TestMeasure_EmptyStateRootIsNotAnError(t *testing.T) {
	got, err := Measure(t.TempDir())
	if err != nil {
		t.Fatalf("Measure on a root with no worktrees: %v", err)
	}
	if got.Worktrees != 0 || got.TotalBytes != 0 {
		t.Errorf("empty root measured as %+v", got)
	}
}
