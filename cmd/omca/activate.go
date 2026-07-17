package main

import (
	stdcontext "context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	hostcontext "github.com/wangzitian0/oh-my-code-agent/internal/context"
	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
	"github.com/wangzitian0/oh-my-code-agent/internal/observe"
	"github.com/wangzitian0/oh-my-code-agent/internal/profiles"
	"github.com/wangzitian0/oh-my-code-agent/internal/runtime"
)

// realConfigRoot resolves this machine's real XDG config root for OMCA:
// $XDG_CONFIG_HOME/omca if XDG_CONFIG_HOME is set, else $HOME/.config/omca
// -- docs/architecture/README.md §7's documented layout, the exact same
// XDG-override pattern statedir.go's realStateRoot already establishes for
// $XDG_STATE_HOME. Only this file (the command layer) is allowed to call
// it; internal/profiles.Compose takes every directory as an explicit
// parameter and never resolves one itself (CompositionInput's own doc
// comment).
func realConfigRoot() (string, error) {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		if !filepath.IsAbs(xdg) {
			return "", fmt.Errorf("omca: XDG_CONFIG_HOME %q is not an absolute path", xdg)
		}
		return filepath.Join(xdg, "omca"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("omca: resolving real config root: %w", err)
	}
	return filepath.Join(home, ".config", "omca"), nil
}

// compositionDirsFor builds the profiles.CompositionInput directory lists
// docs/architecture/README.md §7 documents: user config
// (~/.config/omca/profiles/{personal,company,team,task}/, .../bindings/,
// .../exceptions/) plus the repository's own <repository>/.omca/profiles/
// and <repository>/.omca/exceptions/ (internal/profiles.CompositionInput's
// own doc comment names exactly this set).
func compositionDirsFor(configRoot, repoRoot string) (profileDirs, bindingDirs, exceptionDirs []string) {
	profileDirs = []string{
		filepath.Join(configRoot, "profiles", "personal"),
		filepath.Join(configRoot, "profiles", "company"),
		filepath.Join(configRoot, "profiles", "team"),
		filepath.Join(configRoot, "profiles", "task"),
		filepath.Join(repoRoot, ".omca", "profiles"),
	}
	bindingDirs = []string{filepath.Join(configRoot, "bindings")}
	exceptionDirs = []string{
		filepath.Join(configRoot, "exceptions"),
		filepath.Join(repoRoot, ".omca", "exceptions"),
	}
	return profileDirs, bindingDirs, exceptionDirs
}

// activateArgs is `omca activate`'s parsed command line: the target host,
// plus zero or more --confirm <changeKind> flags naming a confirmation
// class the operator has already explicitly reviewed and approved outside
// this process (there is no TUI yet, roadmap M7 -- this is the CLI-level
// stand-in issue #19's round-2 addendum asks for: "CLI activation enforces
// confirmation classes ... from the first activation path").
type activateArgs struct {
	Host    string
	Confirm []runtime.ChangeKind
}

func parseActivateArgs(args []string) (activateArgs, error) {
	out := activateArgs{}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--confirm":
			if i+1 >= len(args) {
				return activateArgs{}, fmt.Errorf("--confirm requires a value (a confirmation class's ChangeKind, e.g. enable-mcp-server)")
			}
			out.Confirm = append(out.Confirm, runtime.ChangeKind(args[i+1]))
			i++
		case strings.HasPrefix(a, "--confirm="):
			out.Confirm = append(out.Confirm, runtime.ChangeKind(strings.TrimPrefix(a, "--confirm=")))
		case out.Host == "" && !strings.HasPrefix(a, "-"):
			out.Host = a
		default:
			return activateArgs{}, fmt.Errorf("unrecognized argument %q", a)
		}
	}
	if out.Host == "" {
		return activateArgs{}, fmt.Errorf("a host is required: omca activate [--confirm <changeKind>]... <codex|claude>")
	}
	return out, nil
}

