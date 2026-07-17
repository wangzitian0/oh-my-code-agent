package perf

import (
	stdcontext "context"
	"fmt"
	"path/filepath"
	"time"

	hostcontext "github.com/wangzitian0/oh-my-code-agent/internal/context"
	"github.com/wangzitian0/oh-my-code-agent/internal/observe"
	"github.com/wangzitian0/oh-my-code-agent/internal/runtime"
	"github.com/wangzitian0/oh-my-code-agent/internal/shim"
)

// Result is one complete measurement pass's output: the three phases the M1
// exit gate and issue #15's AC name.
type Result struct {
	// FirstBootstrap times the full detect+observe+compile sequence
	// `omca env`/`omca run` perform against a worktree with no prior
	// generation — the "first bootstrap" path, where runtime.EnsureGeneration
	// must actually compile (internal/runtime.Bootstrap runs in full).
	FirstBootstrap Stats
	// SteadyState times the identical sequence against a worktree whose
	// generation is already compiled and unchanged — runtime.EnsureGeneration
	// takes its fast, no-recompile reuse path (current.go's own doc comment:
	// "a present, valid outputDir for the same ID is -- by construction --
	// identical to what Bootstrap would produce again"). This is what the
	// roadmap's M1 exit gate calls "generation-selection overhead."
	SteadyState Stats
	// ShimEntry times internal/shim.Build alone (no DetectHost, no Observe,
	// no compile) against an already-established "current" pointer — the
	// roadmap's M1 exit gate's other named overhead source, "shim... overhead."
	// A managed session's steady-state total entry cost is SteadyState plus
	// ShimEntry (two separate processes in production — `omca env`'s own
	// invocation, then a later `codex`/`claude` PATH shim invocation — timed
	// separately here for exactly that reason, never summed into one Stats
	// value by this package itself).
	ShimEntry Stats
}

// hostPassConfig is everything one timed detect+observe+compile-or-reuse
// pass needs — the exact sequence cmd/omca/env.go's runEnv performs for one
// host, extracted here so MeasureSynthetic and MeasureRealEnvironment share
// one implementation rather than two driftable copies.
type hostPassConfig struct {
	Host             string
	Env              hostcontext.Environment
	WorktreeRoot     string
	GenerationsRoot  string
	WorktreeStateDir string
	OMCABinaryPath   string
}

// runHostPass performs DetectWorktree, DetectHost, Observe, EnsureGeneration,
// and SetCurrentGeneration for cfg.Host, in that order — production code,
// never mocked — and returns the total elapsed wall time plus the resulting
// HostDetection (Installed:false is a legitimate outcome: measuring an
// uninstalled host on this machine is a real, honestly-reported zero-work
// case, not an error).
func runHostPass(cfg hostPassConfig) (time.Duration, hostcontext.HostDetection, error) {
	start := time.Now()

	wt, err := hostcontext.DetectWorktree(cfg.WorktreeRoot)
	if err != nil {
		return 0, hostcontext.HostDetection{}, fmt.Errorf("perf: runHostPass: DetectWorktree: %w", err)
	}
	hd, err := hostcontext.DetectHost(stdcontext.Background(), cfg.Env, cfg.Host)
	if err != nil {
		return 0, hostcontext.HostDetection{}, fmt.Errorf("perf: runHostPass: DetectHost: %w", err)
	}
	if !hd.Installed {
		return time.Since(start), hd, nil
	}

	obs, err := observe.Observe(observe.Request{Detection: hd, WorktreeRoot: wt.Root})
	if err != nil {
		return 0, hostcontext.HostDetection{}, fmt.Errorf("perf: runHostPass: Observe: %w", err)
	}
	req := runtime.BootstrapRequest{
		Detection:      hd,
		Worktree:       wt,
		Observations:   obs,
		Now:            time.Now(),
		OMCABinaryPath: cfg.OMCABinaryPath,
	}
	gen, outputDir, err := runtime.EnsureGeneration(req, cfg.GenerationsRoot)
	if err != nil {
		return 0, hostcontext.HostDetection{}, fmt.Errorf("perf: runHostPass: EnsureGeneration: %w", err)
	}
	if err := runtime.SetCurrentGeneration(cfg.WorktreeStateDir, cfg.Host, outputDir, gen, hd, time.Now()); err != nil {
		return 0, hostcontext.HostDetection{}, fmt.Errorf("perf: runHostPass: SetCurrentGeneration: %w", err)
	}
	return time.Since(start), hd, nil
}

// runShimEntryPass times internal/shim.Build alone: the resolve-current-
// generation-and-prepare-exec-args work a PATH shim invocation performs,
// stopping short of Plan.Exec/syscall.Exec per this PR's safety boundary.
func runShimEntryPass(host string, environ []string) (time.Duration, error) {
	start := time.Now()
	if _, err := shim.Build(host, environ); err != nil {
		return 0, fmt.Errorf("perf: runShimEntryPass: %w", err)
	}
	return time.Since(start), nil
}

// MeasureConfig controls how many repeated samples MeasureSynthetic takes
// of each phase, and the synthetic fixture's size.
type MeasureConfig struct {
	FirstBootstrapReps int
	SteadyStateReps    int
	ShimEntryReps      int
	Fixture            SyntheticFixtureSize
}

// DefaultMeasureConfig is what `make perf` and the CI regression test both
// use unless a caller has a specific reason to override it. First-bootstrap
// sampling is deliberately smaller (5) than steady-state/shim-entry
// sampling (20): each first-bootstrap sample needs its own fresh fixture
// directory (real disk I/O building 30 fake MCP entries and 20 fake Skill
// files, itself not part of the timed region but still real wall-clock
// setup cost per iteration), while steady-state and shim-entry samples all
// reuse one already-built fixture.
var DefaultMeasureConfig = MeasureConfig{
	FirstBootstrapReps: 5,
	SteadyStateReps:    20,
	ShimEntryReps:      20,
	Fixture:            DefaultSyntheticFixtureSize,
}

