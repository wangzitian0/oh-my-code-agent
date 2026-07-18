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
	"github.com/wangzitian0/oh-my-code-agent/internal/observe"
	"github.com/wangzitian0/oh-my-code-agent/internal/profiles"
	"github.com/wangzitian0/oh-my-code-agent/internal/runtime"
)

// bisectArgs is `omca bisect`'s parsed command line: an optional --dry-run
// flag plus the target host.
type bisectArgs struct {
	DryRun bool
	Host   string
}

func parseBisectArgs(args []string) (bisectArgs, error) {
	out := bisectArgs{}
	for _, a := range args {
		switch {
		case a == "--dry-run":
			out.DryRun = true
		case out.Host == "" && !strings.HasPrefix(a, "-"):
			out.Host = a
		default:
			return bisectArgs{}, fmt.Errorf("unrecognized argument %q", a)
		}
	}
	if out.Host == "" {
		return bisectArgs{}, fmt.Errorf("a host is required: omca bisect [--dry-run] <codex|claude>")
	}
	return out, nil
}

// runBisect implements `omca bisect [--dry-run] <host>`
// (docs/architecture/runtime.md §11's "omca bisect codex": "bisect builds
// disposable generations that import candidate sources one at a time").
// See runtime.Bisect's own doc comment for the full algorithm and safety
// argument; this command layer only detects the real worktree/host,
// observes and recomposes desired state exactly like `omca activate` does,
// and prints the resulting plan.
//
// --dry-run is the round-3 pre-dispatch safety audit's mandatory mode
// (issue #28's own acceptance criterion): it reports the exact plan a real
// bisect run would build -- every step's candidate source and content-
// addressed generation ID, in order -- without calling runtime.Compile or
// writing a single byte to disk.
//
// Without --dry-run, bisect DOES compile: real disposable generations land
// under this worktree's generations/ directory, exactly like `omca env`/
// `omca run`/`omca activate` already write there. It NEVER activates any of
// them, though -- see runtime.Bisect's own doc comment for why this is safe
// by construction even in its compiling form: no current/pending pointer
// for host is ever touched, and nothing is ever ledgered by this command.
func runBisect(stdout, stderr io.Writer, args []string) int {
	parsed, err := parseBisectArgs(args)
	if err != nil {
		fmt.Fprintf(stderr, "omca: bisect: %v\n", err)
		return 2
	}
	host, err := normalizeHostArg(parsed.Host)
	if err != nil {
		fmt.Fprintf(stderr, "omca: bisect: %v\n", err)
		return 2
	}

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(stderr, "omca: bisect: %v\n", err)
		return 1
	}
	wt, err := hostcontext.DetectWorktree(cwd)
	if err != nil {
		fmt.Fprintf(stderr, "omca: bisect: %v\n", err)
		return 1
	}
	stateRoot, err := realStateRoot()
	if err != nil {
		fmt.Fprintf(stderr, "omca: bisect: %v\n", err)
		return 1
	}
	worktreeStateDir := worktreeStateDirPath(stateRoot, wt.ID)
	shimDir := shimDirPath(worktreeStateDir)
	// installShims is skipped on --dry-run: omcaCommandPath below is a pure
	// string join (cmd/omca/env.go), and hostcontext.DetectHost only needs
	// shimDir as a PATH-filtering reference (the same pattern runDoctor's
	// checkGenerationFreshness uses without ever calling installShims) --
	// neither requires the shim symlinks to actually exist on disk. Calling
	// installShims unconditionally here previously broke --dry-run's own
	// documented "without ... writing a single byte to disk" contract
	// (Copilot review finding on this PR).
	if !parsed.DryRun {
		if err := installShims(shimDir); err != nil {
			fmt.Fprintf(stderr, "omca: bisect: installing PATH shims: %v\n", err)
			return 1
		}
	}

	realEnv := hostcontext.RealEnvironment()
	detectEnv := envWithFilteredPath(realEnv, shimDir)
	hd, err := hostcontext.DetectHost(stdcontext.Background(), detectEnv, host)
	if err != nil {
		fmt.Fprintf(stderr, "omca: bisect: %v\n", err)
		return 1
	}
	if !hd.Installed {
		fmt.Fprintf(stderr, "omca: bisect: %s is not installed\n", host)
		return 1
	}

	obs, err := observe.Observe(observe.Request{Detection: hd, WorktreeRoot: wt.Root})
	if err != nil {
		fmt.Fprintf(stderr, "omca: bisect: observing %s: %v\n", host, err)
		return 1
	}

	now := time.Now()
	composition, err := composeDesiredStateForBisect(wt, worktreeStateDir, now)
	if err != nil {
		fmt.Fprintf(stderr, "omca: bisect: recomposing desired state: %v\n", err)
		return 1
	}

	compileReq := runtime.CompileRequest{
		Worktree: wt,
		Hosts: []runtime.HostCompileInput{
			{Detection: hd, Observations: obs, OMCABinaryPath: omcaCommandPath(shimDir)},
		},
		Profiles:   composition.Profiles,
		Activation: composition.Activation,
		Exceptions: composition.Exceptions,
		Now:        now,
	}
	generationsRoot := filepath.Join(worktreeStateDir, "generations")

	plan, err := runtime.Bisect(runtime.BisectRequest{Compile: compileReq, GenerationsRoot: generationsRoot, DryRun: parsed.DryRun})
	if err != nil {
		fmt.Fprintf(stderr, "omca: bisect: %v\n", err)
		return 1
	}

	printBisectPlan(stdout, plan)
	return 0
}