// runActivate implements `omca activate [--confirm <changeKind>]... <host>`
// (issue #19, PR-15): runs one host's Activation transaction --
// docs/architecture/runtime.md §5.4's "validate pending -> ensure source
// digests still match -> ... -> atomically switch current -> ... -> append
// Ledger entry" -- gated by docs/product/requirements.md §7's risk-based
// confirmation table (runtime.RequireConfirmation).
//
// This is the one real, existing activation code path this PR wires
// runtime.ClassifyChange's reachable rows into: it composes the worktree's
// real desired state (internal/profiles.Compose) and re-observes every host
// the pending generation names, builds runtime.DiffProposedChanges between
// current and pending, and refuses to activate (printing exactly which
// confirmation classes are outstanding and what a real confirmation UI
// would need to show, per ConfirmationRequirement) unless every one has
// been supplied via --confirm -- the same "surface the decision as data,
// never guess or silently proceed" pattern this project's
// AmbiguousIdentityError already established for identity selection
// (internal/profiles.Compose).
func runActivate(stdout, stderr io.Writer, args []string) int {
	parsed, err := parseActivateArgs(args)
	if err != nil {
		fmt.Fprintf(stderr, "omca: activate: %v\n", err)
		return 2
	}
	host, err := normalizeHostArg(parsed.Host)
	if err != nil {
		fmt.Fprintf(stderr, "omca: activate: %v\n", err)
		return 2
	}

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(stderr, "omca: activate: %v\n", err)
		return 1
	}
	wt, err := hostcontext.DetectWorktree(cwd)
	if err != nil {
		fmt.Fprintf(stderr, "omca: activate: %v\n", err)
		return 1
	}
	stateRoot, err := realStateRoot()
	if err != nil {
		fmt.Fprintf(stderr, "omca: activate: %v\n", err)
		return 1
	}
	worktreeStateDir := worktreeStateDirPath(stateRoot, wt.ID)
	shimDir := shimDirPath(worktreeStateDir)

	pendingDir, err := runtime.PendingGenerationDir(worktreeStateDir, host)
	if err != nil {
		fmt.Fprintf(stderr, "omca: activate: no pending generation for %s: %v (compile one first)\n", host, err)
		return 1
	}
	pendingGen, err := runtime.ReadGenerationManifest(pendingDir)
	if err != nil {
		fmt.Fprintf(stderr, "omca: activate: pending generation manifest for %s is unreadable: %v\n", host, err)
		return 1
	}

	var currentGen domain.Generation
	if currentDir, cerr := runtime.CurrentGenerationDir(worktreeStateDir, host); cerr == nil {
		if g, rerr := runtime.ReadGenerationManifest(currentDir); rerr == nil {
			currentGen = g
		}
	}

	changes := runtime.DiffProposedChanges(currentGen, pendingGen, host)
	confirmed := make(map[runtime.ChangeKind]bool, len(parsed.Confirm))
	for _, k := range parsed.Confirm {
		confirmed[k] = true
	}
	if confErr := runtime.RequireConfirmation(changes, confirmed); confErr != nil {
		fmt.Fprintf(stderr, "omca: activate: %s: activation blocked -- the following changes require explicit confirmation (pass --confirm <changeKind> for each, once reviewed):\n", host)
		for _, c := range changes {
			cr := runtime.ClassifyChange(c)
			if !cr.RequiresConfirmation || confirmed[c.Kind] {
				continue
			}
			fmt.Fprintf(stderr, "  - --confirm %s (asset=%q class=%s detailKeys=%v)\n    %s\n", c.Kind, c.AssetID, cr.Class, cr.RequiredDetailKeys, cr.Explanation)
		}
		return 1
	}

	now := time.Now()
	fresh, err := composeFreshCompileRequest(wt, pendingGen, worktreeStateDir, shimDir, now)
	if err != nil {
		fmt.Fprintf(stderr, "omca: activate: recomposing fresh desired state: %v\n", err)
		return 1
	}

	result, err := runtime.Activate(runtime.ActivateRequest{
		WorktreeStateDir: worktreeStateDir,
		Host:             host,
		Fresh:            fresh,
		Now:              now,
	})
	if err != nil {
		fmt.Fprintf(stderr, "omca: activate: %v\n", err)
		return 1
	}

	fmt.Fprintf(stdout, "omca: activate: %s: activated %s (previous: %q) at %s\n", result.Host, result.ActivatedGenerationID, result.PreviousGenerationID, result.ActivatedAt)
	return 0
}

// composeFreshCompileRequest builds the runtime.CompileRequest Activate's
// CAS check needs -- a fresh recomposition of desired state (internal/
// profiles.Compose) and a fresh re-observation of every host pendingGen
// names (pendingGen.Spec.Hosts' key set, not just the one host being
// activated: a shared multi-host generation's own recorded sourceDigest
// covers every host it names, so a valid freshness recomputation must too).
func composeFreshCompileRequest(wt hostcontext.Worktree, pendingGen domain.Generation, worktreeStateDir, shimDir string, now time.Time) (runtime.CompileRequest, error) {
	configRoot, err := realConfigRoot()
	if err != nil {
		return runtime.CompileRequest{}, err
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
			return runtime.CompileRequest{}, fmt.Errorf("identity selection is ambiguous for %d categories (run a future `omca identity` selection flow first): %v", len(ambErr.Ambiguous), ambErr)
		}
		return runtime.CompileRequest{}, err
	}

	realEnv := hostcontext.RealEnvironment()
	detectEnv := envWithFilteredPath(realEnv, shimDir)

	hostIDs := make([]string, 0, len(pendingGen.Spec.Hosts))
	for h := range pendingGen.Spec.Hosts {
		hostIDs = append(hostIDs, h)
	}
	sort.Strings(hostIDs)

	hosts := make([]runtime.HostCompileInput, 0, len(hostIDs))
	for _, h := range hostIDs {
		hd, err := hostcontext.DetectHost(stdcontext.Background(), detectEnv, h)
		if err != nil {
			return runtime.CompileRequest{}, fmt.Errorf("detecting %s: %w", h, err)
		}
		obs, err := observe.Observe(observe.Request{Detection: hd, WorktreeRoot: wt.Root})
		if err != nil {
			return runtime.CompileRequest{}, fmt.Errorf("observing %s: %w", h, err)
		}
		hosts = append(hosts, runtime.HostCompileInput{Detection: hd, Observations: obs, OMCABinaryPath: omcaCommandPath(shimDir)})
	}

	return runtime.CompileRequest{
		Worktree:   wt,
		Hosts:      hosts,
		Profiles:   composition.Profiles,
		Activation: composition.Activation,
		Exceptions: composition.Exceptions,
		Now:        now,
	}, nil
}
