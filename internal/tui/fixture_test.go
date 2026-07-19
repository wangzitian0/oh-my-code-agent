package tui

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	hostcontext "github.com/wangzitian0/oh-my-code-agent/internal/context"
	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
	"github.com/wangzitian0/oh-my-code-agent/internal/knowledge"
	"github.com/wangzitian0/oh-my-code-agent/internal/observe"
	"github.com/wangzitian0/oh-my-code-agent/internal/qualify"
	"github.com/wangzitian0/oh-my-code-agent/internal/report"
	omcaruntime "github.com/wangzitian0/oh-my-code-agent/internal/runtime"
)

// update is shared by every golden-comparison helper in this package
// (this file's regeneration test, and golden_test.go's compareGolden):
// `go test ./internal/tui/... -update` regenerates every committed fixture
// and golden file this package's tests compare against, instead of just
// failing on a rendering change. This is the same "-update flag"
// convention Go's own stdlib and most golden-file-testing Go projects use;
// this repository had no prior text-content golden-file convention to
// match (internal/plugin/conformance/snapshot.go and
// internal/qualify/snapshot.go's own "snapshot"/"golden" naming is an
// unrelated zero-write filesystem proof, not a rendered-text comparison).
var update = flag.Bool("update", false, "regenerate this package's committed testdata (fixture artifact and view golden files)")

const fixtureArtifactPath = "testdata/fixture_artifact.json"

// repoRootForTest locates this repository's root relative to this source
// file's own location — the same runtime.Caller trick internal/report/
// build_test.go's own repoRootForTest (and internal/effective/
// fixture_test.go, internal/ontology, internal/qualify) use; duplicated
// here rather than exported cross-package since it is a two-line test-only
// helper, matching build_test.go's own doc comment on why it keeps its own
// copy instead of importing another package's unexported one.
func repoRootForTest() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..")
}

// buildRealFixtureArtifact assembles one real, deterministic report.Artifact
// by calling report.Build — the exact same assembly function cmd/omca/
// reportbuild.go's buildArtifactForCLI wraps for every CLI report/drift/
// explain/matrix command — fed by the committed fixtures/codex/0.144.5/
// mcp-merge corpus internal/report/build_test.go's own
// TestBuild_EndToEnd_RealFixture_ProducesSourceDrift already proves
// produces a genuine SOURCE_DRIFT ActionCard from a real multi-source MCP
// collision (both real committed codex Knowledge Packs declare resolve:
// UNKNOWN for mcp_server, so ComputeEffectiveGraph leaves "shared-tools" a
// Conflict). This gives every one of this package's four views real,
// non-synthetic content to render: ActionCards (Drift), Candidates across
// several real dispositions (Assets), and — once a Current and a Pending
// generation are bootstrapped for the same host below — CurrentSources/
// PendingSources (Generations).
//
// This is deliberately NOT a hand-built synthetic Artifact (docs/product/
// requirements.md's own instruction, echoed on issue #34, to reuse the
// real assembly path "rather than hand-constructing a synthetic Artifact
// that might not match real shape"): every field this function's caller
// renders came out of the real observe -> effective -> drift -> report
// pipeline, over real committed fixture input.
func buildRealFixtureArtifact(t *testing.T) report.Artifact {
	t.Helper()

	root := repoRootForTest()
	caseDir := filepath.Join(root, "fixtures", "codex", "0.144.5", "mcp-merge")
	c, err := qualify.LoadCase(caseDir)
	if err != nil {
		t.Fatalf("qualify.LoadCase(%s): %v", caseDir, err)
	}
	sb, err := qualify.NewSandbox(t.TempDir(), c.Host)
	if err != nil {
		t.Fatalf("qualify.NewSandbox: %v", err)
	}
	if err := sb.PopulateFromInput(c.InputDir()); err != nil {
		t.Fatalf("PopulateFromInput: %v", err)
	}

	detection := hostcontext.HostDetection{
		Host:      c.Host,
		Surface:   "cli",
		Installed: true,
		Version:   c.Version,
		NativeHomes: []hostcontext.NativeHome{
			{Name: "CODEX_HOME", Path: sb.CodexHome},
			{Name: "HOME/.agents/skills", Path: filepath.Join(sb.Home, ".agents", "skills")},
		},
	}
	obs, err := observe.Observe(observe.Request{Detection: detection, WorktreeRoot: sb.Project})
	if err != nil {
		t.Fatalf("observe.Observe: %v", err)
	}

	repo, err := knowledge.LoadRepository(filepath.Join(root, "knowledge", "hosts"))
	if err != nil {
		t.Fatalf("knowledge.LoadRepository: %v", err)
	}

	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	worktree := hostcontext.Worktree{ID: "worktree:sha256:tui-fixture", Root: sb.Project}
	worktreeStateDir := t.TempDir()

	// Current generation: every Observation this fixture collected.
	currentGen := bootstrapGeneration(t, detection, worktree, obs, now, worktreeStateDir)
	if err := omcaruntime.SetCurrentGeneration(worktreeStateDir, detection.Host, currentGen.dir, currentGen.gen, detection, now); err != nil {
		t.Fatalf("SetCurrentGeneration: %v", err)
	}

	// Pending generation: one fewer Observation than Current, so
	// (a) its GenerationID genuinely differs (runtime.GenerationID folds in
	// every Observation's own fingerprint) and (b) the Generations view has
	// a real, honest included/excluded difference to show between the two
	// pointers, not two identical-looking generations.
	if len(obs) < 2 {
		t.Fatalf("mcp-merge fixture unexpectedly has fewer than 2 observations (%d); cannot build a distinct pending generation", len(obs))
	}
	pendingGen := bootstrapGeneration(t, detection, worktree, obs[:len(obs)-1], now, worktreeStateDir)
	if err := omcaruntime.SetPendingGeneration(worktreeStateDir, detection.Host, pendingGen.dir, pendingGen.gen, detection, now); err != nil {
		t.Fatalf("SetPendingGeneration: %v", err)
	}

	artifact, err := report.Build(report.BuildRequest{
		Worktree:         worktree,
		WorktreeStateDir: worktreeStateDir,
		Hosts:            []report.HostInput{{Detection: detection, Observations: obs}},
		Repository:       repo,
		Now:              now,
	})
	if err != nil {
		t.Fatalf("report.Build: %v", err)
	}
	return artifact
}

