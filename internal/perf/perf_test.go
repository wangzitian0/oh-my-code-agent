package perf

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	goruntime "runtime"
	"testing"
)

// newMeasurementBaseDir returns a fresh t.TempDir() with a t.Cleanup
// registered to restore write permission across the whole tree before
// t.TempDir()'s own cleanup tries to remove it. Every generation
// MeasureSynthetic/MeasureRealEnvironment compiles lands read-only on disk
// (internal/runtime/readonly.go), exactly like every other package's test
// suite in this module that compiles a real generation must account for
// (internal/runtime/helpers_test.go's restoreWritable) — t.Cleanup runs
// LIFO, so registering this AFTER t.TempDir() has already registered its
// own removal cleanup means this one runs first.
//
// Symlink entries are skipped, never chmod'd — the same distinction
// cmd/omca/testenv_test.go's restoreWritableSkippingSymlinks documents and
// this package hit in exactly the same way during development:
// runtime.SetCurrentGeneration plants a "current" symlink pointing at the
// compiled generation directory, and os.Chmod on Unix follows a symlink to
// its target. A naive walk that chmods every entry, including the symlink,
// as a "file" (0o644, no execute bit) ends up clobbering the ALREADY-fixed
// generation DIRECTORY's mode back down to a non-executable one after the
// walk has already visited and correctly fixed it directly — silently
// re-breaking traversal into that directory for both this cleanup's own
// remaining work and the later os.RemoveAll, with an error message
// ("permission denied" on a file two directories below the symlink) that
// gives no hint the symlink was the actual cause.
func newMeasurementBaseDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Cleanup(func() {
		_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil //nolint:nilerr // best-effort cleanup only
			}
			if d.Type()&os.ModeSymlink != 0 {
				return nil // never chmod through a symlink -- see doc comment above
			}
			if d.IsDir() {
				_ = os.Chmod(path, 0o755)
			} else {
				_ = os.Chmod(path, 0o644)
			}
			return nil
		})
	})
	return dir
}

// Generous, flake-free CI ceilings (round-2 acceptance-criteria addendum:
// "CI asserts generous flake-free ceilings (steady-state <= 300ms, first
// bootstrap <= 5s) so the gate never gets disabled for flakiness; the strict
// numbers live in the committed evidence, reviewed per release"). These are
// NOT the reference-machine targets (steady-state <= 100ms, first bootstrap
// <= 2s, docs/evidence/perf-v0.1.0.md) — they exist only to catch a gross
// regression (an accidental O(n^2) loop, a reintroduced subprocess call on
// the hot path, ...) without ever flaking on a slow or loaded CI runner.
const (
	ciSteadyStateCeiling    = 300_000_000   // 300ms, in time.Duration's int64 nanoseconds
	ciFirstBootstrapCeiling = 5_000_000_000 // 5s
	ciShimEntryCeiling      = 300_000_000   // 300ms: shim.Build alone has no compile step, so it shares the steady-state ceiling
)

// TestPerf_Synthetic_WithinCIGeneralCeilings is this PR's CI perf regression
// test (issue #15 deliverable #4): runs MeasureSynthetic — the hermetic,
// fixture-based measurement doc.go describes, touching no real ~/.codex or
// ~/.claude and spawning no real host binary — and asserts only the
// generous, documented-safe ceilings. `make perf` runs this same test with
// -v, whose t.Logf output is what a human reads off the reference machine
// to populate docs/evidence/perf-v0.1.0.md's strict numbers; this
// assertion never uses those strict numbers itself, precisely so the gate
// cannot flake because someone's laptop is a little slower than the
// reference machine.
func TestPerf_Synthetic_WithinCIGeneralCeilings(t *testing.T) {
	result, err := MeasureSynthetic(newMeasurementBaseDir(t), DefaultMeasureConfig)
	if err != nil {
		t.Fatalf("MeasureSynthetic: %v", err)
	}

	t.Logf("synthetic fixture (%d MCP servers, %d Skills): first-bootstrap %s, steady-state %s, shim-entry %s",
		DefaultMeasureConfig.Fixture.MCPServers, DefaultMeasureConfig.Fixture.Skills,
		result.FirstBootstrap, result.SteadyState, result.ShimEntry)

	assertWithinCeiling(t, "first-bootstrap", result.FirstBootstrap, ciFirstBootstrapCeiling)
	assertWithinCeiling(t, "steady-state", result.SteadyState, ciSteadyStateCeiling)
	assertWithinCeiling(t, "shim-entry", result.ShimEntry, ciShimEntryCeiling)
}

func assertWithinCeiling(t *testing.T, name string, s Stats, ceilingNanos int64) {
	t.Helper()
	if s.Count == 0 {
		t.Fatalf("%s: no samples were collected", name)
	}
	if int64(s.Max) > ceilingNanos {
		t.Errorf("%s: max sample %s exceeds the generous CI ceiling of %dms (stats: %s)", name, s.Max, ceilingNanos/1_000_000, s)
	}
}

