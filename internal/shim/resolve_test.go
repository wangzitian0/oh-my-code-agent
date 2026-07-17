package shim

import (
	"os"
	"path/filepath"
	"testing"
)

// writeFakeExecutable writes an empty, harmless, executable file at
// dir/name — enough for isExecutableFile/ResolveReal's purposes, which only
// ever check existence and the executable bit, never run the file (running
// it, if this test's fixtures were ever exec'd, is exactly what
// ExecReplace's own subprocess-based tests in cmd/omca do deliberately and
// safely; this package's own resolve_test.go never invokes anything it
// creates).
func writeFakeExecutable(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("writeFakeExecutable: %v", err)
	}
	return path
}

// TestResolveReal_SkipsShimDir_EvenWhenListedFirst is issue #14's literal
// AC test: "build a fake 'real' binary, put the shim dir FIRST in PATH
// ahead of it (exactly the production condition), invoke the shim, and
// assert it reaches the fake real binary exactly once." This is the pure,
// non-subprocess half of that proof — ResolveReal is the actual decision
// logic a shim invocation's Build calls; cmd/omca/shim_test.go separately
// proves the same property end-to-end through a real exec.
//
// Both shimDir and realDir contain a file named "codex" — the shim
// directory's copy stands in for the shim's own PATH entry (in production,
// a symlink back to the omca binary); if ResolveReal ever returned that
// path instead of realDir's, a caller that then exec'd it would recurse.
func TestResolveReal_SkipsShimDir_EvenWhenListedFirst(t *testing.T) {
	shimDir := t.TempDir()
	realDir := t.TempDir()
	writeFakeExecutable(t, shimDir, "codex")
	wantPath := writeFakeExecutable(t, realDir, "codex")

	pathEnv := shimDir + string(os.PathListSeparator) + realDir
	got, err := ResolveReal("codex", pathEnv, shimDir)
	if err != nil {
		t.Fatalf("ResolveReal: %v", err)
	}
	if got != wantPath {
		t.Fatalf("ResolveReal = %q, want %q (the real binary, not the shim's own copy at %q)", got, wantPath, filepath.Join(shimDir, "codex"))
	}
}

// TestResolveReal_ShimDirOnly_NotFound proves ResolveReal never falls back
// to the shim's own copy even when it is the ONLY PATH entry containing the
// requested name — anything else would mean "no real binary available"
// silently degrading into "recurse into myself."
func TestResolveReal_ShimDirOnly_NotFound(t *testing.T) {
	shimDir := t.TempDir()
	writeFakeExecutable(t, shimDir, "codex")

	_, err := ResolveReal("codex", shimDir, shimDir)
	if err == nil {
		t.Fatal("ResolveReal with only the shim directory on PATH: want error, got nil")
	}
}

// TestResolveReal_EmptyShimDir_PlainLookup proves shimDir == "" behaves as
// an ordinary PATH search with nothing excluded — the mode `omca run
// --mode native` relies on when OMCA_SHIM_DIR is unset (running outside any
// managed shell).
func TestResolveReal_EmptyShimDir_PlainLookup(t *testing.T) {
	dir := t.TempDir()
	want := writeFakeExecutable(t, dir, "codex")

	got, err := ResolveReal("codex", dir, "")
	if err != nil {
		t.Fatalf("ResolveReal: %v", err)
	}
	if got != want {
		t.Fatalf("ResolveReal = %q, want %q", got, want)
	}
}

// TestResolveReal_NotFoundAnywhere proves a genuinely absent binary is a
// clean error, not a panic or an empty-string success.
func TestResolveReal_NotFoundAnywhere(t *testing.T) {
	dir := t.TempDir()
	if _, err := ResolveReal("does-not-exist", dir, ""); err == nil {
		t.Fatal("ResolveReal for a nonexistent binary: want error, got nil")
	}
}

// TestResolveReal_SymlinkedShimDir_StillExcluded proves the shim-directory
// exclusion compares resolved (symlink-evaluated) paths, not literal
// strings — a PATH entry that reaches the shim directory through a
// different-looking but equivalent path (e.g. a symlinked parent, common
// on macOS where /tmp itself is a symlink to /private/tmp) must still be
// excluded, since that is exactly the situation a real macOS shell PATH can
// produce.
func TestResolveReal_SymlinkedShimDir_StillExcluded(t *testing.T) {
	realShimDir := t.TempDir()
	writeFakeExecutable(t, realShimDir, "codex")
	realBinDir := t.TempDir()
	wantPath := writeFakeExecutable(t, realBinDir, "codex")

	aliasParent := t.TempDir()
	aliasShimDir := filepath.Join(aliasParent, "alias")
	if err := os.Symlink(realShimDir, aliasShimDir); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	// PATH lists the alias (a different string than realShimDir, but the
	// same directory once resolved) first, then the real binary directory.
	// shimDir is supplied as realShimDir's own literal path -- proving the
	// PATH *entry* need not spell shimDir identically to be excluded.
	pathEnv := aliasShimDir + string(os.PathListSeparator) + realBinDir
	got, err := ResolveReal("codex", pathEnv, realShimDir)
	if err != nil {
		t.Fatalf("ResolveReal: %v", err)
	}
	if got != wantPath {
		t.Fatalf("ResolveReal = %q, want %q", got, wantPath)
	}
}

// TestFilterOutDir_EmptyExcludeDir_NoOp proves FilterOutDir's documented
// no-op contract for excludeDir == "".
func TestFilterOutDir_EmptyExcludeDir_NoOp(t *testing.T) {
	pathEnv := "/a" + string(os.PathListSeparator) + "/b"
	if got := FilterOutDir(pathEnv, ""); got != pathEnv {
		t.Errorf("FilterOutDir(_, \"\") = %q, want unchanged %q", got, pathEnv)
	}
}
