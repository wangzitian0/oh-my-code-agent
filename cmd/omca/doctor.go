package main

import (
	stdcontext "context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	hostcontext "github.com/wangzitian0/oh-my-code-agent/internal/context"
	"github.com/wangzitian0/oh-my-code-agent/internal/observe"
	"github.com/wangzitian0/oh-my-code-agent/internal/runtime"
	"github.com/wangzitian0/oh-my-code-agent/internal/shim"
)

// doctorStatus is one finding's severity. "No false green" (this PR's
// quality bar) means every check must be able to report FAIL, not only
// OK/WARN — a check that can never fail is not actually verifying anything.
type doctorStatus string

const (
	statusOK   doctorStatus = "OK"
	statusWarn doctorStatus = "WARN"
	statusFail doctorStatus = "FAIL"
)

// doctorFinding is one `omca doctor` check's result.
type doctorFinding struct {
	Check  string
	Status doctorStatus
	Detail string
}

// direnvStatusTimeout hard-bounds the one subprocess `omca doctor` ever
// runs (`direnv status`), matching internal/context/host.go's detectTimeout
// and internal/qualify's invokeTimeout precedent: a hang must never be
// mistaken for a slow but passing check. A var, not a const, solely so
// TestRunDoctor_DirenvApproval_StatusTimesOut can shrink it and prove the
// timeout-reporting branch with a fast, deterministic test instead of
// either waiting out the real 10s or leaving that branch unverified.
var direnvStatusTimeout = 10 * time.Second

// runDoctor implements `omca doctor` (issue #14): PATH bypass, missing
// direnv approval, stale generation, host binary moved since qualification,
// and whether the CURRENT `omca doctor` process's own shell session is
// itself managed or unmanaged. Every check is independent — one check's
// failure to run (e.g. direnv not installed) never suppresses the others,
// matching the same "degrade honestly, do not go silent on a working part
// of the pipeline" stance cmd/omca/context.go documents for Knowledge Pack
// resolution failures.
//
// Exit code is 0 unless at least one finding is statusFail — WARN findings
// are still printed but do not fail the command, since several of them
// describe expected states (a host simply not installed, a worktree that
// has never run `omca env` yet) rather than problems.
func runDoctor(stdout, stderr io.Writer) int {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(stderr, "omca: doctor: %v\n", err)
		return 1
	}
	wt, err := hostcontext.DetectWorktree(cwd)
	if err != nil {
		fmt.Fprintf(stderr, "omca: doctor: %v\n", err)
		return 1
	}
	stateRoot, err := realStateRoot()
	if err != nil {
		fmt.Fprintf(stderr, "omca: doctor: %v\n", err)
		return 1
	}
	worktreeStateDir := worktreeStateDirPath(stateRoot, wt.ID)
	shimDir := shimDirPath(worktreeStateDir)
	realEnv := hostcontext.RealEnvironment()

	var findings []doctorFinding
	findings = append(findings, checkSessionManaged(realEnv, wt))
	for _, host := range hostcontext.DetectedHostIDs {
		binName, _ := hostcontext.BinaryName(host) // hostcontext.DetectedHostIDs are always known to BinaryName
		findings = append(findings, checkPathBypass(host, binName, shimDir))
		findings = append(findings, checkGenerationFreshness(host, wt, worktreeStateDir, realEnv, shimDir)...)
	}
	findings = append(findings, checkDirenvApproval(wt))

	fmt.Fprintf(stdout, "omca doctor: worktree %s (%s)\n\n", wt.ID, wt.Root)
	failed := false
	for _, f := range findings {
		if f.Status == statusFail {
			failed = true
		}
		fmt.Fprintf(stdout, "[%-4s] %s: %s\n", f.Status, f.Check, f.Detail)
	}
	if failed {
		return 1
	}
	return 0
}

