package report

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	hostcontext "github.com/wangzitian0/oh-my-code-agent/internal/context"
	"github.com/wangzitian0/oh-my-code-agent/internal/knowledge"
	"github.com/wangzitian0/oh-my-code-agent/internal/runtime"
)

// restoreGenerationDirWritable undoes runtime.Bootstrap's read-only tree
// (issue #13 AC "Generated artifact trees are read-only on disk") so
// t.TempDir()'s own cleanup can remove it — the same pattern
// internal/runtime/helpers_test.go's unexported restoreWritable uses,
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

// TestComparePlanes_MCPServerFragmentCorrelation_RealFixture reproduces,
// against the real observe.Observe pipeline over the committed
// fixtures/codex/0.144.5/mcp-merge corpus (the same corpus the M0-M3 review
// drove `omca compare --native --current --host codex` and
// `omca compare --observed --effective --host codex` over live), the
// ID-scheme bug planes.go's planeRows doc comment now documents: NATIVE/
// OBSERVED key mcp_server rows by Candidate.Ref (a per-server physical
// reference), HOST_EFFECTIVE by EffectiveEntry.LogicalID (a logical
// identity), and CURRENT/PENDING by GenerationSourceEntry.Source (a bare
// file path) — three schemes that essentially never intersect on their own,
// so every mcp_server row reported as "different" between any two planes
// regardless of whether the underlying state actually agreed.
//
// Before the fix: every mcp_server row in both comparisons below is a
// phantom mismatch (hasA != hasB, because the two planes never produce the
// same key for the same physical server). After the fix: the fixture's two
// single-source servers (user-only-server, project-only-server — see
// expected-effective.json) correlate correctly across both plane pairs,
// proving `omca compare`/`omca diff` can now tell "actually different" apart
// from "just differently keyed."
func TestComparePlanes_MCPServerFragmentCorrelation_RealFixture(t *testing.T) {
	root := repoRootForTest()
	c, sb := loadCaseSandbox(t, root, filepath.Join("fixtures", "codex", "0.144.5", "mcp-merge"))
	hostInput := buildCodexHostInput(t, sb, c)

	repo, err := knowledge.LoadRepository(filepath.Join(root, "knowledge", "hosts"))
	if err != nil {
		t.Fatalf("LoadRepository: %v", err)
	}

	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	worktree := hostcontext.Worktree{ID: "worktree:sha256:planes-fixture-test", Root: sb.Project}

	// Compile a real CURRENT generation from this exact fixture's
	// Observations via the same Bootstrap compiler `omca env` calls in
	// production. GenerationSourceEntry.Source ends up a bare config.toml
	// file path here, never a per-server fragment — exactly the CURRENT-
	// plane half of the bug this test reproduces (internal/runtime/
	// compile.go's compileHostTree loops over Observations, one entry per
	// file, not per server).
	bootstrapReq := runtime.BootstrapRequest{
		Detection:    hostInput.Detection,
		Worktree:     worktree,
		Observations: hostInput.Observations,
		Now:          now,
	}
	generationDir := filepath.Join(t.TempDir(), "generation")
	gen, err := runtime.Bootstrap(bootstrapReq, generationDir)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	restoreGenerationDirWritable(t, generationDir)
	worktreeStateDir := t.TempDir()
	if err := runtime.SetCurrentGeneration(worktreeStateDir, hostInput.Detection.Host, generationDir, gen, hostInput.Detection, now); err != nil {
		t.Fatalf("SetCurrentGeneration: %v", err)
	}

	artifact, err := Build(BuildRequest{
		Worktree:         worktree,
		WorktreeStateDir: worktreeStateDir,
		Hosts:            []HostInput{hostInput},
		Repository:       repo,
		Now:              now,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	if len(artifact.Debug["codex"].CurrentSources) == 0 {
		t.Fatal("sanity check failed: CurrentSources is empty; Bootstrap/SetCurrentGeneration wiring did not produce a readable CURRENT generation")
	}

	// OBSERVED vs EFFECTIVE: user-only-server is defined by exactly one
	// source (expected-effective.json), so the Identity Matcher resolves it
	// to a single-winner EffectiveEntry (not a Conflict) whose
	// Provenance.ActiveSources names the exact same Candidate.Ref NATIVE/
	// OBSERVED already uses for it. Before the fix, EFFECTIVE keyed this
	// entry by LogicalID ("stdio|user-only-server" or similar) while
	// OBSERVED keyed it by Candidate.Ref
	// ("<...>/config.toml#mcp_servers.user-only-server") — two unrelated
	// strings that never share a planeKey, so hasA and hasB could never
	// both be true for the same physical server. This only asserts
	// correlation (both sides present under one key), not agreement: the
	// two planes define "Active" differently (OBSERVED's Active means
	// literally ACTIVE-disposed at physical-scan time, which raw discovery
	// never claims; EFFECTIVE's Active means "resolved to a winner") —
	// a real, orthogonal distinction, not the ID-scheme bug this test
	// targets.
	t.Run("OBSERVED_vs_EFFECTIVE", func(t *testing.T) {
		result, ok := ComparePlanes(artifact, "codex", PlaneObserved, PlaneEffective)
		if !ok {
			t.Fatal("ComparePlanes: ok=false")
		}
		row := findMCPRowContaining(result.Rows, "user-only-server")
		if row == nil {
			t.Fatalf("no mcp_server row for user-only-server; rows: %+v", result.Rows)
		}
		if row.A == nil || row.B == nil {
			t.Fatalf("user-only-server row failed to correlate across OBSERVED/EFFECTIVE (phantom ID-scheme mismatch): %+v", row)
		}
		if row.A.ID != row.B.ID {
			t.Errorf("correlated row should share one physical-ref ID across OBSERVED/EFFECTIVE, got A.ID=%q B.ID=%q", row.A.ID, row.B.ID)
		}
	})

	// NATIVE vs CURRENT: the M1 bootstrap policy excludes every
	// user-global source unconditionally, so user-only-server is excluded
	// from the compiled CURRENT generation -- but for that to be a *real*,
	// explained exclusion rather than a phantom ID-scheme mismatch (before
	// the fix, CURRENT only ever carried the bare config.toml file path as
	// its row ID, which never equals any per-server Candidate.Ref), the row
	// must correlate: both A (native) and B (current) present under the
	// same physical-ref key.
	t.Run("NATIVE_vs_CURRENT", func(t *testing.T) {
		result, ok := ComparePlanes(artifact, "codex", PlaneNative, PlaneCurrent)
		if !ok {
			t.Fatal("ComparePlanes: ok=false")
		}
		row := findMCPRowContaining(result.Rows, "user-only-server")
		if row == nil {
			t.Fatalf("no mcp_server row for user-only-server; rows: %+v", result.Rows)
		}
		if row.A == nil || row.B == nil {
			t.Fatalf("user-only-server row failed to correlate across NATIVE/CURRENT (phantom ID-scheme mismatch): %+v", row)
		}
		if row.B.Active {
			t.Errorf("user-only-server should be excluded in CURRENT under the M1 bootstrap policy (every user-global source is excluded): %+v", row.B)
		}
		if row.B.Detail == "" {
			t.Error("CURRENT row should carry the compiler's exclusion reason, not an empty Detail")
		}
	})
}

// findMCPRowContaining returns the first mcp_server-concept row whose ID
// contains substr, or nil.
func findMCPRowContaining(rows []CompareRow, substr string) *CompareRow {
	for i := range rows {
		if rows[i].Concept == "mcp_server" && strings.Contains(rows[i].ID, substr) {
			return &rows[i]
		}
	}
	return nil
}