// TestPerf_Synthetic_SteadyStateIsFasterThanFirstBootstrap is a sanity
// check on the measurement methodology itself, not a ceiling assertion:
// reusing an already-compiled generation must be faster than compiling one
// from scratch, or this benchmark's own "steady-state exercises the fast,
// no-recompile path" premise (internal/runtime/current.go's EnsureGeneration
// doc comment) would be unverified.
//
// Compares Min, not Mean: this was a real, observed CI flake (a shared,
// loaded runner occasionally inflated one side's mean enough to invert the
// comparison — first-bootstrap's mean measured faster than steady-state's
// on one CI run, with only 5 first-bootstrap samples against 20
// steady-state samples). Min is far more robust to that kind of scheduling
// noise than Mean here specifically because the two phases have
// structurally different floors, not just different averages:
// first-bootstrap's best possible sample still has to build a fresh fixture
// (real disk I/O: 30 fake MCP entries, 20 fake Skill files) and run a full
// compile, while steady-state's best possible sample is close to a bare
// EnsureGeneration existence check. A single lucky-fast first-bootstrap
// sample or one contention-slowed steady-state sample can shift a mean
// across 5-20 samples; it essentially never lets the cheaper phase's floor
// exceed the more expensive phase's floor.
func TestPerf_Synthetic_SteadyStateIsFasterThanFirstBootstrap(t *testing.T) {
	result, err := MeasureSynthetic(newMeasurementBaseDir(t), DefaultMeasureConfig)
	if err != nil {
		t.Fatalf("MeasureSynthetic: %v", err)
	}
	if result.SteadyState.Min >= result.FirstBootstrap.Min {
		t.Errorf("steady-state min (%s) is not faster than first-bootstrap min (%s); the reuse fast path may not be triggering", result.SteadyState.Min, result.FirstBootstrap.Min)
	}
}

// TestPerf_Synthetic_SmallConfig_NeverFlakesOnFixtureBuild is a fast,
// minimal-repetition run of the exact same code path
// TestPerf_Synthetic_WithinCIGeneralCeilings exercises, specifically to
// catch the class of hidden-external-dependency flake PR-10's own
// TestRunDoctor_DirenvApproval_StatusTimesOut lesson names (a fixture
// script's `sleep` failing to resolve because the synthetic PATH excluded
// /bin): this measurement's one subprocess call is buildFakeHostBinary's
// own `--version` script, invoked through internal/context.DetectHost with
// a PATH containing ONLY that fake binary's directory — proving the probe
// succeeds without depending on any other binary (sh itself, resolved via
// the script's own #! interpreter line, is the sole external dependency,
// present on every POSIX system this project's CI targets).
func TestPerf_Synthetic_SmallConfig_NeverFlakesOnFixtureBuild(t *testing.T) {
	cfg := MeasureConfig{
		FirstBootstrapReps: 1,
		SteadyStateReps:    1,
		ShimEntryReps:      1,
		Fixture:            SyntheticFixtureSize{MCPServers: 1, Skills: 1},
	}
	result, err := MeasureSynthetic(newMeasurementBaseDir(t), cfg)
	if err != nil {
		t.Fatalf("MeasureSynthetic: %v", err)
	}
	if result.FirstBootstrap.Count != 1 || result.SteadyState.Count != 1 || result.ShimEntry.Count != 1 {
		t.Errorf("sample counts = (%d, %d, %d), want (1, 1, 1)", result.FirstBootstrap.Count, result.SteadyState.Count, result.ShimEntry.Count)
	}
}

// TestPerf_RealEnvironment_ReportsThisMachinesActualNumbers is the
// round-2 addendum's "real-environment proof" line, run as an ordinary
// `go test` (not gated behind a build tag or explicit opt-in flag) so it
// participates in every normal `go test ./...`/`make test` pass — it never
// fails on a host-count assertion (a CI runner with neither codex nor
// claude installed simply logs "not installed" for both, matching this
// project's "UNKNOWN is safer than a guessed number" ethos, see
// internal/knowledge's knownUnknowns precedent), so it can never flake
// CI. On a machine that DOES have one or both hosts installed (a
// contributor's real dev machine, e.g. the one this PR's own evidence file
// was measured on), it produces the actual native-vs-managed numbers
// `make perf -v`'s output is read from.
func TestPerf_RealEnvironment_ReportsThisMachinesActualNumbers(t *testing.T) {
	repoRoot, err := findRepoRoot()
	if err != nil {
		t.Fatalf("findRepoRoot: %v", err)
	}
	cfg := DefaultRealEnvironmentConfig
	cfg.WorktreeRoot = repoRoot

	result, err := MeasureRealEnvironment(newMeasurementBaseDir(t), cfg)
	if err != nil {
		t.Fatalf("MeasureRealEnvironment: %v", err)
	}
	if len(result.Hosts) == 0 {
		t.Fatal("MeasureRealEnvironment returned no hosts at all")
	}
	for _, h := range result.Hosts {
		if !h.Installed {
			t.Logf("%s: %s (nothing measured)", h.Host, h.Detail)
			continue
		}
		t.Logf("%s: %s", h.Host, h.Detail)
		t.Logf("%s: first-bootstrap %s", h.Host, h.FirstBootstrap)
		t.Logf("%s: steady-state %s", h.Host, h.SteadyState)
		t.Logf("%s: shim-entry %s", h.Host, h.ShimEntry)
		t.Logf("%s: excluded %d native MCP configuration source(s), %d native Skill(s); estimated context-cost delta: %d tokens (%s)",
			h.Host, h.ExcludedMCPServers, h.ExcludedSkills, h.ContextCost.EstimatedTokensExcluded, h.ContextCost.Confidence)
	}
}

// packageDir locates this package's own source directory via
// runtime.Caller — the same technique internal/observe/helpers_test.go's
// repoFixturesDir, internal/qualify/fixtures_test.go, and cmd/omca/
// shim_test.go's identical packageDir all use — so path resolution below
// does not depend on the test binary's working directory.
func packageDir() string {
	_, file, _, _ := goruntime.Caller(0)
	return filepath.Dir(file)
}

// findRepoRoot walks upward from packageDir to the nearest ancestor
// containing a go.mod — this repository's own root — so
// MeasureRealEnvironment's WorktreeRoot is always this actual checkout,
// regardless of the test binary's working directory.
func findRepoRoot() (string, error) {
	dir := packageDir()
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("perf: findRepoRoot: no go.mod found above %s", packageDir())
		}
		dir = parent
	}
}