// checkSessionManaged addresses issue #14's "doctor distinguishes managed
// vs unmanaged sessions": is the shell that invoked `omca doctor` itself
// one `omca env`/`omca run --mode isolated` already set up? It looks only
// at the current process's own environment (realEnv, i.e. os.Environ()),
// never at PATH resolution — that is checkPathBypass's job.
func checkSessionManaged(env hostcontext.Environment, wt hostcontext.Worktree) doctorFinding {
	const check = "session"
	contextID := env.Get("OMCA_CONTEXT_ID")
	worktreeID := env.Get("OMCA_WORKTREE_ID")
	if contextID == "" || worktreeID == "" {
		return doctorFinding{Check: check, Status: statusWarn, Detail: "this shell has no OMCA_CONTEXT_ID/OMCA_WORKTREE_ID set — the current session is UNMANAGED (not evaluated via `omca env`, typically through direnv)"}
	}
	if worktreeID != wt.ID {
		return doctorFinding{Check: check, Status: statusWarn, Detail: fmt.Sprintf("OMCA_WORKTREE_ID=%s does not match the worktree detected from the current directory (%s) — this shell's managed environment belongs to a different worktree", worktreeID, wt.ID)}
	}
	return doctorFinding{Check: check, Status: statusOK, Detail: fmt.Sprintf("managed: this shell was evaluated for worktree %s (context %s)", wt.ID, contextID)}
}

// checkPathBypass resolves where binName would actually run from given the
// real ambient PATH (exec.LookPath — deliberately NOT internal/shim.
// ResolveReal's own shim-dir-stripped lookup, since this check's entire
// point is "what does a bare `codex`/`claude` typed in this shell actually
// resolve to right now") and reports whether that is the OMCA shim or a
// native binary that bypasses management entirely (issue #14's "PATH
// bypass" AC).
//
// Both the resolved binary's directory and shimDir are canonicalized via
// shim.CleanAbs (symlink-evaluated, not just filepath.Abs) before
// comparing: on macOS in particular, a shim/state directory that lives
// under /tmp resolves through /private/tmp, so a bare filepath.Abs
// comparison would falsely report a PATH bypass even when the shim is
// genuinely being used (Copilot review finding on this PR).
func checkPathBypass(host, binName, shimDir string) doctorFinding {
	check := "path-bypass:" + host
	resolved, err := exec.LookPath(binName)
	if err != nil {
		return doctorFinding{Check: check, Status: statusWarn, Detail: fmt.Sprintf("%s (%s) is not on PATH", host, binName)}
	}
	resolvedDir := shim.CleanAbs(filepath.Dir(resolved))
	if resolvedDir == shim.CleanAbs(shimDir) {
		return doctorFinding{Check: check, Status: statusOK, Detail: fmt.Sprintf("%s resolves to the OMCA shim (%s): managed", binName, resolved)}
	}
	return doctorFinding{Check: check, Status: statusFail, Detail: fmt.Sprintf("PATH bypass: %s resolves to %s, which is not the OMCA shim (%s) — direnv is not active, or the shim directory is not first on PATH; direct invocations of %s are UNMANAGED", binName, resolved, shimDir, binName)}
}

