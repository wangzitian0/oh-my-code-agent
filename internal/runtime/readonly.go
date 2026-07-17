package runtime

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// readOnlyFileMode / readOnlyDirMode are the permission bits Bootstrap sets
// on every file and directory in a compiled generation, satisfying issue
// #13 AC "Generated artifact trees are read-only on disk"
// (docs/architecture/runtime.md §5.2: "Compiled configuration and asset
// trees are read-only."). A directory needs the execute bit to remain
// traversable/listable; only the write bit is actually being removed
// relative to the 0755/0644 modes os.MkdirAll/os.WriteFile create with.
const (
	readOnlyFileMode = 0o444
	readOnlyDirMode  = 0o555
)

// makeTreeReadOnly walks root (already fully written -- every file and
// subdirectory Bootstrap will ever create under it must exist before this
// runs) and strips the write bit from every file and directory, root
// included. It only ever removes permission, never grants any, so applying
// it after every write has already completed is safe: filepath.WalkDir's
// own directory listing only ever needs read+execute, which readOnlyDirMode
// still grants, so chmod'ing a directory read-only partway through the walk
// does not block WalkDir from continuing to descend into it.
//
// A caller (test, or a real generation consumer that needs to delete a
// superseded generation) that needs to remove a read-only tree afterward
// must first restore write permission -- see readonly_test.go's
// restoreWritable helper, the whole-tree analogue of
// internal/observe/observe_test.go's TestObserve_UnreadableFile_EmitsE0
// t.Cleanup pattern for a single file.
func makeTreeReadOnly(root string) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		mode := fs.FileMode(readOnlyFileMode)
		if d.IsDir() {
			mode = readOnlyDirMode
		}
		if chmodErr := os.Chmod(path, mode); chmodErr != nil {
			return fmt.Errorf("runtime: makeTreeReadOnly: chmod %s: %w", path, chmodErr)
		}
		return nil
	})
}
