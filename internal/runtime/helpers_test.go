package runtime

import (
	"os"
	"path/filepath"
	"testing"

	hostcontext "github.com/wangzitian0/oh-my-code-agent/internal/context"
	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
	"github.com/wangzitian0/oh-my-code-agent/internal/observe"
)

// worktreeIDFor computes a stand-in Worktree.ID the same way
// internal/context/worktree.go's DetectWorktree does for a real worktree
// ("worktree:" + domain.CanonicalDigest(root)), without needing a real .git
// directory -- these fixture trees only need a stable, collision-resistant
// identifier tied to root, not real Git detection.
func worktreeIDFor(t *testing.T, root string) string {
	t.Helper()
	digest, err := domain.CanonicalDigest(root)
	if err != nil {
		t.Fatalf("worktreeIDFor(%s): %v", root, err)
	}
	return "worktree:" + digest
}

// mustWriteFile writes content to path, creating parent directories as
// needed -- ordinary test fixture setup, mirroring
// internal/observe/helpers_test.go's identical helper.
func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mustWriteFile: mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("mustWriteFile: write %s: %v", path, err)
	}
}

// codexFixtureTree is a hermetic, synthetic Codex host layout: a CODEX_HOME
// and a worktree root, neither the real machine's actual ~/.codex (this
// matters because the `claude` binary running this very test suite is
// itself a Claude Code session -- see fixtures/README.md's safety
// boundary).
type codexFixtureTree struct {
	CodexHome    string
	WorktreeRoot string
}

func newCodexFixtureTree(t *testing.T) codexFixtureTree {
	t.Helper()
	root := t.TempDir()
	return codexFixtureTree{
		CodexHome:    filepath.Join(root, "codex-home"),
		WorktreeRoot: filepath.Join(root, "project"),
	}
}

// detection returns this tree's hostcontext.HostDetection, ready to hand to
// observe.Observe (via request) and to a BootstrapRequest.
func (tr codexFixtureTree) detection(version string) hostcontext.HostDetection {
	return hostcontext.HostDetection{
		Host:    "codex",
		Surface: "cli",
		Version: version,
		NativeHomes: []hostcontext.NativeHome{
			{Name: "CODEX_HOME", Path: tr.CodexHome, FromEnvVar: "CODEX_HOME"},
		},
	}
}

func (tr codexFixtureTree) request(version string) observe.Request {
	return observe.Request{Detection: tr.detection(version), WorktreeRoot: tr.WorktreeRoot}
}

func (tr codexFixtureTree) worktree(t *testing.T) hostcontext.Worktree {
	t.Helper()
	return hostcontext.Worktree{ID: worktreeIDFor(t, tr.WorktreeRoot), Root: tr.WorktreeRoot}
}

// claudeFixtureTree is codexFixtureTree's Claude Code analogue.
type claudeFixtureTree struct {
	ClaudeConfigDir string
	WorktreeRoot    string
}

func newClaudeFixtureTree(t *testing.T) claudeFixtureTree {
	t.Helper()
	root := t.TempDir()
	return claudeFixtureTree{
		ClaudeConfigDir: filepath.Join(root, "claude-config"),
		WorktreeRoot:    filepath.Join(root, "project"),
	}
}

func (tr claudeFixtureTree) detection(version string) hostcontext.HostDetection {
	return hostcontext.HostDetection{
		Host:    "claude-code",
		Surface: "cli",
		Version: version,
		NativeHomes: []hostcontext.NativeHome{
			{Name: "CLAUDE_CONFIG_DIR", Path: tr.ClaudeConfigDir, FromEnvVar: "CLAUDE_CONFIG_DIR"},
			// ClaudeConfigDir stands in for an explicitly-set
			// CLAUDE_CONFIG_DIR, under which real Claude Code relocates
			// .claude.json right along with the asset directory, so both
			// entries deliberately share the identical Path here (see
			// internal/context/host.go's claudeNativeHomes doc comment).
			{Name: "HOME/.claude.json", Path: tr.ClaudeConfigDir, FromEnvVar: "CLAUDE_CONFIG_DIR"},
		},
	}
}

func (tr claudeFixtureTree) request(version string) observe.Request {
	return observe.Request{Detection: tr.detection(version), WorktreeRoot: tr.WorktreeRoot}
}

func (tr claudeFixtureTree) worktree(t *testing.T) hostcontext.Worktree {
	t.Helper()
	return hostcontext.Worktree{ID: worktreeIDFor(t, tr.WorktreeRoot), Root: tr.WorktreeRoot}
}

// walkGeneratedTree returns every regular file's path (relative to root)
// and content under a compiled generation directory, for tests that need to
// assert something about the whole tree at once (issue #13 AC #1: "walk
// every generated file's content/paths").
func walkGeneratedTree(t *testing.T, root string) map[string][]byte {
	t.Helper()
	out := make(map[string][]byte)
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		out[rel] = data
		return nil
	})
	if err != nil {
		t.Fatalf("walkGeneratedTree(%s): %v", root, err)
	}
	return out
}

// restoreWritable chmods every file and directory under root back to a
// writable mode, registered via t.Cleanup before t.TempDir() tries to
// remove a read-only generation tree -- otherwise cleanup itself fails,
// exactly the pattern internal/observe/observe_test.go's
// TestObserve_UnreadableFile_EmitsE0 documents for a single file.
func restoreWritable(t *testing.T, root string) {
	t.Helper()
	t.Cleanup(func() {
		_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil //nolint:nilerr // best-effort cleanup, never fail the test here
			}
			if d.IsDir() {
				_ = os.Chmod(path, 0o755)
			} else {
				_ = os.Chmod(path, 0o644)
			}
			return nil
		})
	})
}
