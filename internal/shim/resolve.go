package shim

import (
	"fmt"
	"os"
	"path/filepath"
)

// CleanAbs resolves dir to its canonical, symlink-evaluated absolute form
// for comparison purposes, falling back to a merely filepath.Clean'd form
// when dir does not exist or cannot be resolved (e.g. a PATH entry naming a
// directory that was removed after PATH was exported) — a lookup miss
// there is not this function's problem to report. Exported so any other
// package comparing two filesystem paths for "same location" (not "same
// literal spelling") can reuse this exact canonicalization instead of
// re-deriving it — e.g. cmd/omca/doctor.go's checkPathBypass, which needs
// the identical macOS /tmp-vs-/private/tmp symlink-aware comparison this
// package's own non-recursion guarantee already depends on.
func CleanAbs(dir string) string {
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		if abs, err := filepath.Abs(resolved); err == nil {
			return abs
		}
	}
	if abs, err := filepath.Abs(dir); err == nil {
		return filepath.Clean(abs)
	}
	return filepath.Clean(dir)
}

// FilterOutDir returns pathEnv (an os.PathListSeparator-delimited PATH
// value) with every directory entry that resolves to the same location as
// excludeDir removed, preserving the relative order of every other entry.
// excludeDir == "" is a no-op (pathEnv is returned unchanged) — callers
// that have no shim directory to exclude (e.g. `omca run --mode native`
// invoked outside any managed shell) can pass it through unconditionally
// rather than branching themselves.
//
// Comparison is by resolved absolute path (CleanAbs), not string equality,
// so a PATH entry that reaches the same directory through a different
// symlink chain than excludeDir's own literal spelling is still correctly
// excluded — see doc.go's non-recursion design note for why this matters:
// this is the one piece of logic standing between a shim invocation and
// infinite self-recursion.
func FilterOutDir(pathEnv, excludeDir string) string {
	if excludeDir == "" {
		return pathEnv
	}
	excluded := CleanAbs(excludeDir)

	var kept []string
	for _, dir := range filepath.SplitList(pathEnv) {
		if dir == "" {
			kept = append(kept, dir) // preserve an empty PATH entry (POSIX: means ".") verbatim
			continue
		}
		if CleanAbs(dir) == excluded {
			continue
		}
		kept = append(kept, dir)
	}
	out := ""
	for i, dir := range kept {
		if i > 0 {
			out += string(os.PathListSeparator)
		}
		out += dir
	}
	return out
}

// isExecutableFile mirrors internal/context/host.go's and
// internal/qualify/invoke.go's identical, deliberately small helper of the
// same name (see internal/context/host.go's lookPathIn doc comment for why
// this ~10-line check is duplicated per package rather than exported: it
// has no state and no package-specific entanglement worth sharing).
func isExecutableFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	return info.Mode()&0o111 != 0
}

// ResolveReal resolves name (a bare executable name, e.g. "codex") to an
// absolute path by searching pathEnv left to right, skipping any directory
// that resolves to the same location as shimDir (via FilterOutDir) before
// searching — the shim's non-recursion guarantee (doc.go), proven under the
// adversarial "shim directory first in PATH" ordering by
// resolve_test.go's TestResolveReal_SkipsShimDir_EvenWhenListedFirst.
//
// shimDir == "" performs a plain PATH search with nothing excluded —
// `omca run --mode native` (cmd/omca/run.go) reuses this same function for
// its own real-binary lookup with shimDir set to whatever OMCA_SHIM_DIR
// this process actually computed (which may be "" outside any managed
// worktree), rather than duplicating a second PATH-search loop.
func ResolveReal(name, pathEnv, shimDir string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("shim: ResolveReal: empty command name")
	}
	filtered := FilterOutDir(pathEnv, shimDir)
	for _, dir := range filepath.SplitList(filtered) {
		if dir == "" {
			continue
		}
		candidate := filepath.Join(dir, name)
		if isExecutableFile(candidate) {
			return candidate, nil
		}
	}
	if shimDir != "" {
		return "", fmt.Errorf("shim: ResolveReal: %s: not found in PATH outside the OMCA shim directory %s", name, shimDir)
	}
	return "", fmt.Errorf("shim: ResolveReal: %s: not found in PATH", name)
}