// bootstrapGeneration runs the real runtime.Bootstrap compiler over obs —
// the same "compile a real CURRENT/PENDING generation from this exact
// fixture's Observations" technique internal/report/planes_fixture_test.go
// uses (TestComparePlanes_MCPServerFragmentCorrelation_RealFixture) — into
// a fresh, permanently-writable-again temp directory.
func bootstrapGeneration(t *testing.T, detection hostcontext.HostDetection, worktree hostcontext.Worktree, obs []domain.Observation, now time.Time, worktreeStateDir string) bootstrapResult {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "generation")
	gen, err := omcaruntime.Bootstrap(omcaruntime.BootstrapRequest{
		Detection:    detection,
		Worktree:     worktree,
		Observations: obs,
		Now:          now,
	}, dir)
	if err != nil {
		t.Fatalf("runtime.Bootstrap: %v", err)
	}
	restoreGenerationDirWritable(t, dir)
	return bootstrapResult{dir: dir, gen: gen}
}

type bootstrapResult struct {
	dir string
	gen domain.Generation
}

// restoreGenerationDirWritable undoes runtime.Bootstrap's read-only tree
// (issue #13 AC "Generated artifact trees are read-only on disk") so
// t.TempDir()'s own cleanup can remove it — the same pattern internal/
// report/planes_fixture_test.go's own restoreGenerationDirWritable uses,
// duplicated here since that helper does not cross the package boundary.
func restoreGenerationDirWritable(t *testing.T, root string) {
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

// TestRegenerateFixtureArtifact regenerates testdata/fixture_artifact.json
// from the real report.Build pipeline above when run with -update; every
// other test in this package loads the already-committed file (via
// loadFixtureArtifact) rather than rebuilding it, so a rendering change
// cannot silently "fix" a golden comparison by also changing the fixture
// out from under it — matching this repo's stated convention that golden/
// fixture files are committed, reviewed files, not generated at test-run
// time.
func TestRegenerateFixtureArtifact(t *testing.T) {
	if !*update {
		t.Skip("run with -update to regenerate testdata/fixture_artifact.json")
	}
	artifact := buildRealFixtureArtifact(t)
	b, err := json.MarshalIndent(artifact, "", "  ")
	if err != nil {
		t.Fatalf("json.MarshalIndent: %v", err)
	}
	b = append(b, '\n')
	if err := os.WriteFile(fixtureArtifactPath, b, 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", fixtureArtifactPath, err)
	}
}

// loadFixtureArtifact reads the committed testdata/fixture_artifact.json
// every one of this package's view snapshot tests renders from — "the
// same artifact the CLI goldens use" in spirit: it is produced by the
// identical report.Build call internal/report's own real-fixture golden
// tests (build_test.go, planes_fixture_test.go) already exercise, over the
// same committed fixtures/codex/0.144.5/mcp-merge corpus.
func loadFixtureArtifact(t *testing.T) report.Artifact {
	t.Helper()
	b, err := os.ReadFile(fixtureArtifactPath)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v (run `go test ./internal/tui/... -update` to generate it)", fixtureArtifactPath, err)
	}
	var a report.Artifact
	if err := json.Unmarshal(b, &a); err != nil {
		t.Fatalf("json.Unmarshal(%s): %v", fixtureArtifactPath, err)
	}
	return a
}
