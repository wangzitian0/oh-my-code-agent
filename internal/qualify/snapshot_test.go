package qualify

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSnapshotTreeNonexistentRootIsEmpty(t *testing.T) {
	snap, err := snapshotTree(filepath.Join(t.TempDir(), "does-not-exist"))
	if err != nil {
		t.Fatalf("snapshotTree(nonexistent) error = %v, want nil", err)
	}
	if len(snap) != 0 {
		t.Errorf("snapshotTree(nonexistent) = %v, want empty", snap)
	}
}

func TestSnapshotTreeDetectsContentChange(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "a.txt")
	if err := os.WriteFile(path, []byte("before"), 0o644); err != nil {
		t.Fatal(err)
	}

	before, err := snapshotTree(root)
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, []byte("after"), 0o644); err != nil {
		t.Fatal(err)
	}

	after, err := snapshotTree(root)
	if err != nil {
		t.Fatal(err)
	}

	diffs := diffSnapshots(before, after)
	if len(diffs) != 1 || diffs[0] != "changed: a.txt" {
		t.Errorf("diffSnapshots after content change = %v, want [\"changed: a.txt\"]", diffs)
	}
}

func TestSnapshotTreeUnchangedIsEmptyDiff(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("stable"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}

	before, err := snapshotTree(root)
	if err != nil {
		t.Fatal(err)
	}
	after, err := snapshotTree(root)
	if err != nil {
		t.Fatal(err)
	}

	if diffs := diffSnapshots(before, after); len(diffs) != 0 {
		t.Errorf("diffSnapshots(unchanged tree) = %v, want empty", diffs)
	}
}

func TestSnapshotTreeDetectsAddedAndRemoved(t *testing.T) {
	root := t.TempDir()
	keep := filepath.Join(root, "keep.txt")
	remove := filepath.Join(root, "remove.txt")
	if err := os.WriteFile(keep, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(remove, []byte("remove"), 0o644); err != nil {
		t.Fatal(err)
	}

	before, err := snapshotTree(root)
	if err != nil {
		t.Fatal(err)
	}

	if err := os.Remove(remove); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "added.txt"), []byte("added"), 0o644); err != nil {
		t.Fatal(err)
	}

	after, err := snapshotTree(root)
	if err != nil {
		t.Fatal(err)
	}

	diffs := diffSnapshots(before, after)
	want := []string{"added: added.txt", "removed: remove.txt"}
	if len(diffs) != len(want) {
		t.Fatalf("diffSnapshots = %v, want %v", diffs, want)
	}
	for i := range want {
		if diffs[i] != want[i] {
			t.Errorf("diffSnapshots[%d] = %q, want %q", i, diffs[i], want[i])
		}
	}
}