// checkGenerationFreshness covers issue #14's remaining two AC checks for
// one host: "stale generation" (recompute runtime.GenerationID from fresh
// detect+observe, compare against the generation the "current" pointer
// names) and "host binary moved since qualification" (compare the
// CurrentRecord sidecar's recorded binary path against a fresh
// resolution). It returns multiple findings (or a single one, for a host
// that is not installed or has no compiled generation yet) rather than one
// combined finding, so a caller/CI can see each condition independently.
func checkGenerationFreshness(host string, wt hostcontext.Worktree, worktreeStateDir string, env hostcontext.Environment, shimDir string) []doctorFinding {
	detectEnv := envWithFilteredPath(env, shimDir)
	hd, err := hostcontext.DetectHost(stdcontext.Background(), detectEnv, host)
	if err != nil {
		return []doctorFinding{{Check: "generation:" + host, Status: statusFail, Detail: err.Error()}}
	}
	if !hd.Installed {
		return []doctorFinding{{Check: "generation:" + host, Status: statusOK, Detail: fmt.Sprintf("%s is not installed; nothing to manage", host)}}
	}

	genDir, cgErr := runtime.CurrentGenerationDir(worktreeStateDir, host)
	if cgErr != nil {
		if os.IsNotExist(cgErr) {
			return []doctorFinding{{Check: "generation:" + host, Status: statusWarn, Detail: fmt.Sprintf("%s has no compiled generation yet in this worktree — run `omca env` or `omca run %s`", host, host)}}
		}
		// Anything other than "no current pointer yet" (e.g. the "current"
		// entry exists but isn't a readable symlink) is real corruption,
		// not an expected not-managed-yet state, and must not be
		// downgraded to a WARN (Copilot review finding on this PR).
		return []doctorFinding{{Check: "generation:" + host, Status: statusFail, Detail: fmt.Sprintf("%s's current-generation pointer in %s is corrupt: %v", host, worktreeStateDir, cgErr)}}
	}

	var findings []doctorFinding
	findings = append(findings, checkStaleGeneration(host, wt, genDir, hd))
	findings = append(findings, checkBinaryMoved(host, worktreeStateDir, hd))
	return findings
}

// checkStaleGeneration is issue #14's "stale generation" AC.
func checkStaleGeneration(host string, wt hostcontext.Worktree, genDir string, hd hostcontext.HostDetection) doctorFinding {
	check := "stale-generation:" + host
	gen, readErr := runtime.ReadGenerationManifest(genDir)
	if readErr != nil {
		return doctorFinding{Check: check, Status: statusFail, Detail: fmt.Sprintf("current generation manifest for %s at %s is unreadable: %v", host, genDir, readErr)}
	}
	obs, obsErr := observe.Observe(observe.Request{Detection: hd, WorktreeRoot: wt.Root})
	if obsErr != nil {
		return doctorFinding{Check: check, Status: statusFail, Detail: fmt.Sprintf("re-observing %s failed: %v", host, obsErr)}
	}
	freshID, idErr := runtime.GenerationID(runtime.BootstrapRequest{Detection: hd, Worktree: wt, Observations: obs, Now: time.Now()})
	if idErr != nil {
		return doctorFinding{Check: check, Status: statusFail, Detail: fmt.Sprintf("recomputing %s's generation ID failed: %v", host, idErr)}
	}
	if freshID != gen.Metadata.ID {
		return doctorFinding{Check: check, Status: statusWarn, Detail: fmt.Sprintf("stale: current generation is %s, but current inputs now produce %s — run `omca env` to recompile", gen.Metadata.ID, freshID)}
	}
	return doctorFinding{Check: check, Status: statusOK, Detail: fmt.Sprintf("current generation %s matches fresh inputs", gen.Metadata.ID)}
}

// checkBinaryMoved is issue #14's "host binary moved since qualification"
// AC, using the CurrentRecord sidecar SetCurrentGeneration writes (see
// internal/runtime/current.go's doc comment for why this local bookkeeping
// exists instead of a new domain.Generation field).
func checkBinaryMoved(host, worktreeStateDir string, hd hostcontext.HostDetection) doctorFinding {
	check := "binary-moved:" + host
	rec, err := runtime.ReadCurrentRecord(worktreeStateDir, host)
	if err != nil {
		return doctorFinding{Check: check, Status: statusWarn, Detail: fmt.Sprintf("no recorded qualification data for %s yet", host)}
	}
	if rec.HostBinaryPath != "" && rec.HostBinaryPath != hd.BinaryPath {
		return doctorFinding{Check: check, Status: statusWarn, Detail: fmt.Sprintf("%s binary moved since its generation was recorded: was %s, now resolves to %s — run `omca env` to re-qualify", host, rec.HostBinaryPath, hd.BinaryPath)}
	}
	return doctorFinding{Check: check, Status: statusOK, Detail: fmt.Sprintf("%s binary path unchanged since qualification (%s)", host, hd.BinaryPath)}
}

