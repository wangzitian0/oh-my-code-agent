package shim

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/wangzitian0/oh-my-code-agent/internal/runtime"
)

// hostForInvokedName maps the shim's own two entry-point basenames to the
// canonical host ID docs/ontology/README.md's Host Registry uses. This is
// the shim's own tiny dispatch table, deliberately not sourced from
// internal/context.BinaryName's inverse: importing internal/context here
// would pull host-detection/version-probing scope into this package for a
// two-entry lookup (see doc.go's "why this is its own package" section).
var hostForInvokedName = map[string]string{
	"codex":  "codex",
	"claude": "claude-code",
}

// surfaceCLI is the one surface every HostDetection this project produces
// today reports (internal/context/host.go's DetectHost sets Surface: "cli"
// unconditionally). The shim hardcodes it rather than threading a surface
// value through another OMCA_* environment variable, because there is
// currently no second surface for a generation directory to disambiguate
// between; a future surface would need this constant promoted to an actual
// input.
const surfaceCLI = "cli"

// IsShimInvocation reports whether invokedName — normally
// filepath.Base(os.Args[0]) — names one of this package's two shim entry
// points. cmd/omca's main() calls this before anything else, to decide
// whether to dispatch into shim mode instead of the normal `omca <command>`
// switch (the "multi-call binary" design issue #14 recommends: the same
// compiled omca binary serves as both the management CLI and, invoked
// through a symlink named codex/claude, the PATH shim for that host).
func IsShimInvocation(invokedName string) bool {
	_, ok := hostForInvokedName[invokedName]
	return ok
}

// Plan is everything a shim invocation needs to launch the real host binary
// with generation environment injected, computed once by Build and then
// handed to Plan.Exec. Separating "decide what to run" (Build, pure aside
// from the two filesystem lookups it needs) from "actually replace this
// process" (Exec, which never returns on success) is what makes the
// decision logic unit-testable without needing a subprocess for every case
// — only Exec itself needs the subprocess-based tests
// (cmd/omca/shim_test.go), because syscall.Exec cannot be safely called
// from inside the `go test` process itself.
type Plan struct {
	// Host is the canonical host ID ("codex" or "claude-code").
	Host string
	// RealBinaryPath is the non-shim binary ResolveReal found.
	RealBinaryPath string
	// NativeHomeEnvVar is the environment variable Exec sets to
	// NativeHomeDir before exec'ing RealBinaryPath (runtime.NativeHomeEnvVar).
	NativeHomeEnvVar string
	// NativeHomeDir is the writable, worktree-scoped directory Exec points
	// Host's native-home variable at (runtime.MutableNativeHomeDir) --
	// pre-synced by Build with the current generation's compiled config
	// artifacts (runtime.SyncMutableNativeHome), but NOT itself inside
	// GenerationDir: GenerationDir's own native-home directory
	// (runtime.NativeHomeDirName) is read-only (readonly.go), and Host
	// stores mutable runtime state (sessions, local databases, trust
	// decisions) at whatever path its native-home variable resolves to, so
	// pointing it directly at a read-only directory fails the moment Host
	// tries to write.
	NativeHomeDir string
	// GenerationID is the compiled generation's metadata.id, best-effort —
	// empty if the current generation's manifest could not be re-read (see
	// Build's doc comment). When non-empty, Exec sets OMCA_RUN_ID to it
	// (docs/architecture/runtime.md §7.1).
	GenerationID string
	// GenerationDir is the resolved "current" generation directory for Host
	// under OMCA_STATE_DIR.
	GenerationDir string
	// VirtualHomeDir is the current generation's virtual-home directory for
	// Host (runtime.VirtualHomeDirName, joined under GenerationDir, exactly
	// parallel to NativeHomeDir's own construction). Exec sets HOME to this
	// directory before exec'ing RealBinaryPath (docs/architecture/
	// runtime.md §7.1) -- an empty, compiler-controlled directory that is
	// never the caller's real home, so a native discovery path a host
	// resolves relative to HOME (e.g. $HOME/.agents/skills,
	// internal/context/host.go's codexNativeHomes/claudeNativeHomes) finds
	// nothing real there.
	VirtualHomeDir string
	// RealHomeDir is the caller's real HOME, read from environ, that Exec
	// records as OMCA_REAL_HOME on the exec'd process (docs/architecture/
	// runtime.md §7.1) -- purely informational for the launched process (and
	// anything it spawns) to recover the identity-shared real home it was
	// launched from; Exec itself never reads it back.
	RealHomeDir string
	// InterpreterPath, when non-empty, is the real, concrete interpreter
	// binary that RealBinaryPath's own "#!/usr/bin/env <name>" shebang line
	// names -- already resolved past any asdf-shim indirection of its own,
	// exactly like RealBinaryPath itself was. Set only when RealBinaryPath
	// is such a script and <name> was resolvable; empty otherwise (the
	// common case: RealBinaryPath is an ordinary native binary with no
	// shebang, so there is nothing to resolve). When set, Exec invokes
	// InterpreterPath directly with RealBinaryPath as its first argument,
	// bypassing the OS's own shebang-driven PATH lookup of <name> entirely
	// -- the one piece of exec behavior that would otherwise depend on a
	// resolvable HOME at exec time (an asdf-managed <name>'s own shim
	// dispatch needs one), which Exec is about to virtualize. This closes
	// the same asdf+virtualized-HOME class of failure issue #69 fixed for
	// RealBinaryPath itself, one interpreter layer deeper: a real, observed
	// case is codex's own asdf-installed binary being a
	// "#!/usr/bin/env node" script, where node is *also* asdf-managed.
	InterpreterPath string
}