// MeasureSynthetic runs the full FirstBootstrap/SteadyState/ShimEntry
// measurement against a hermetic synthetic fixture built fresh under
// baseDir (caller-supplied — normally a test's t.TempDir(), never resolved
// internally, matching internal/runtime.Bootstrap's own outputDir
// discipline). See doc.go for why this mode never touches real ~/.codex/
// ~/.claude and is the source of both the CI ceiling assertion and the
// evidence file's "synthetic-fixture" numbers.
func MeasureSynthetic(baseDir string, cfg MeasureConfig) (Result, error) {
	if !filepath.IsAbs(baseDir) {
		return Result{}, fmt.Errorf("perf: MeasureSynthetic: baseDir %q is not absolute", baseDir)
	}

	fakeBinDir, err := buildFakeHostBinary(filepath.Join(baseDir, "fake-bin"), "codex")
	if err != nil {
		return Result{}, err
	}
	fakeEnv := func(homeDir, codexHome string) hostcontext.Environment {
		return hostcontext.Environment{Vars: []string{
			"HOME=" + homeDir,
			"PATH=" + fakeBinDir,
			"CODEX_HOME=" + codexHome,
			"FAKE_HOST_VERSION_OUTPUT=codex-cli 0.144.5",
		}}
	}
	const fakeOMCABinaryPath = "/fake/omca-binary-for-synthetic-perf-measurement"

	// First bootstrap: cfg.FirstBootstrapReps independent passes, each into
	// its own never-before-seen fixture and generationsRoot, so
	// EnsureGeneration must compile every single time.
	firstSamples := make([]time.Duration, 0, cfg.FirstBootstrapReps)
	for i := 0; i < cfg.FirstBootstrapReps; i++ {
		iterRoot := filepath.Join(baseDir, "first-bootstrap", fmt.Sprintf("%03d", i))
		codexHome, worktreeRoot, err := buildSyntheticFixture(iterRoot, cfg.Fixture)
		if err != nil {
			return Result{}, err
		}
		d, hd, err := runHostPass(hostPassConfig{
			Host:             "codex",
			Env:              fakeEnv(filepath.Join(iterRoot, "home"), codexHome),
			WorktreeRoot:     worktreeRoot,
			GenerationsRoot:  filepath.Join(iterRoot, "generations"),
			WorktreeStateDir: filepath.Join(iterRoot, "state"),
			OMCABinaryPath:   fakeOMCABinaryPath,
		})
		if err != nil {
			return Result{}, fmt.Errorf("perf: MeasureSynthetic: first-bootstrap sample %d: %w", i, err)
		}
		if !hd.Installed {
			return Result{}, fmt.Errorf("perf: MeasureSynthetic: fake codex binary was not detected as installed (fixture bug)")
		}
		firstSamples = append(firstSamples, d)
	}

	// Steady state: one fixture, compiled once (untimed warm-up), then
	// cfg.SteadyStateReps repeated passes against the SAME generationsRoot
	// so EnsureGeneration hits the reuse fast path every time.
	steadyRoot := filepath.Join(baseDir, "steady-state")
	codexHome, worktreeRoot, err := buildSyntheticFixture(steadyRoot, cfg.Fixture)
	if err != nil {
		return Result{}, err
	}
	steadyCfg := hostPassConfig{
		Host:             "codex",
		Env:              fakeEnv(filepath.Join(steadyRoot, "home"), codexHome),
		WorktreeRoot:     worktreeRoot,
		GenerationsRoot:  filepath.Join(steadyRoot, "generations"),
		WorktreeStateDir: filepath.Join(steadyRoot, "state"),
		OMCABinaryPath:   fakeOMCABinaryPath,
	}
	if _, _, err := runHostPass(steadyCfg); err != nil {
		return Result{}, fmt.Errorf("perf: MeasureSynthetic: steady-state warm-up: %w", err)
	}
	steadySamples := make([]time.Duration, 0, cfg.SteadyStateReps)
	for i := 0; i < cfg.SteadyStateReps; i++ {
		d, _, err := runHostPass(steadyCfg)
		if err != nil {
			return Result{}, fmt.Errorf("perf: MeasureSynthetic: steady-state sample %d: %w", i, err)
		}
		steadySamples = append(steadySamples, d)
	}

	// Shim entry: reuse the steady-state fixture's already-established
	// "current" pointer. OMCA_SHIM_DIR is deliberately a nonexistent path —
	// shim.FilterOutDir treats an unresolvable exclude directory the same as
	// "nothing to exclude" (shim/resolve.go's CleanAbs doc comment), and
	// this measurement has no separate shim directory of its own to exclude
	// from fakeBinDir.
	shimEnviron := []string{
		"PATH=" + fakeBinDir,
		"OMCA_SHIM_DIR=" + filepath.Join(baseDir, "nonexistent-shim-dir"),
		"OMCA_STATE_DIR=" + steadyCfg.WorktreeStateDir,
	}
	shimSamples := make([]time.Duration, 0, cfg.ShimEntryReps)
	for i := 0; i < cfg.ShimEntryReps; i++ {
		d, err := runShimEntryPass("codex", shimEnviron)
		if err != nil {
			return Result{}, fmt.Errorf("perf: MeasureSynthetic: shim-entry sample %d: %w", i, err)
		}
		shimSamples = append(shimSamples, d)
	}

	return Result{
		FirstBootstrap: computeStats(firstSamples),
		SteadyState:    computeStats(steadySamples),
		ShimEntry:      computeStats(shimSamples),
	}, nil
}
