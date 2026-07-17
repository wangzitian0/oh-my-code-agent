package perf

import (
	stdcontext "context"
	"fmt"
	"path/filepath"
	"time"

	hostcontext "github.com/wangzitian0/oh-my-code-agent/internal/context"
	"github.com/wangzitian0/oh-my-code-agent/internal/mcp"
	"github.com/wangzitian0/oh-my-code-agent/internal/runtime"
)

// realEnvironmentOMCABinaryPath is the placeholder OMCA binary path this
// measurement's compiled generations register as their MCP server command.
// It does not need to resolve to a real file (internal/runtime never
// validates the path's existence — it is written into the generated config
// verbatim, exactly as cmd/omca/statedir.go's resolveOMCABinaryPath would
// supply a real one in production); a fixed, obviously-synthetic value
// keeps this measurement's output self-documenting.
const realEnvironmentOMCABinaryPath = "/usr/local/bin/omca-perf-measurement"

// RealEnvironmentConfig controls MeasureRealEnvironment.
type RealEnvironmentConfig struct {
	// WorktreeRoot is the real worktree whose repository-scope Instructions/
	// Skills/MCP sources are observed alongside each host's real native
	// user-global sources — caller-supplied (normally this repository's own
	// root), never derived internally, matching every other "explicit
	// inputs" package in this module.
	WorktreeRoot string
	// FirstBootstrapReps / SteadyStateReps / ShimEntryReps are smaller than
	// MeasureConfig's synthetic defaults: each first-bootstrap and
	// steady-state sample here pays a REAL subprocess `--version` probe
	// (internal/context.DetectHost) against the real installed binary,
	// which — for an asdf-shimmed or Node-wrapped install, see
	// fixtures/README.md's own acquisition-method notes — can be
	// meaningfully slower than a synthetic fake-script probe; keeping this
	// measurement fast enough to run routinely matters more than a large
	// sample size for a number whose whole point is "does real native state
	// exist, and roughly how much."
	FirstBootstrapReps int
	SteadyStateReps    int
	ShimEntryReps      int
}

// DefaultRealEnvironmentConfig is what `make perf` uses unless overridden.
var DefaultRealEnvironmentConfig = RealEnvironmentConfig{
	FirstBootstrapReps: 3,
	SteadyStateReps:    5,
	ShimEntryReps:      5,
}

// RealEnvironmentHostResult is one host's real-environment measurement.
type RealEnvironmentHostResult struct {
	// Host is the canonical host ID.
	Host string
	// Installed reports whether this real machine actually has Host
	// installed. false means every timing/count field below is zero — this
	// is reported honestly, per this project's own "UNKNOWN is safer than a
	// guessed number" ethos, rather than a fabricated measurement.
	Installed bool
	// Detail explains an uninstalled host, or (when Installed) names the
	// real binary path/version this measurement actually ran against, so a
	// report reader can verify what was measured.
	Detail string

	FirstBootstrap Stats
	SteadyState    Stats
	ShimEntry      Stats

	// ExcludedMCPServers / ExcludedSkills / ContextCost are this real
	// machine's actual native exclusion counts and estimated context-cost
	// delta — internal/mcp.CountUserExclusions/EstimateContextCost applied
	// to the real generation this measurement compiled from this machine's
	// real native ~/.codex or ~/.claude, reusing the identical method
	// omca_status itself uses (never a second, driftable computation).
	ExcludedMCPServers int
	ExcludedSkills     int
	ContextCost        mcp.ContextCostEstimate
}

// RealEnvironmentResult is MeasureRealEnvironment's complete output.
type RealEnvironmentResult struct {
	Hosts []RealEnvironmentHostResult
}

// MeasureRealEnvironment runs the identical FirstBootstrap/SteadyState/
// ShimEntry measurement MeasureSynthetic runs, but against THIS machine's
// actual, real installed hosts and real native user-global configuration —
// see doc.go for the full safety rationale (read-only DetectHost/Observe
// against real state is explicitly sanctioned; every write lands under
// baseDir, never the real $XDG_STATE_HOME/omca). A host this machine does
// not have installed is reported as such, not skipped silently or
// fabricated — every entry in the returned Hosts slice names every host
// hostcontext.DetectedHostIDs knows about, Installed or not.
func MeasureRealEnvironment(baseDir string, cfg RealEnvironmentConfig) (RealEnvironmentResult, error) {
	if !filepath.IsAbs(baseDir) {
		return RealEnvironmentResult{}, fmt.Errorf("perf: MeasureRealEnvironment: baseDir %q is not absolute", baseDir)
	}
	if cfg.WorktreeRoot == "" {
		return RealEnvironmentResult{}, fmt.Errorf("perf: MeasureRealEnvironment: WorktreeRoot is required")
	}

	realEnv := hostcontext.RealEnvironment()
	result := RealEnvironmentResult{}

	for _, host := range hostcontext.DetectedHostIDs {
		hostResult, err := measureRealEnvironmentHost(baseDir, host, realEnv, cfg)
		if err != nil {
			return RealEnvironmentResult{}, err
		}
		result.Hosts = append(result.Hosts, hostResult)
	}
	return result, nil
}

