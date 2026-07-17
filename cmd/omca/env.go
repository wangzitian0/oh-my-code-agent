package main

import (
	stdcontext "context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	hostcontext "github.com/wangzitian0/oh-my-code-agent/internal/context"
	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
	"github.com/wangzitian0/oh-my-code-agent/internal/observe"
	"github.com/wangzitian0/oh-my-code-agent/internal/runtime"
)

// shimEntryNames are the two PATH entry points this project installs into
// every worktree's shim directory (docs/architecture/runtime.md §4: "codex
// # managed current generation", "claude # managed current generation").
// Both are always installed, regardless of whether that host is actually
// installed on this machine — an uninstalled host's shim entry simply fails
// informatively at invocation time (internal/shim.ResolveReal's "not found"
// error) rather than being silently absent, which would make "is codex
// managed" depend on install order.
var shimEntryNames = []string{"codex", "claude"}

// runEnv implements `omca env [--shell bash]`: the direnv integration point
// (docs/architecture/runtime.md §4, `eval "$(omca env --shell bash)"`).
// Only bash-syntax export statements are supported for M1 — this project's
// first implementation slice is explicitly "zsh and direnv"
// (docs/project/roadmap.md), and zsh, bash, and direnv's own .envrc
// evaluator all accept plain POSIX `export KEY=VALUE` syntax identically, so
// there is no fish/csv-specific quoting to gold-plate here.
//
// Exactly two things ever reach stdout: the export lines themselves, one
// per docs/architecture/runtime.md §4 variable plus the PATH line — nothing
// else, so `eval "$(omca env --shell bash)"` never chokes on stray output.
// Every diagnostic (which hosts were detected, which generations were
// compiled or reused) goes to stderr.
func runEnv(stdout, stderr io.Writer, args []string) int {
	shell := "bash"
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--shell":
			if i+1 >= len(args) {
				fmt.Fprintln(stderr, "omca: env: --shell requires a value")
				return 2
			}
			shell = args[i+1]
			i++
		default:
			fmt.Fprintf(stderr, "omca: env: unrecognized argument %q\n", args[i])
			return 2
		}
	}
	if shell != "bash" {
		fmt.Fprintf(stderr, "omca: env: --shell %q is not supported (only \"bash\", which zsh and direnv's .envrc evaluator both accept)\n", shell)
		return 2
	}

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(stderr, "omca: env: %v\n", err)
		return 1
	}
	wt, err := hostcontext.DetectWorktree(cwd)
	if err != nil {
		fmt.Fprintf(stderr, "omca: env: %v\n", err)
		return 1
	}

	stateRoot, err := realStateRoot()
	if err != nil {
		fmt.Fprintf(stderr, "omca: env: %v\n", err)
		return 1
	}
	worktreeStateDir := worktreeStateDirPath(stateRoot, wt.ID)
	shimDir := shimDirPath(worktreeStateDir)
	generationsDir := filepath.Join(worktreeStateDir, "generations")

	if err := installShims(shimDir); err != nil {
		fmt.Fprintf(stderr, "omca: env: installing PATH shims: %v\n", err)
		return 1
	}

	realEnv := hostcontext.RealEnvironment()
	detectEnv := envWithFilteredPath(realEnv, shimDir)

	now := time.Now()
	hostDetections := make([]hostcontext.HostDetection, 0, len(hostcontext.DetectedHostIDs))
	for _, host := range hostcontext.DetectedHostIDs {
		hd, err := hostcontext.DetectHost(stdcontext.Background(), detectEnv, host)
		if err != nil {
			fmt.Fprintf(stderr, "omca: env: detecting %s: %v\n", host, err)
			return 1
		}
		hostDetections = append(hostDetections, hd)

		if !hd.Installed {
			fmt.Fprintf(stderr, "omca: env: %s is not installed; skipping generation compile\n", host)
			continue
		}

		obs, err := observe.Observe(observe.Request{Detection: hd, WorktreeRoot: wt.Root})
		if err != nil {
			fmt.Fprintf(stderr, "omca: env: observing %s: %v\n", host, err)
			return 1
		}

		req := runtime.BootstrapRequest{Detection: hd, Worktree: wt, Observations: obs, Now: now}
		gen, outputDir, err := runtime.EnsureGeneration(req, generationsDir)
		if err != nil {
			fmt.Fprintf(stderr, "omca: env: compiling %s generation: %v\n", host, err)
			return 1
		}
		if err := runtime.SetCurrentGeneration(worktreeStateDir, host, outputDir, gen, hd, now); err != nil {
			fmt.Fprintf(stderr, "omca: env: recording current generation for %s: %v\n", host, err)
			return 1
		}
		fmt.Fprintf(stderr, "omca: env: %s -> generation %s (%s)\n", host, gen.Metadata.ID, outputDir)
	}

	contextID, err := computeContextID(wt, hostDetections)
	if err != nil {
		fmt.Fprintf(stderr, "omca: env: %v\n", err)
		return 1
	}

	printExports(stdout, exportVars{
		ContextID:  contextID,
		WorktreeID: wt.ID,
		RealHome:   realEnv.Get("HOME"),
		StateDir:   worktreeStateDir,
		ShimDir:    shimDir,
	})
	return 0
}

