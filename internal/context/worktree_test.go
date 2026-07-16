package context

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
)

// TestDetectWorktree_StableAcrossSubdirectories is the issue #11 acceptance
// criterion: "Worktree ID is stable when invoked from any subdirectory of
// the same worktree."
func TestDetectWorktree_StableAcrossSubdirectories(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(root, "a", "b", "c")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}

	atRoot, err := DetectWorktree(root)
	if err != nil {
		t.Fatalf("DetectWorktree(root): %v", err)
	}
	atNested, err := DetectWorktree(nested)
	if err != nil {
		t.Fatalf("DetectWorktree(nested): %v", err)
	}

	if atRoot.ID == "" {
		t.Fatal("DetectWorktree(root).ID is empty")
	}
	if atRoot.ID != atNested.ID {
		t.Errorf("ID differs by cwd: root=%q nested=%q, want identical", atRoot.ID, atNested.ID)
	}
	if atRoot.Root != atNested.Root {
		t.Errorf("Root differs by cwd: root=%q nested=%q, want identical", atRoot.Root, atNested.Root)
	}

	// A second, independent invocation from a third subdirectory must still
	// agree, not just a two-way comparison.
	third := filepath.Join(root, "a", "other")
	if err := os.MkdirAll(third, 0o755); err != nil {
		t.Fatal(err)
	}
	atThird, err := DetectWorktree(third)
	if err != nil {
		t.Fatalf("DetectWorktree(third): %v", err)
	}
	if atThird.ID != atRoot.ID {
		t.Errorf("ID at a third subdirectory = %q, want %q", atThird.ID, atRoot.ID)
	}
}

func TestDetectWorktree_IDIsCanonicalDigest(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	wt, err := DetectWorktree(root)
	if err != nil {
		t.Fatal(err)
	}
	const prefix = "worktree:"
	if !strings.HasPrefix(wt.ID, prefix) {
		t.Fatalf("ID = %q, want prefix %q", wt.ID, prefix)
	}
	digest := strings.TrimPrefix(wt.ID, prefix)
	if !domain.IsCanonicalDigest(digest) {
		t.Errorf("ID %q: %q is not a canonical sha256 digest", wt.ID, digest)
	}
}

