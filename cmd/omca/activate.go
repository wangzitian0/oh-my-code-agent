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

// confirmArg is one parsed --confirm flag: a confirmation class plus, for a
// class that gates a specific asset (enable-mcp-server, expand-access), the
// exact asset ID the operator reviewed. Host is filled in later, once
// runActivate has resolved the target host (parseActivateArgs runs before
// that) -- see runActivate's confirmedSet construction.
//
// AssetID-scoped, not bare-Kind: an earlier version of this flag confirmed
// an entire ChangeKind at once, which meant --confirm enable-mcp-server
// would silently also approve every OTHER MCP server enabled in the same
// activation, not just the one the operator actually reviewed (Copilot
// review finding on this PR, fixed at its root in
// runtime.ConfirmationKey/RequireConfirmation -- this flag format is the
// CLI-level half of that same fix).
type confirmArg struct {
	Kind    runtime.ChangeKind
	AssetID string
}

// activateArgs is `omca activate`'s parsed command line: the target host,
// plus zero or more --confirm <changeKind>[:<assetId>] flags naming a
// specific proposed change the operator has already explicitly reviewed and
// approved outside this process (there is no TUI yet, roadmap M7 -- this is
// the CLI-level stand-in issue #19's round-2 addendum asks for: "CLI
// activation enforces confirmation classes ... from the first activation
// path"). The :<assetId> suffix is required for an asset-scoped class
// (enable-mcp-server, expand-access) -- RequireConfirmation only ever
// matches a confirmation to the exact ProposedChange it names, never to
// "any change of this Kind," so an assetId-less confirmation for one of
// those classes simply never matches anything and activation stays blocked,
// rather than silently over-confirming.
type activateArgs struct {
	Host    string
	Confirm []confirmArg
}

func parseConfirmValue(v string) confirmArg {
	if i := strings.IndexByte(v, ':'); i >= 0 {
		return confirmArg{Kind: runtime.ChangeKind(v[:i]), AssetID: v[i+1:]}
	}
	return confirmArg{Kind: runtime.ChangeKind(v)}
}

func parseActivateArgs(args []string) (activateArgs, error) {
	out := activateArgs{}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--confirm":
			if i+1 >= len(args) {
				return activateArgs{}, fmt.Errorf("--confirm requires a value (e.g. enable-mcp-server:codegraph, or a bare class like select-reviewed-skill:my-skill)")
			}
			out.Confirm = append(out.Confirm, parseConfirmValue(args[i+1]))
			i++
		case strings.HasPrefix(a, "--confirm="):
			out.Confirm = append(out.Confirm, parseConfirmValue(strings.TrimPrefix(a, "--confirm=")))
		case out.Host == "" && !strings.HasPrefix(a, "-"):
			out.Host = a
		default:
			return activateArgs{}, fmt.Errorf("unrecognized argument %q", a)
		}
	}
	if out.Host == "" {
		return activateArgs{}, fmt.Errorf("a host is required: omca activate [--confirm <changeKind>[:<assetId>]]... <codex|claude>")
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
	// Host is filled in here, not at parse time (confirmArg has no Host of
	// its own): --confirm flags are parsed before this command knows its
	// target host, and every ProposedChange DiffProposedChanges produces
	// for this invocation is scoped to that same, single host anyway.
	confirmed := make(map[runtime.ConfirmationKey]bool, len(parsed.Confirm))
	for _, c := range parsed.Confirm {
		confirmed[runtime.ConfirmationKey{Kind: c.Kind, AssetID: c.AssetID, Host: host}] = true
	}
	if confErr := runtime.RequireConfirmation(changes, confirmed); confErr != nil {
		fmt.Fprintf(stderr, "omca: activate: %s: activation blocked -- the following changes require explicit confirmation (pass --confirm <changeKind>:<assetId> for each, once reviewed):\n", host)
		for i, cr := range confErr.Requirements {
			c := confErr.Changes[i]
			fmt.Fprintf(stderr, "  - --confirm %s:%s (class=%s detailKeys=%v)\n    %s\n", c.Kind, c.AssetID, cr.Class, cr.RequiredDetailKeys, cr.Explanation)
		}
		return 1
	}

	now := time.Now()
	fresh, err := composeFreshCompileRequest(wt, pendingGen, worktreeStateDir, shimDir, now)
	if err != nil {
		fmt.Fprintf(stderr, "omca: activate: recomposing fresh desired state: %v\n", err)
		return 1
	}

	generationsRoot := filepath.Join(worktreeStateDir, "generations")
	result, err := runtime.ActivateAndVerify(runtime.ActivateRequest{
		WorktreeStateDir: worktreeStateDir,
		Host:             host,
		Fresh:            fresh,
		Now:              now,
	}, generationsRoot)
	if err != nil {
		fmt.Fprintf(stderr, "omca: activate: %v\n", err)
		return 1
	}

	if !result.RolledBack {
		fmt.Fprintf(stdout, "omca: activate: %s: activated %s (previous: %q) at %s\n", result.Activation.Host, result.Activation.ActivatedGenerationID, result.Activation.PreviousGenerationID, result.Activation.ActivatedAt)
		fmt.Fprintf(stdout, "omca: activate: %s: post-activation verification passed (%s)\n", result.Activation.Host, result.Verification.Detail)
		return 0
	}

	// Post-activation verification failed and automated rollback already
	// recovered the previous generation (runtime.ActivateAndVerify's own
	// doc comment) -- both events are already ledgered by the time this
	// prints. The requested activation did NOT end up in effect, so this
	// still exits non-zero: an operator who ran `omca activate` needs to
	// know their intended change did not stick, even though the worktree
	// itself was left in a safe, recoverable state. The "activated" success
	// line is deliberately NOT printed on this path -- printing it
	// unconditionally previously misled operators/log parsers into
	// believing the activation stuck even though it was rolled back
	// (Copilot review finding on this PR); the attempted generation ID is
	// folded into the failure line below instead.
	fmt.Fprintf(stderr, "omca: activate: %s: activation of %s: post-activation verification FAILED (%s) -- automatically rolled back to the parent generation %s at %s\n", result.Activation.Host, result.Activation.ActivatedGenerationID, result.Verification.Detail, result.Rollback.RestoredGenerationID, result.Rollback.RolledBackAt)
	return 1
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