func measureRealEnvironmentHost(baseDir, host string, realEnv hostcontext.Environment, cfg RealEnvironmentConfig) (RealEnvironmentHostResult, error) {
	hd, err := hostcontext.DetectHost(stdcontext.Background(), realEnv, host)
	if err != nil {
		return RealEnvironmentHostResult{}, fmt.Errorf("perf: MeasureRealEnvironment: DetectHost(%s): %w", host, err)
	}
	if !hd.Installed {
		return RealEnvironmentHostResult{Host: host, Installed: false, Detail: fmt.Sprintf("%s is not installed on this machine", host)}, nil
	}

	hostRoot := filepath.Join(baseDir, host)

	firstSamples := make([]time.Duration, 0, cfg.FirstBootstrapReps)
	for i := 0; i < cfg.FirstBootstrapReps; i++ {
		iterRoot := filepath.Join(hostRoot, "first-bootstrap", fmt.Sprintf("%03d", i))
		d, _, err := runHostPass(hostPassConfig{
			Host:             host,
			Env:              realEnv,
			WorktreeRoot:     cfg.WorktreeRoot,
			GenerationsRoot:  filepath.Join(iterRoot, "generations"),
			WorktreeStateDir: filepath.Join(iterRoot, "state"),
			OMCABinaryPath:   realEnvironmentOMCABinaryPath,
		})
		if err != nil {
			return RealEnvironmentHostResult{}, fmt.Errorf("perf: MeasureRealEnvironment: %s first-bootstrap sample %d: %w", host, i, err)
		}
		firstSamples = append(firstSamples, d)
	}

	steadyRoot := filepath.Join(hostRoot, "steady-state")
	steadyCfg := hostPassConfig{
		Host:             host,
		Env:              realEnv,
		WorktreeRoot:     cfg.WorktreeRoot,
		GenerationsRoot:  filepath.Join(steadyRoot, "generations"),
		WorktreeStateDir: filepath.Join(steadyRoot, "state"),
		OMCABinaryPath:   realEnvironmentOMCABinaryPath,
	}
	if _, _, err := runHostPass(steadyCfg); err != nil {
		return RealEnvironmentHostResult{}, fmt.Errorf("perf: MeasureRealEnvironment: %s steady-state warm-up: %w", host, err)
	}
	steadySamples := make([]time.Duration, 0, cfg.SteadyStateReps)
	for i := 0; i < cfg.SteadyStateReps; i++ {
		d, _, err := runHostPass(steadyCfg)
		if err != nil {
			return RealEnvironmentHostResult{}, fmt.Errorf("perf: MeasureRealEnvironment: %s steady-state sample %d: %w", host, i, err)
		}
		steadySamples = append(steadySamples, d)
	}

	binName, err := hostcontext.BinaryName(host)
	if err != nil {
		return RealEnvironmentHostResult{}, fmt.Errorf("perf: MeasureRealEnvironment: %w", err)
	}
	shimEnviron := []string{
		"HOME=" + realEnv.Get("HOME"),
		"PATH=" + filepath.Dir(hd.BinaryPath),
		"OMCA_SHIM_DIR=" + filepath.Join(baseDir, "nonexistent-shim-dir-"+host),
		"OMCA_STATE_DIR=" + steadyCfg.WorktreeStateDir,
	}
	shimSamples := make([]time.Duration, 0, cfg.ShimEntryReps)
	for i := 0; i < cfg.ShimEntryReps; i++ {
		d, err := runShimEntryPass(binName, shimEnviron)
		if err != nil {
			return RealEnvironmentHostResult{}, fmt.Errorf("perf: MeasureRealEnvironment: %s shim-entry sample %d: %w", host, i, err)
		}
		shimSamples = append(shimSamples, d)
	}

	genDir, err := runtime.CurrentGenerationDir(steadyCfg.WorktreeStateDir, host)
	if err != nil {
		return RealEnvironmentHostResult{}, fmt.Errorf("perf: MeasureRealEnvironment: %s: reading back the compiled generation: %w", host, err)
	}
	gen, err := runtime.ReadGenerationManifest(genDir)
	if err != nil {
		return RealEnvironmentHostResult{}, fmt.Errorf("perf: MeasureRealEnvironment: %s: %w", host, err)
	}
	excludedMCP, excludedSkills := mcp.CountUserExclusions(gen)

	return RealEnvironmentHostResult{
		Host:               host,
		Installed:          true,
		Detail:             fmt.Sprintf("%s %s at %s", host, hd.Version, hd.BinaryPath),
		FirstBootstrap:     computeStats(firstSamples),
		SteadyState:        computeStats(steadySamples),
		ShimEntry:          computeStats(shimSamples),
		ExcludedMCPServers: excludedMCP,
		ExcludedSkills:     excludedSkills,
		ContextCost:        mcp.EstimateContextCost(excludedMCP, excludedSkills),
	}, nil
}
