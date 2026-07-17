package observe

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	hostcontext "github.com/wangzitian0/oh-my-code-agent/internal/context"
	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
)

// repoFixturesDir locates the repository's top-level fixtures/ directory
// relative to this source file's own location, the same runtime.Caller
// technique internal/qualify/fixtures_test.go and internal/ontology's
// defaultConceptsDir use, so it resolves correctly regardless of the
// caller's working directory.
func repoFixturesDir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "fixtures")
}

// mustWriteFile writes content to path, creating parent directories as
// needed. Test-only helper: internal/observe's own production code must
// never write, but building a fixture tree for a test to read is ordinary,
// expected test setup.
func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mustWriteFile: mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("mustWriteFile: write %s: %v", path, err)
	}
}

// findObservation returns the one observation matching concept and an exact
// Source.Path, failing the test if there is not exactly one.
func findObservation(t *testing.T, obs []domain.Observation, concept, path string) domain.Observation {
	t.Helper()
	var matches []domain.Observation
	for _, o := range obs {
		if o.Spec.Concept == concept && o.Spec.Source.Path == path {
			matches = append(matches, o)
		}
	}
	if len(matches) != 1 {
		t.Fatalf("findObservation(concept=%s, path=%s): got %d matches, want 1 (have %d total observations)", concept, path, len(matches), len(obs))
	}
	return matches[0]
}

// hasObservation reports whether obs contains a record for concept at path.
func hasObservation(obs []domain.Observation, concept, path string) bool {
	for _, o := range obs {
		if o.Spec.Concept == concept && o.Spec.Source.Path == path {
			return true
		}
	}
	return false
}

// assertValid fails the test unless every observation in obs is
// domain.ValidateObservation-clean and carries a non-empty RawDigest and
// ParsedDigest — the PR-08 acceptance criterion "every observation record
// carries source path, scope, raw/parsed digests, and evidence level" is
// not fully covered by ValidateObservation alone (RawDigest/ParsedDigest
// are `omitempty` there, not required), so this helper asserts the
// stronger PR-08-specific bar every test in this package should hold
// Observe's output to.
func assertValid(t *testing.T, obs []domain.Observation) {
	t.Helper()
	for _, o := range obs {
		if err := domain.ValidateObservation(o); err != nil {
			t.Errorf("ValidateObservation(%s): %v", o.Metadata.ID, err)
		}
		if o.Spec.Source.Path == "" {
			t.Errorf("%s: source.path is empty", o.Metadata.ID)
		}
		if o.Spec.Scope.Kind == "" {
			t.Errorf("%s: scope.kind is empty", o.Metadata.ID)
		}
		if o.Spec.RawDigest == "" {
			t.Errorf("%s: rawDigest is empty", o.Metadata.ID)
		}
		if o.Spec.ParsedDigest == "" {
			t.Errorf("%s: parsedDigest is empty", o.Metadata.ID)
		}
		if o.Spec.EvidenceLevel != domain.EvidenceLevelDiscovered && o.Spec.EvidenceLevel != domain.EvidenceLevelParsed {
			t.Errorf("%s: evidenceLevel = %s, want E0 or E1 (this package must never emit a higher level)", o.Metadata.ID, o.Spec.EvidenceLevel)
		}
	}
}

// codexTree is one hermetic, synthetic Codex host layout for tests: a
// CODEX_HOME, a shared $HOME/.agents/skills root, and a worktree root, none
// of them the real machine's actual ~/.codex or ~/.agents (this matters
// because the `claude` binary running this very test suite is itself a
// Claude Code session — see fixtures/README.md's safety boundary).
type codexTree struct {
	CodexHome     string
	HomeAgentsDir string // $HOME/.agents/skills
	WorktreeRoot  string
}

func newCodexTree(t *testing.T) codexTree {
	t.Helper()
	root := t.TempDir()
	return codexTree{
		CodexHome:     filepath.Join(root, "codex-home"),
		HomeAgentsDir: filepath.Join(root, "home", ".agents", "skills"),
		WorktreeRoot:  filepath.Join(root, "project"),
	}
}

func (tr codexTree) request(version string) Request {
	return Request{
		Detection: hostcontext.HostDetection{
			Host:    "codex",
			Surface: "cli",
			Version: version,
			NativeHomes: []hostcontext.NativeHome{
				{Name: "CODEX_HOME", Path: tr.CodexHome, FromEnvVar: "CODEX_HOME"},
				{Name: "HOME/.agents/skills", Path: tr.HomeAgentsDir},
			},
		},
		WorktreeRoot: tr.WorktreeRoot,
	}
}

// claudeTree is claude-code's analogous synthetic host layout.
type claudeTree struct {
	ClaudeConfigDir string
	HomeAgentsDir   string
	WorktreeRoot    string
}

func newClaudeTree(t *testing.T) claudeTree {
	t.Helper()
	root := t.TempDir()
	return claudeTree{
		ClaudeConfigDir: filepath.Join(root, "claude-config"),
		HomeAgentsDir:   filepath.Join(root, "home", ".agents", "skills"),
		WorktreeRoot:    filepath.Join(root, "project"),
	}
}

func (tr claudeTree) request(version string) Request {
	return Request{
		Detection: hostcontext.HostDetection{
			Host:    "claude-code",
			Surface: "cli",
			Version: version,
			NativeHomes: []hostcontext.NativeHome{
				{Name: "CLAUDE_CONFIG_DIR", Path: tr.ClaudeConfigDir, FromEnvVar: "CLAUDE_CONFIG_DIR"},
				{Name: "HOME/.agents/skills", Path: tr.HomeAgentsDir},
			},
		},
		WorktreeRoot: tr.WorktreeRoot,
	}
}