// exportVars is the exact docs/architecture/runtime.md §4 variable set
// printExports emits, gathered into one struct so the printing function has
// a single, obviously-complete source rather than five ad hoc parameters.
type exportVars struct {
	ContextID  string
	WorktreeID string
	RealHome   string
	StateDir   string
	ShimDir    string
}

// printExports writes exactly the export statements docs/architecture/
// runtime.md §4 shows, in the same order, to stdout.
func printExports(stdout io.Writer, v exportVars) {
	fmt.Fprintf(stdout, "export OMCA_CONTEXT_ID=%s\n", shellQuote(v.ContextID))
	fmt.Fprintf(stdout, "export OMCA_WORKTREE_ID=%s\n", shellQuote(v.WorktreeID))
	fmt.Fprintf(stdout, "export OMCA_REAL_HOME=%s\n", shellQuote(v.RealHome))
	fmt.Fprintf(stdout, "export OMCA_STATE_DIR=%s\n", shellQuote(v.StateDir))
	fmt.Fprintf(stdout, "export OMCA_SHIM_DIR=%s\n", shellQuote(v.ShimDir))
	fmt.Fprintln(stdout, `export PATH="$OMCA_SHIM_DIR:$PATH"`)
}

// shellQuote wraps s in single quotes, escaping any embedded single quote
// the POSIX-portable way ('\''): close the quote, emit an escaped quote,
// reopen. None of the values this command actually prints (worktree/
// generation IDs, resolved filesystem paths) are expected to contain a
// single quote, but eval'd shell output is exactly the place a defensive
// habit costs nothing and an unquoted or naively-quoted path containing a
// space or shell metacharacter would otherwise corrupt the caller's shell
// state silently.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// contextIDInputs is the deterministic subset of one `omca env` pass that
// OMCA_CONTEXT_ID digests: the worktree identity plus every first-party
// host's installation identity. Two separate `omca env` invocations against
// an unchanged worktree and unchanged host installations produce the
// identical OMCA_CONTEXT_ID; a host being installed/uninstalled, upgraded,
// or resolving to a different binary path changes it. This is the
// undocumented-beyond-its-name design decision this PR makes for
// OMCA_CONTEXT_ID (docs/architecture/runtime.md §4 names the variable but
// does not define its value): a content-addressed token `omca doctor` can
// recompute and compare against a shell's baked-in value to detect "this
// shell's managed environment has gone stale relative to the worktree it
// was evaluated in" — distinct from OMCA_WORKTREE_ID, which never changes
// across a host upgrade.
type contextIDInputs struct {
	Worktree string              `json:"worktree"`
	Hosts    []contextIDHostFact `json:"hosts"`
}

type contextIDHostFact struct {
	Host       string `json:"host"`
	Installed  bool   `json:"installed"`
	BinaryPath string `json:"binaryPath,omitempty"`
	Version    string `json:"version,omitempty"`
}

// computeContextID digests contextIDInputs built from wt and hosts (in
// hostcontext.DetectedHostIDs order, already how Detect/DetectHost callers
// iterate) using domain.CanonicalDigest, the one stable digest function
// this project uses everywhere.
func computeContextID(wt hostcontext.Worktree, hosts []hostcontext.HostDetection) (string, error) {
	facts := make([]contextIDHostFact, 0, len(hosts))
	for _, hd := range hosts {
		facts = append(facts, contextIDHostFact{Host: hd.Host, Installed: hd.Installed, BinaryPath: hd.BinaryPath, Version: hd.Version})
	}
	digest, err := domain.CanonicalDigest(contextIDInputs{Worktree: wt.ID, Hosts: facts})
	if err != nil {
		return "", fmt.Errorf("omca: computeContextID: %w", err)
	}
	return "context:" + digest, nil
}

// installShims ensures shimDir exists and contains a working codex and
// claude entry point, each a symlink to the currently running omca
// executable — refreshed unconditionally on every `omca env` call (cheap: a
// handful of syscalls), matching this PR's own instruction that `omca env`
// must "ensure $OMCA_SHIM_DIR actually contains working codex/claude shim
// entries... creating/refreshing them as part of this command."
//
// Symlink, not a hardlink or a copy: the issue's own design note picks
// this out as simplest, and it is what makes
// filepath.Base(os.Args[0]) == "codex"/"claude" true inside the shim
// process without cmd/omca needing to duplicate its own compiled binary —
// the OS resolves the symlink to run the same omca binary, but argv[0] (and
// so os.Args[0]) is set to the path the shell actually invoked, i.e. the
// symlink's own name, not the resolved target's.
func installShims(shimDir string) error {
	if err := os.MkdirAll(shimDir, 0o755); err != nil {
		return fmt.Errorf("omca: installShims: %w", err)
	}
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("omca: installShims: resolving the running omca binary: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}

	for _, name := range shimEntryNames {
		linkPath := filepath.Join(shimDir, name)
		// Idempotent refresh: remove whatever is there (a prior symlink, a
		// stale copy, or nothing) and recreate. os.Remove on a nonexistent
		// path is reported via the returned error, which is intentionally
		// ignored here — "nothing to remove" is not a failure.
		_ = os.Remove(linkPath)
		if err := os.Symlink(exe, linkPath); err != nil {
			return fmt.Errorf("omca: installShims: %s: %w", name, err)
		}
	}
	return nil
}