func TestDetectWorktree_DifferentWorktreesDifferentIDs(t *testing.T) {
	rootA := t.TempDir()
	rootB := t.TempDir()
	for _, r := range []string{rootA, rootB} {
		if err := os.Mkdir(filepath.Join(r, ".git"), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	a, err := DetectWorktree(rootA)
	if err != nil {
		t.Fatal(err)
	}
	b, err := DetectWorktree(rootB)
	if err != nil {
		t.Fatal(err)
	}
	if a.ID == b.ID {
		t.Errorf("two distinct worktrees produced the same ID %q", a.ID)
	}
}

func TestDetectWorktree_LinkedWorktreeGitFile(t *testing.T) {
	root := t.TempDir()
	mainGitDir := filepath.Join(t.TempDir(), "main-repo", ".git", "worktrees", "linked")
	if err := os.MkdirAll(mainGitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	gitFile := filepath.Join(root, ".git")
	if err := os.WriteFile(gitFile, []byte("gitdir: "+mainGitDir+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	wt, err := DetectWorktree(root)
	if err != nil {
		t.Fatalf("DetectWorktree: %v", err)
	}
	// GitDir is symlink-resolved (parseGitLinkFile), which matters on macOS:
	// t.TempDir() commonly lives under /var/folders/..., itself a symlink to
	// /private/var/folders/.... The expected value must go through the same
	// resolution, or this assertion would spuriously fail on exactly the
	// platform (macOS) this project's CI runs tests on.
	wantGitDir, err := filepath.EvalSymlinks(mainGitDir)
	if err != nil {
		t.Fatalf("EvalSymlinks(%s): %v", mainGitDir, err)
	}
	if wt.GitDir != wantGitDir {
		t.Errorf("GitDir = %q, want %q", wt.GitDir, wantGitDir)
	}
	if wt.ID == "" {
		t.Error("ID is empty for a linked worktree")
	}
}

// TestDetectWorktree_GitDirResolvesSymlinkInsideTheGitdirTargetItself covers
// a symlink that appears only inside the gitdir pointer's own target — not
// anywhere on startDir's path, so DetectWorktree's single startDir
// EvalSymlinks call (which runs before the .git file is even found) cannot
// resolve it. Without parseGitLinkFile's own EvalSymlinks pass, GitDir would
// report the symlinked path rather than what it actually points at,
// contradicting its "resolved Git directory" doc comment.
func TestDetectWorktree_GitDirResolvesSymlinkInsideTheGitdirTargetItself(t *testing.T) {
	base := t.TempDir()
	realMainRepo := filepath.Join(base, "real-main-repo")
	realGitDir := filepath.Join(realMainRepo, ".git", "worktrees", "linked")
	if err := os.MkdirAll(realGitDir, 0o755); err != nil {
		t.Fatal(err)
	}

	linkedMainRepo := filepath.Join(base, "main-repo-link")
	if err := os.Symlink(realMainRepo, linkedMainRepo); err != nil {
		t.Skipf("symlinks unsupported in this environment: %v", err)
	}

	worktreeDir := filepath.Join(base, "worktree")
	if err := os.MkdirAll(worktreeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// The gitdir target itself goes through the main-repo-link symlink,
	// which is disjoint from worktreeDir's own (already-real) path.
	gitdirTarget := filepath.Join(linkedMainRepo, ".git", "worktrees", "linked")
	if err := os.WriteFile(filepath.Join(worktreeDir, ".git"), []byte("gitdir: "+gitdirTarget+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	wt, err := DetectWorktree(worktreeDir)
	if err != nil {
		t.Fatalf("DetectWorktree: %v", err)
	}
	wantGitDir, err := filepath.EvalSymlinks(realGitDir)
	if err != nil {
		t.Fatalf("EvalSymlinks(%s): %v", realGitDir, err)
	}
	if wt.GitDir != wantGitDir {
		t.Errorf("GitDir = %q, want the symlink-resolved target %q (not the symlinked path %q)", wt.GitDir, wantGitDir, gitdirTarget)
	}
	if strings.Contains(wt.GitDir, "main-repo-link") {
		t.Errorf("GitDir = %q still contains the symlink component %q, want it fully resolved", wt.GitDir, "main-repo-link")
	}
}

func TestDetectWorktree_LinkedWorktreeGitFileRelativePath(t *testing.T) {
	// Self-contained layout under one TempDir (never reaching into a
	// TempDir's parent, which other tests may share):
	//   base/worktree/.git   -> file, "gitdir: ../main-repo/.git/worktrees/linked"
	//   base/main-repo/.git/worktrees/linked/
	base := t.TempDir()
	worktreeDir := filepath.Join(base, "worktree")
	mainGitDir := filepath.Join(base, "main-repo", ".git", "worktrees", "linked")
	if err := os.MkdirAll(worktreeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(mainGitDir, 0o755); err != nil {
		t.Fatal(err)
	}

	gitFile := filepath.Join(worktreeDir, ".git")
	relTarget := filepath.Join("..", "main-repo", ".git", "worktrees", "linked")
	if err := os.WriteFile(gitFile, []byte("gitdir: "+relTarget+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	wt, err := DetectWorktree(worktreeDir)
	if err != nil {
		t.Fatalf("DetectWorktree: %v", err)
	}
	// DetectWorktree resolves symlinks in the search path before computing
	// GitDir (filepath.EvalSymlinks in worktree.go), which matters on macOS:
	// t.TempDir() commonly lives under /var/folders/..., itself a symlink to
	// /private/var/folders/.... The expected value must go through the same
	// resolution, or this assertion would spuriously fail on exactly the
	// platform (macOS) this project's CI runs tests on.
	resolvedMainGitDir, err := filepath.EvalSymlinks(mainGitDir)
	if err != nil {
		t.Fatalf("EvalSymlinks(%s): %v", mainGitDir, err)
	}
	wantGitDir := filepath.Clean(resolvedMainGitDir)
	if wt.GitDir != wantGitDir {
		t.Errorf("GitDir = %q, want %q (relative gitdir resolved against .git file's directory)", wt.GitDir, wantGitDir)
	}
}

func TestDetectWorktree_NoGitFound(t *testing.T) {
	// os.TempDir() is not expected to sit inside a git worktree; if this
	// assumption ever breaks in some environment, the test fails loudly
	// rather than silently passing for the wrong reason.
	root := t.TempDir()
	if _, err := DetectWorktree(root); err == nil {
		t.Fatalf("DetectWorktree(%s): want an error when no .git exists above it (or this test environment unexpectedly nests temp dirs inside a git worktree)", root)
	}
}

func TestDetectWorktree_MalformedGitFileFallsBackToRawPath(t *testing.T) {
	// A .git file that doesn't parse as "gitdir: <path>" must not fail
	// DetectWorktree outright — GitDir degrades to the .git file's own path
	// rather than blocking worktree identification on a field that is,
	// ultimately, informational.
	root := t.TempDir()
	gitFile := filepath.Join(root, ".git")
	if err := os.WriteFile(gitFile, []byte("not a gitdir line\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wt, err := DetectWorktree(root)
	if err != nil {
		t.Fatalf("DetectWorktree: %v", err)
	}
	// See TestDetectWorktree_LinkedWorktreeGitFileRelativePath: DetectWorktree
	// resolves symlinks in the search path first, which matters on macOS
	// (t.TempDir() commonly lives under a /var/folders symlink).
	resolvedGitFile, err := filepath.EvalSymlinks(gitFile)
	if err != nil {
		t.Fatalf("EvalSymlinks(%s): %v", gitFile, err)
	}
	if wt.GitDir != resolvedGitFile {
		t.Errorf("GitDir = %q, want fallback to the raw .git file path %q", wt.GitDir, resolvedGitFile)
	}
	if wt.ID == "" {
		t.Error("ID is empty despite a malformed .git file")
	}
}

func TestParseGitLinkFile(t *testing.T) {
	dir := t.TempDir()

	t.Run("empty target", func(t *testing.T) {
		p := filepath.Join(dir, "empty-target")
		if err := os.WriteFile(p, []byte("gitdir: \n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := parseGitLinkFile(p); err == nil {
			t.Error("parseGitLinkFile: want error for an empty gitdir target")
		}
	})

	t.Run("missing file", func(t *testing.T) {
		if _, err := parseGitLinkFile(filepath.Join(dir, "does-not-exist")); err == nil {
			t.Error("parseGitLinkFile: want error for a missing file")
		}
	})

	t.Run("wrong prefix", func(t *testing.T) {
		p := filepath.Join(dir, "wrong-prefix")
		if err := os.WriteFile(p, []byte("not-a-gitdir-line\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := parseGitLinkFile(p); err == nil {
			t.Error("parseGitLinkFile: want error when the file does not start with \"gitdir:\"")
		}
	})
}

func TestDetectWorktree_SymlinkedSubdirectoryResolvesToSameID(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	real := filepath.Join(root, "real")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks unsupported in this environment: %v", err)
	}

	viaReal, err := DetectWorktree(real)
	if err != nil {
		t.Fatalf("DetectWorktree(real): %v", err)
	}
	viaLink, err := DetectWorktree(link)
	if err != nil {
		t.Fatalf("DetectWorktree(link): %v", err)
	}
	if viaReal.ID != viaLink.ID {
		t.Errorf("ID via symlink = %q, want %q (same physical worktree)", viaLink.ID, viaReal.ID)
	}
}