// composeDesiredStateForBisect recomposes this worktree's real desired
// state (internal/profiles.Compose) -- the same Profiles/Activation/
// Exceptions a real `omca activate` would compile a pending generation from
// (composeFreshCompileRequest, activate.go) -- without that function's own
// per-host re-detection/re-observation loop, which exists only to recompute
// Activate's CAS-check digest across every host a specific PENDING
// generation names. Bisect has no pending generation and is single-host by
// contract (runtime.Bisect's own doc comment), so it only ever needs the
// desired-state composition itself; this small, deliberate duplication of
// composeFreshCompileRequest's own realConfigRoot/compositionDirsFor/
// profiles.Compose call sequence beats reaching into that function's
// unrelated multi-host CAS-recomputation shape just to reuse three lines of
// it (the same "a small intentional duplicate beats a forced dependency on
// unrelated internals" precedent internal/assurance/verify.go's
// resolveQualified doc comment documents elsewhere in this project).
func composeDesiredStateForBisect(wt hostcontext.Worktree, worktreeStateDir string, now time.Time) (profiles.CompositionResult, error) {
	configRoot, err := realConfigRoot()
	if err != nil {
		return profiles.CompositionResult{}, err
	}
	profileDirs, bindingDirs, exceptionDirs := compositionDirsFor(configRoot, wt.Root)
	composition, err := profiles.Compose(profiles.CompositionInput{
		Repository:       wt.Root,
		ProfileDirs:      profileDirs,
		BindingDirs:      bindingDirs,
		ExceptionDirs:    exceptionDirs,
		ActivationPath:   filepath.Join(worktreeStateDir, "desired", "activation.yaml"),
		WorktreeStateDir: worktreeStateDir,
		Now:              now,
	})
	if err != nil {
		if ambErr, ok := err.(*profiles.AmbiguousIdentityError); ok {
			return profiles.CompositionResult{}, fmt.Errorf("identity selection is ambiguous for %d categories (run a future `omca identity` selection flow first): %v", len(ambErr.Ambiguous), ambErr)
		}
		return profiles.CompositionResult{}, err
	}
	return composition, nil
}

// printBisectPlan renders plan for a human, distinguishing a DryRun plan
// (nothing on disk backs any of these generation IDs yet) from a real one
// (every step's OutputDir is a real, compiled, inspectable generation
// directory).
func printBisectPlan(stdout io.Writer, plan runtime.BisectPlan) {
	if len(plan.Steps) == 0 {
		fmt.Fprintf(stdout, "omca: bisect: %s: no candidate sources observed; nothing to bisect\n", plan.Host)
		return
	}
	if plan.DryRun {
		fmt.Fprintf(stdout, "omca: bisect: %s: DRY RUN -- would build %d disposable generation(s), none compiled, none activated:\n", plan.Host, len(plan.Steps))
	} else {
		fmt.Fprintf(stdout, "omca: bisect: %s: built %d disposable generation(s), none activated (inspect each with `omca diff`/`omca compare`, or read its manifest.json directly):\n", plan.Host, len(plan.Steps))
	}
	for _, step := range plan.Steps {
		if plan.DryRun {
			fmt.Fprintf(stdout, "  step %d/%d: + %s -> %s (not compiled)\n", step.Index, len(plan.Steps), step.CandidateID, step.GenerationID)
		} else {
			fmt.Fprintf(stdout, "  step %d/%d: + %s -> %s at %s\n", step.Index, len(plan.Steps), step.CandidateID, step.GenerationID, step.OutputDir)
		}
	}
}