// getEnv returns the value of the last environ entry named key
// ("KEY=VALUE" parsing; last-occurrence-wins, matching
// internal/context.Environment.Get's identical semantics), or "" if absent.
// Duplicated rather than imported from internal/context for the same
// "keep this package's own dependency surface minimal" reason doc.go gives
// for not importing internal/context at all.
func getEnv(environ []string, key string) string {
	prefix := key + "="
	value := ""
	for _, kv := range environ {
		if rest, ok := strings.CutPrefix(kv, prefix); ok {
			value = rest
		}
	}
	return value
}

// Build resolves invokedName ("codex" or "claude") into a Plan using only
// environ — the shim's own received environment, normally os.Environ() —
// and two filesystem lookups: the real binary (ResolveReal, non-recursive
// per doc.go) and the worktree's current generation directory for this host
// (runtime.CurrentGenerationDir, reading the pointer `omca env`/`omca run`
// already established under OMCA_STATE_DIR). It performs no host-version
// detection and no observation of its own — by the time a shim invocation
// happens, that work was already done once, when the generation was
// compiled.
//
// Every failure mode here is a clear, actionable error (never a silent
// fallback to an unmanaged launch): HOME or OMCA_STATE_DIR unset, no
// "current" pointer for this host, or a pointer whose generation directory
// has no native-home or virtual-home directory all mean "run `omca env`
// again" (or, for HOME specifically, "this shell's environment is missing
// HOME entirely"), and Build says so.
func Build(invokedName string, environ []string) (Plan, error) {
	host, ok := hostForInvokedName[invokedName]
	if !ok {
		return Plan{}, fmt.Errorf("shim: Build: %q is not a recognized OMCA shim entry point (only codex, claude)", invokedName)
	}

	envVar, err := runtime.NativeHomeEnvVar(host)
	if err != nil {
		return Plan{}, fmt.Errorf("shim: Build: %w", err)
	}
	homeDirName, err := runtime.NativeHomeDirName(host)
	if err != nil {
		return Plan{}, fmt.Errorf("shim: Build: %w", err)
	}

	shimDir := getEnv(environ, "OMCA_SHIM_DIR")
	realPath, err := ResolveReal(invokedName, getEnv(environ, "PATH"), shimDir)
	if err != nil {
		return Plan{}, fmt.Errorf("shim: Build: %w", err)
	}

	// realPath itself can be another shim -- specifically, an asdf-managed
	// one (issue #69): ResolveReal's own PATH search has no way to tell an
	// ordinary binary from an asdf shim script, and an asdf shim's dispatch
	// needs a real, resolvable HOME to find asdf's own
	// ~/.tool-versions-derived state, which Exec (exec.go) is about to
	// override to this generation's virtual-home directory -- exactly the
	// override docs/architecture/runtime.md §7.1 requires and this package
	// cannot skip. ResolveASDFShimTarget resolves straight past the shim to
	// the concrete, per-version real binary asdf's own `asdf reshim` step
	// already recorded, entirely without invoking asdf or depending on
	// HOME, so Plan.RealBinaryPath below can point directly at it.
	if IsASDFShim(realPath) {
		resolved, asdfErr := ResolveASDFShimTarget(realPath)
		if asdfErr != nil {
			return Plan{}, fmt.Errorf("shim: Build: %s resolves to %s, a path under an asdf shims directory, but isolated mode could not resolve it to a concrete asdf-installed binary: %w -- install it outside asdf (e.g. a plain global npm/brew install, not asdf-managed), or use `omca run --mode native` instead of the PATH shim", invokedName, realPath, asdfErr)
		}
		realPath = resolved
	}

	// realPath can still carry its own, one-layer-deeper HOME dependency
	// even now: a "#!/usr/bin/env <name>" script (e.g. codex's own
	// asdf-installed binary is exactly this -- a Node entry point) defers
	// resolving <name> to the OS's own shebang handling AT EXEC TIME, using
	// whatever PATH is in the exec'd environment -- and if <name> is
	// *itself* asdf-managed (node commonly is, on the same machine that
	// triggered issue #69), that lookup needs the same real, resolvable
	// HOME the outer asdf shim needed, which Exec is about to virtualize.
	// Resolve <name> ourselves, now, using this process's own real PATH
	// (before virtualization), so Exec can invoke the real interpreter
	// directly and this second layer never depends on HOME either.
	interpreterPath := ""
	if name, isEnvIndirect := ShebangEnvIndirectInterpreter(realPath); isEnvIndirect {
		if interpCandidate, resolveErr := ResolveReal(name, getEnv(environ, "PATH"), shimDir); resolveErr == nil {
			if IsASDFShim(interpCandidate) {
				if resolved, asdfErr := ResolveASDFShimTarget(interpCandidate); asdfErr == nil {
					interpCandidate = resolved
				}
				// asdfErr != nil: leave interpCandidate as the unresolved
				// asdf shim path rather than failing Build outright -- Exec
				// will still try it, and a genuine failure there surfaces
				// the real OS exec error instead of this function silently
				// declining to even attempt the common case.
			}
			interpreterPath = interpCandidate
		}
		// resolveErr != nil: <name> is not found in PATH outside the OMCA
		// shim dir at all -- leave interpreterPath empty and fall through to
		// today's existing behavior (exec realPath directly). This keeps an
		// interpreter Build cannot resolve a soft, "maybe not immune to this
		// class of bug" case rather than a hard failure.
	}

	// HOME is required, the same fail-closed way OMCA_STATE_DIR is below:
	// Exec must always set HOME to the generation's virtual-home directory
	// (docs/architecture/runtime.md §7.1), and it must always record the
	// caller's real HOME as OMCA_REAL_HOME -- a shim invoked with no HOME at
	// all (e.g. a stripped-down environment, or a caller that forgot
	// `eval "$(omca env)"`) has no real value to preserve, so this fails
	// loudly rather than silently exec'ing with an empty/missing
	// OMCA_REAL_HOME that would be indistinguishable from "this really is
	// the real home."
	realHome := getEnv(environ, "HOME")
	if realHome == "" {
		return Plan{}, fmt.Errorf("shim: Build: HOME is not set in this shell's environment; run `eval \"$(omca env)\"` (usually via direnv's .envrc) before invoking %s", invokedName)
	}

	stateDir := getEnv(environ, "OMCA_STATE_DIR")
	if stateDir == "" {
		return Plan{}, fmt.Errorf("shim: Build: OMCA_STATE_DIR is not set in this shell's environment; run `eval \"$(omca env)\"` (usually via direnv's .envrc) before invoking %s", invokedName)
	}
	genDir, err := runtime.CurrentGenerationDir(stateDir, host)
	if err != nil {
		return Plan{}, fmt.Errorf("shim: Build: no managed generation found for %s under %s: %w (run `omca env` again)", host, stateDir, err)
	}

	// Best-effort: OMCA_RUN_ID is documentation/diagnostic value
	// (docs/architecture/runtime.md §7.1), not a safety gate — the
	// directory-existence check on nativeHomeDir below is what actually
	// guards against launching against a broken generation, so a manifest
	// this package cannot re-read does not block the launch.
	genID := ""
	if gen, readErr := runtime.ReadGenerationManifest(genDir); readErr == nil {
		genID = gen.Metadata.ID
	}

	nativeHomeDir := filepath.Join(genDir, "hosts", host, surfaceCLI, homeDirName)
	if info, statErr := os.Stat(nativeHomeDir); statErr != nil || !info.IsDir() {
		return Plan{}, fmt.Errorf("shim: Build: current generation %s for %s has no %s directory at %s; run `omca env` again", genDir, host, homeDirName, nativeHomeDir)
	}

	// mutableHomeDir is what Plan.NativeHomeDir actually resolves to below --
	// nativeHomeDir itself is read-only (readonly.go), and a host launched
	// with its native-home variable pointing directly at it fails the moment
	// it tries to write its own runtime state there (runtime.
	// MutableNativeHomeDir's doc comment has the full rationale and the real
	// observed failure this fixes).
	mutableHomeDir, err := runtime.MutableNativeHomeDir(stateDir, host, surfaceCLI)
	if err != nil {
		return Plan{}, fmt.Errorf("shim: Build: %w", err)
	}
	if err := runtime.SyncMutableNativeHome(nativeHomeDir, mutableHomeDir); err != nil {
		return Plan{}, fmt.Errorf("shim: Build: %w", err)
	}

	virtualHomeDir := filepath.Join(genDir, "hosts", host, surfaceCLI, runtime.VirtualHomeDirName)
	if info, statErr := os.Stat(virtualHomeDir); statErr != nil || !info.IsDir() {
		return Plan{}, fmt.Errorf("shim: Build: current generation %s for %s has no %s directory at %s; run `omca env` again", genDir, host, runtime.VirtualHomeDirName, virtualHomeDir)
	}

	return Plan{
		Host:             host,
		RealBinaryPath:   realPath,
		NativeHomeEnvVar: envVar,
		NativeHomeDir:    mutableHomeDir,
		GenerationID:     genID,
		GenerationDir:    genDir,
		VirtualHomeDir:   virtualHomeDir,
		RealHomeDir:      realHome,
		InterpreterPath:  interpreterPath,
	}, nil
}
