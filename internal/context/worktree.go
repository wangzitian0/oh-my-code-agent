package context

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
)

// Worktree identifies one Git worktree: the runtime's primary scope
// (docs/architecture/runtime.md §1: "The runtime is scoped to a directory or
// Git worktree.").
type Worktree struct {
	// ID is a stable, content-addressed identifier ("worktree:sha256:...",
	// reusing domain.CanonicalDigest) derived from Root's resolved absolute
	// path. It is identical no matter which subdirectory of the worktree
	// DetectWorktree was invoked from.
	ID string `json:"id"`
	// Root is the resolved (symlink-evaluated) absolute path to the
	// worktree's top directory: the one containing the `.git` entry.
	Root string `json:"root"`
	// GitDir is the resolved Git directory for this worktree: `Root/.git`
	// itself for the main worktree, or the target of the `gitdir:` pointer
	// file for a linked worktree or submodule.
	GitDir string `json:"gitDir,omitempty"`
}

// DetectWorktree resolves the Git worktree containing startDir by walking
// upward from startDir until it finds a `.git` entry — a directory for the
// main worktree, or a file (containing "gitdir: <path>") for a linked
// worktree or a submodule. It deliberately never shells out to git: a pure
// filesystem walk keeps worktree detection hermetically testable (a test
// only needs to create a `.git` marker and some nested directories, not a
// real git binary or a real repository) and gives the exact same answer for
// every subdirectory of one worktree, which is what makes Worktree.ID stable
// regardless of the caller's cwd — the walk always converges on the same
// ancestor directory, and the ID is derived from that ancestor, never from
// startDir itself.
func DetectWorktree(startDir string) (Worktree, error) {
	abs, err := filepath.Abs(startDir)
	if err != nil {
		return Worktree{}, fmt.Errorf("context: DetectWorktree: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return Worktree{}, fmt.Errorf("context: DetectWorktree: resolve %s: %w", abs, err)
	}

	dir := resolved
	for {
		gitPath := filepath.Join(dir, ".git")
		info, statErr := os.Stat(gitPath)
		if statErr == nil {
			gitDir := gitPath
			if !info.IsDir() {
				if resolvedGitDir, parseErr := parseGitLinkFile(gitPath); parseErr == nil {
					gitDir = resolvedGitDir
				}
			}
			digest, digestErr := domain.CanonicalDigest(dir)
			if digestErr != nil {
				return Worktree{}, fmt.Errorf("context: DetectWorktree: %w", digestErr)
			}
			return Worktree{
				ID:     "worktree:" + digest,
				Root:   dir,
				GitDir: gitDir,
			}, nil
		}
		if !os.IsNotExist(statErr) {
			return Worktree{}, fmt.Errorf("context: DetectWorktree: stat %s: %w", gitPath, statErr)
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return Worktree{}, fmt.Errorf("context: DetectWorktree: no .git found above %s", abs)
		}
		dir = parent
	}
}

// parseGitLinkFile reads a linked-worktree or submodule `.git` file (exactly
// one line, "gitdir: <path>") and resolves path relative to the file's own
// directory when it is not already absolute, matching git's own documented
// behavior for this file.
func parseGitLinkFile(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("context: parseGitLinkFile: %w", err)
	}
	text := strings.TrimSpace(string(raw))
	const prefix = "gitdir:"
	if !strings.HasPrefix(text, prefix) {
		return "", fmt.Errorf("context: parseGitLinkFile: %s: does not start with %q", path, prefix)
	}
	target := strings.TrimSpace(strings.TrimPrefix(text, prefix))
	if target == "" {
		return "", fmt.Errorf("context: parseGitLinkFile: %s: empty gitdir target", path)
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(path), target)
	}
	target = filepath.Clean(target)

	// Best-effort symlink resolution: DetectWorktree only evaluates symlinks
	// once, on startDir, before this function ever runs, so a symlink
	// appearing solely inside the gitdir file's own target (not shared with
	// startDir's resolved path) would otherwise never be resolved, despite
	// this function's return value being documented as "the resolved Git
	// directory." EvalSymlinks errors on a target that does not exist (a
	// legitimate state — the pointer file can be read before whatever it
	// names is created); that is not this function's problem to report, so
	// it falls back to the cleaned-but-unresolved target rather than
	// failing the whole worktree detection over an informational field.
	if evaluated, err := filepath.EvalSymlinks(target); err == nil {
		return evaluated, nil
	}
	return target, nil
}