// checkDirenvApproval is issue #14's "missing direnv approval" AC. direnv's
// own convention is that an unapproved .envrc is simply never loaded; this
// checks for a .envrc at the worktree root that invokes `omca env`, then
// shells out to `direnv status` (the real, safety-bounded, non-interactive
// way to ask direnv itself whether that .envrc is approved) if direnv is
// installed. direnv not being installed is reported as its own distinct
// finding, never conflated with "installed but not approved."
func checkDirenvApproval(wt hostcontext.Worktree) doctorFinding {
	const check = "direnv-approval"
	envrcPath := filepath.Join(wt.Root, ".envrc")
	data, err := os.ReadFile(envrcPath)
	if err != nil {
		return doctorFinding{Check: check, Status: statusWarn, Detail: fmt.Sprintf("no .envrc found at %s (docs/architecture/runtime.md §4: add `eval \"$(omca env --shell bash)\"`)", envrcPath)}
	}
	if !strings.Contains(string(data), "omca env") {
		return doctorFinding{Check: check, Status: statusWarn, Detail: fmt.Sprintf(".envrc at %s does not appear to invoke `omca env`", envrcPath)}
	}

	direnvPath, lookErr := exec.LookPath("direnv")
	if lookErr != nil {
		return doctorFinding{Check: check, Status: statusWarn, Detail: "direnv is not installed — cannot verify .envrc approval (distinct from 'not approved': install direnv to enable the managed shim path)"}
	}

	ctx, cancel := stdcontext.WithTimeout(stdcontext.Background(), direnvStatusTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, direnvPath, "status")
	cmd.Dir = wt.Root
	out, runErr := cmd.Output() // direnv status's exit code varies by state, so a plain *ExitError below is expected and not itself reported
	text := string(out)

	// ctx.Err() is checked FIRST and independently of runErr's own type:
	// exec.CommandContext kills the child on a timeout, and Wait/Output
	// then reports that as an ordinary *exec.ExitError ("signal: killed"),
	// indistinguishable from direnv's own varying-by-state exit codes by
	// type alone — an earlier version of this check tried to key off
	// "is runErr a plain *exec.ExitError" and, for exactly this reason,
	// never actually reached the timeout branch (caught by this PR's own
	// TestRunDoctor_DirenvApproval_StatusTimesOut). ctx.Err() is the only
	// reliable signal. A non-nil runErr that is NOT an *exec.ExitError at
	// all (binary vanished between LookPath and Run, a permission error,
	// ...) is a second, distinct failure this must not silently fall
	// through to the generic "could not determine approval state" text
	// switch below for either (Copilot review finding on this PR).
	if ctx.Err() != nil {
		return doctorFinding{Check: check, Status: statusWarn, Detail: fmt.Sprintf("`direnv status` did not complete within %s: %v", direnvStatusTimeout, ctx.Err())}
	}
	if runErr != nil {
		if _, isExitError := runErr.(*exec.ExitError); !isExitError {
			return doctorFinding{Check: check, Status: statusWarn, Detail: fmt.Sprintf("`direnv status` failed to run: %v", runErr)}
		}
	}

	switch {
	case strings.Contains(text, "Found RC allowed true"):
		return doctorFinding{Check: check, Status: statusOK, Detail: ".envrc is approved by direnv"}
	case strings.Contains(text, "Found RC allowed false"):
		return doctorFinding{Check: check, Status: statusFail, Detail: fmt.Sprintf(".envrc at %s exists but is NOT approved — run `direnv allow`", envrcPath)}
	case strings.Contains(text, "No .envrc or .env found"):
		return doctorFinding{Check: check, Status: statusWarn, Detail: "direnv reports no .envrc loaded for this directory"}
	default:
		return doctorFinding{Check: check, Status: statusWarn, Detail: fmt.Sprintf("could not determine direnv approval state from `direnv status` output:\n%s", strings.TrimSpace(text))}
	}
}
