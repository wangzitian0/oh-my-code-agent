package conformance

import (
	"os"
	"path/filepath"
	"testing"
)

// TestDiffSnapshotsDetectsChanges proves the enforcement mechanism behind
// the Observe zero-write proof actually detects violations, rather than
// vacuously passing because nothing ever differs. This is the "confirm the
// check would actually fail" proof for the snapshot diff itself.
func TestDiffSnapshotsDetectsChanges(t *testing.T) {
	before := snapshot{
		"a.txt": {mode: 0o644, digest: "aaa"},
		"dir":   {mode: os.ModeDir | 0o755},
	}

	t.Run("no changes", func(t *testing.T) {
		after := snapshot{
			"a.txt": {mode: 0o644, digest: "aaa"},
			"dir":   {mode: os.ModeDir | 0o755},
		}
		if diffs := diffSnapshots(before, after); len(diffs) != 0 {
			t.Errorf("diffSnapshots(identical trees) = %v, want empty", diffs)
		}
	})

	t.Run("content changed", func(t *testing.T) {
		after := snapshot{
			"a.txt": {mode: 0o644, digest: "bbb"}, // a buggy adapter wrote new content
			"dir":   {mode: os.ModeDir | 0o755},
		}
		diffs := diffSnapshots(before, after)
		if len(diffs) != 1 || diffs[0] != "changed: a.txt" {
			t.Errorf("diffSnapshots(content changed) = %v, want [\"changed: a.txt\"]", diffs)
		}
	})

	t.Run("file added", func(t *testing.T) {
		after := snapshot{
			"a.txt":     {mode: 0o644, digest: "aaa"},
			"dir":       {mode: os.ModeDir | 0o755},
			"marker.sh": {mode: 0o644, digest: "ccc"}, // a buggy adapter wrote a new file
		}
		diffs := diffSnapshots(before, after)
		if len(diffs) != 1 || diffs[0] != "added: marker.sh" {
			t.Errorf("diffSnapshots(file added) = %v, want [\"added: marker.sh\"]", diffs)
		}
	})

	t.Run("file removed", func(t *testing.T) {
		after := snapshot{
			"dir": {mode: os.ModeDir | 0o755},
		}
		diffs := diffSnapshots(before, after)
		if len(diffs) != 1 || diffs[0] != "removed: a.txt" {
			t.Errorf("diffSnapshots(file removed) = %v, want [\"removed: a.txt\"]", diffs)
		}
	})
}

func TestSnapshotTreeRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"a":1}`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "instructions.md"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	before, err := snapshotTree(dir)
	if err != nil {
		t.Fatalf("snapshotTree: %v", err)
	}
	if len(before) != 3 {
		t.Fatalf("snapshotTree found %d entries, want 3 (settings.json, sub, sub/instructions.md): %v", len(before), before)
	}

	// Re-snapshotting an untouched tree must diff to nothing.
	after, err := snapshotTree(dir)
	if err != nil {
		t.Fatalf("second snapshotTree: %v", err)
	}
	if diffs := diffSnapshots(before, after); len(diffs) != 0 {
		t.Errorf("diffSnapshots(untouched tree) = %v, want empty", diffs)
	}

	// Now actually mutate the tree and confirm the round trip catches it,
	// proving snapshotTree (not just diffSnapshots in isolation) detects a
	// real filesystem write.
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"a":2}`), 0o644); err != nil {
		t.Fatalf("WriteFile (mutation): %v", err)
	}
	mutated, err := snapshotTree(dir)
	if err != nil {
		t.Fatalf("third snapshotTree: %v", err)
	}
	diffs := diffSnapshots(before, mutated)
	if len(diffs) != 1 || diffs[0] != "changed: settings.json" {
		t.Errorf("diffSnapshots(after real mutation) = %v, want [\"changed: settings.json\"]", diffs)
	}
}
