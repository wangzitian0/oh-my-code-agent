package tui

import (
	stdcontext "context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	hostcontext "github.com/wangzitian0/oh-my-code-agent/internal/context"
	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
	"github.com/wangzitian0/oh-my-code-agent/internal/knowledge"
	"github.com/wangzitian0/oh-my-code-agent/internal/observe"
	"github.com/wangzitian0/oh-my-code-agent/internal/profiles"
	"github.com/wangzitian0/oh-my-code-agent/internal/report"
	"github.com/wangzitian0/oh-my-code-agent/internal/runtime"
)

// ActionContext carries every real filesystem/environment input issue #35's
// action layer (stage/activate/rollback/restart-detection) needs, already
// resolved by the command layer (cmd/omca/tui.go) -- exactly the same
// "directories are explicit parameters, never resolved internally"
// discipline profiles.CompositionInput's own doc comment establishes, and
// cmd/omca/activate.go's realConfigRoot doc comment states even more
// bluntly: "Only this file [cmd/omca] is allowed to call it." internal/tui
// never reads $HOME/$XDG_CONFIG_HOME/$XDG_STATE_HOME itself; this struct is
// how cmd/omca's own already-resolved values reach this package's Model.
//
// See doc.go's "Action layer" section for why the composition/detection
// SEQUENCES this file's functions run are a deliberate, doc-comment-flagged
// mirror of cmd/omca/activate.go's composeFreshCompileRequest and
// cmd/omca/mcp.go's compileFuncForMCP, rather than a shared package both
// import: cmd/omca is `package main`, which internal/tui can never import
// at all, and the reverse dependency (cmd/omca already imports internal/tui
// for its Model) forecloses moving these cmd/omca-owned helpers the other
// way for this PR.
type ActionContext struct {
	// Worktree is this session's detected worktree (hostcontext.
	// DetectWorktree's result).
	Worktree hostcontext.Worktree
	// WorktreeStateDir is this worktree's state root (current/pending/
	// generations/ledger all live under it) -- cmd/omca's
	// worktreeStateDirPath(realStateRoot(), wt.ID).
	WorktreeStateDir string
	// ShimDir is this worktree's PATH-shim directory -- cmd/omca's
	// shimDirPath(WorktreeStateDir). Only its path string is used (baked
	// into a freshly-compiled generation's own OMCABinaryPath, exactly like
	// cmd/omca/env.go's omcaCommandPath); this package never installs or
	// reads shim files itself.
	ShimDir string
	// ConfigRoot is this machine's real OMCA config root -- cmd/omca's
	// realConfigRoot() ($XDG_CONFIG_HOME/omca or $HOME/.config/omca).
	ConfigRoot string
	// Env is the real ambient process environment (hostcontext.
	// RealEnvironment()), used for host detection and for the
	// restart-required env-var signal (restartStatusForHost).
	Env hostcontext.Environment
}

// enabled reports whether ctx carries enough real state for this package's
// action layer to do anything. The zero-value ActionContext{} -- what every
// pre-existing PR-30 test still constructs via NewModel, and what a caller
// that never wires actions in leaves Model with -- must leave every action
// key (activate/approve/rollback) inert rather than panicking on an empty
// WorktreeStateDir, preserving this package's pre-existing read-only
// behavior exactly.
func (ctx ActionContext) enabled() bool {
	return ctx.WorktreeStateDir != "" && ctx.Worktree.Root != ""
}

// compositionDirsForAction mirrors cmd/omca/activate.go's
// compositionDirsFor EXACTLY (identical directories, identical order):
// duplicated here, not imported, because compositionDirsFor is an
// unexported function of cmd/omca (package main), which internal/tui can
// never import. See this package's doc.go for the full trade-off this
// mirror makes.
func compositionDirsForAction(configRoot, repoRoot string) (profileDirs, bindingDirs, exceptionDirs []string) {
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

// omcaCommandPathForAction mirrors cmd/omca/env.go's omcaCommandPath
// exactly: the stable, worktree-scoped path a compiled generation's own MCP
// registration should invoke.
func omcaCommandPathForAction(shimDir string) string {
	return filepath.Join(shimDir, "omca")
}

// composeDesiredState runs internal/profiles.Compose over ctx's
// already-resolved config root/worktree state dir -- the identical
// CompositionInput cmd/omca/activate.go's composeFreshCompileRequest and
// cmd/omca/mcp.go's compileFuncForMCP both build. An AmbiguousIdentityError
// is reported with a TUI-appropriate hint (there is no interactive identity
// picker yet -- roadmap M7 territory beyond this PR) rather than the CLI's
// own wording, matching composeFreshCompileRequest's identical treatment of
// this same error.
func composeDesiredState(ctx ActionContext, now time.Time) (profiles.CompositionResult, error) {
	profileDirs, bindingDirs, exceptionDirs := compositionDirsForAction(ctx.ConfigRoot, ctx.Worktree.Root)
	composed, err := profiles.Compose(profiles.CompositionInput{
		Repository:       ctx.Worktree.Root,
		ProfileDirs:      profileDirs,
		BindingDirs:      bindingDirs,
		ExceptionDirs:    exceptionDirs,
		ActivationPath:   filepath.Join(ctx.WorktreeStateDir, "desired", "activation.yaml"),
		WorktreeStateDir: ctx.WorktreeStateDir,
		Now:              now,
	})
	if err != nil {
		if ambErr, ok := err.(*profiles.AmbiguousIdentityError); ok {
			return profiles.CompositionResult{}, fmt.Errorf("identity selection is ambiguous for %d categories (no interactive identity picker exists yet; run `omca activate` from a shell to resolve it first): %w", len(ambErr.Ambiguous), ambErr)
		}
		return profiles.CompositionResult{}, err
	}
	return composed, nil
}

// detectAndObserveHost runs the fresh detect-then-observe sequence every
// cmd/omca compose/compile helper (composeFreshCompileRequest,
// compileFuncForMCP, buildArtifactForCLI) repeats per host.
func detectAndObserveHost(ctx ActionContext, host string) (hostcontext.HostDetection, []domain.Observation, error) {
	hd, err := hostcontext.DetectHost(stdcontext.Background(), ctx.Env, host)
	if err != nil {
		return hostcontext.HostDetection{}, nil, fmt.Errorf("detecting %s: %w", host, err)
	}
	if !hd.Installed {
		return hostcontext.HostDetection{}, nil, fmt.Errorf("%s is not installed -- cannot compile a generation for it", host)
	}
	obs, err := observe.Observe(observe.Request{Detection: hd, WorktreeRoot: ctx.Worktree.Root})
	if err != nil {
		return hostcontext.HostDetection{}, nil, fmt.Errorf("observing %s: %w", host, err)
	}
	return hd, obs, nil
}

// mergeHostActivationForAction mirrors cmd/omca/mcp.go's
// mergeHostActivation exactly: an additive union (resolve.inSelection only
// ever checks list membership, so a duplicate entry is harmless).
func mergeHostActivationForAction(dst, src domain.HostActivation) domain.HostActivation {
	dst.Enable.Skills = append(dst.Enable.Skills, src.Enable.Skills...)
	dst.Enable.MCPServers = append(dst.Enable.MCPServers, src.Enable.MCPServers...)
	dst.Disable.Skills = append(dst.Disable.Skills, src.Disable.Skills...)
	dst.Disable.MCPServers = append(dst.Disable.MCPServers, src.Disable.MCPServers...)
	return dst
}

// activationSelectionFor maps an Assets-view AVAILABLE Candidate's Concept
// onto the domain.ActivationSelection field an Activation.Spec.Hosts entry
// can enable it through. Only "skill" and "mcpServer" are selectable this
// way (internal/resolve/resolve.go's inSelection doc comment: "Instructions
// have no ActivationSelection field, so they are never
// Activation-selected") -- ok is false for any other concept (instruction,
// permission, or anything this package does not recognize), so a caller
// never silently no-ops an activation request for a concept this mechanism
// cannot actually express.
func activationSelectionFor(concept, logicalID string) (domain.HostActivation, bool) {
	switch concept {
	case "skill":
		return domain.HostActivation{Enable: domain.ActivationSelection{Skills: []string{logicalID}}}, true
	case "mcpServer":
		return domain.HostActivation{Enable: domain.ActivationSelection{MCPServers: []string{logicalID}}}, true
	default:
		return domain.HostActivation{}, false
	}
}

// firstActivatableCandidate returns the first AVAILABLE-but-not-yet-Active
// resolved asset for host in a.Debug[host].Desired (resolve.ResolvedState --
// the Desired Graph), whose Kind activationSelectionFor can actually stage.
//
// This deliberately reads the Desired Graph, not the Effective Graph's
// physical Candidate.Disposition bucket RenderAssets/bucketFor render (the
// "Available" section a human sees on the Assets view): no code path in
// this repository computes domain.DispositionAvailable on a real Candidate
// today (see internal/report/adapter.go's own "Known follow-up:
// Desired-vs-Effective correlation (EFFECTIVE_DRIFT)" doc comment for why
// that correlation does not exist yet), so an AVAILABLE Candidate can never
// actually be produced from real data yet, and this package will not build
// an action around a bucket that is always empty in practice.
// resolve.ResolvedAsset with Intent==domain.IntentAvailable and
// Active==false IS real and reachable today: it is the exact same asset
// identity runtime.DiffProposedChanges/runtime.ClassifyChange already
// classify once an ActivationSelection actually activates it, and
// docs/product/requirements.md's own "AVAILABLE" intent vocabulary. That
// makes it the honest, currently-working notion of "AVAILABLE asset" issue
// #35's action operates on.
//
// Only Kind skill/mcpServer are returned (activationSelectionFor's own
// scope). resolve.ResolvedState.Assets' own doc comment guarantees Assets
// is already sorted by (Kind, ID), so the first match here always matches
// the first such asset a Kind-then-ID listing would show.
func firstActivatableCandidate(a report.Artifact, host string) (concept, logicalID string, ok bool) {
	hd, exists := a.Debug[host]
	if !exists {
		return "", "", false
	}
	for _, asset := range hd.Desired.Assets {
		if asset.Active || asset.Intent != domain.IntentAvailable {
			continue
		}
		if _, selectable := activationSelectionFor(string(asset.Kind), asset.ID); !selectable {
			continue
		}
		return string(asset.Kind), asset.ID, true
	}
	return "", "", false
}

// stageResult is stageAssetActivation's success value.
type stageResult struct {
	Host       string
	PendingGen domain.Generation
	// CurrentGen is host's current generation at the moment of staging, the
	// zero value if host has never been activated in this worktree yet.
	CurrentGen domain.Generation
}

// stageAssetActivation stages a pending generation for host that
// additionally enables (concept, logicalID) -- issue #35's "Activating an
// AVAILABLE asset stages pending" AC, for exactly the physical Candidate
// this package's own RenderAssets already shows in the AVAILABLE bucket
// (bucketFor).
//
// This mirrors cmd/omca/mcp.go's compileFuncForMCP -- the real production
// `omca_stage` MCP tool's code path -- narrowed to exactly one host and one
// newly-enabled asset (never compileFuncForMCP's own multi-host
// Parent-divergence logic, which a single-host, single-asset action can
// never trigger): compose the worktree's real desired state, merge the one
// Enable selection onto the composed Activation's host entry, PERSIST that
// merged Activation to disk (profiles.PersistActivation -- see below for
// why this step exists), detect and observe host fresh, compile (reusing an
// already-compiled generation by content-addressed ID exactly like
// EnsureGeneration/compileFuncForMCP, and reconciling a stale cached Parent
// exactly like compileFuncForMCP's own ReconcileGenerationParent call), and
// record it as host's "pending" pointer via runtime.SetPendingGeneration --
// "current" stays untouched.
//
// # Why this persists the merged Activation (a gap compileFuncForMCP itself
// still has)
//
// compileFuncForMCP merges hostActivations into composed.Activation
// PURELY IN MEMORY, for the one CompileRequest it is about to compile, and
// never durably writes it anywhere. That is invisible to `omca_stage`
// itself (it always compiles successfully against its own freshly-merged
// request), but it is a real, latent problem for anything that activates
// the result LATER, in a separate call: composeFreshCompileRequest
// (cmd/omca/activate.go) -- and this file's own identical mirror,
// composeFreshForActivate -- recompose desired state from SCRATCH via
// profiles.Compose, which has no way to see an Enable choice that only
// ever existed in some earlier caller's memory. Activate's CAS check
// (freshSourceDigest) then correctly reports the fresh recomposition no
// longer matches what pending was compiled from — not a bug in the CAS
// check, but a real hole in the production `omca_stage`-then-later-`omca
// activate` path for any generation staged via an ActivationSelection
// Enable rather than a Profile's own REQUIRED intent (mcp_stage_rollback_
// test.go's own real-world regression test only ever exercises REQUIRED
// intent for exactly this reason).
//
// Persisting to <worktree state dir>/desired/activation.yaml — the exact
// path profiles.CompositionInput.ActivationPath already names and
// profiles.LoadActivation already reads on every Compose call — closes
// this gap the correct way, using the SAME durable worktree-local-state
// document this package's own composeDesiredState/composeFreshForActivate
// both already read, rather than inventing a second, TUI-only persistence
// mechanism: once written, composeFreshForActivate's later, independent
// profiles.Compose call reads back the identical merged Activation, so its
// CAS check's fresh recomputation genuinely matches what was staged.
func stageAssetActivation(ctx ActionContext, host, concept, logicalID string, now time.Time) (stageResult, error) {
	sel, ok := activationSelectionFor(concept, logicalID)
	if !ok {
		return stageResult{}, fmt.Errorf("%s %q cannot be activated this way (no domain.ActivationSelection field exists for this concept)", concept, logicalID)
	}

	composed, err := composeDesiredState(ctx, now)
	if err != nil {
		return stageResult{}, fmt.Errorf("composing desired state: %w", err)
	}
	mergedActivation := composed.Activation
	mergedActivation.APIVersion = domain.SupportedAPIVersion
	mergedActivation.Kind = "Activation"
	mergedActivation.Metadata.Worktree = ctx.Worktree.ID
	if mergedActivation.Spec.Hosts == nil {
		mergedActivation.Spec.Hosts = map[string]domain.HostActivation{}
	}
	mergedActivation.Spec.Hosts[host] = mergeHostActivationForAction(mergedActivation.Spec.Hosts[host], sel)

	if err := profiles.PersistActivation(ctx.WorktreeStateDir, mergedActivation); err != nil {
		return stageResult{}, fmt.Errorf("persisting worktree activation: %w", err)
	}

	hd, obs, err := detectAndObserveHost(ctx, host)
	if err != nil {
		return stageResult{}, err
	}

	var currentGen domain.Generation
	var parent *string
	if curDir, cerr := runtime.CurrentGenerationDir(ctx.WorktreeStateDir, host); cerr == nil {
		g, rerr := runtime.ReadGenerationManifest(curDir)
		if rerr != nil {
			return stageResult{}, fmt.Errorf("host %q has a current-generation pointer at %s but its manifest is unreadable, refusing to guess its Parent: %w", host, curDir, rerr)
		}
		currentGen = g
		id := g.Metadata.ID
		parent = &id
	} else if !os.IsNotExist(cerr) {
		return stageResult{}, fmt.Errorf("host %q: resolving current-generation pointer: %w", host, cerr)
	}

	req := runtime.CompileRequest{
		Worktree:   ctx.Worktree,
		Hosts:      []runtime.HostCompileInput{{Detection: hd, Observations: obs, OMCABinaryPath: omcaCommandPathForAction(ctx.ShimDir)}},
		Profiles:   composed.Profiles,
		Activation: mergedActivation,
		Exceptions: composed.Exceptions,
		Now:        now,
		Invocation: "tui_stage",
		Parent:     parent,
	}

	genID, err := runtime.CompileGenerationID(req)
	if err != nil {
		return stageResult{}, fmt.Errorf("computing generation ID: %w", err)
	}
	outputDir := filepath.Join(ctx.WorktreeStateDir, "generations", runtime.DirSafeID(genID))

	var gen domain.Generation
	if _, readErr := runtime.ReadGenerationManifest(outputDir); readErr != nil {
		if !os.IsNotExist(readErr) {
			return stageResult{}, fmt.Errorf("existing generation directory %s is present but its manifest failed validation, refusing to overwrite a content-addressed path: %w", outputDir, readErr)
		}
		gen, err = runtime.Compile(req, outputDir)
		if err != nil {
			return stageResult{}, fmt.Errorf("compiling: %w", err)
		}
	} else {
		gen, err = runtime.ReconcileGenerationParent(outputDir, parent)
		if err != nil {
			return stageResult{}, fmt.Errorf("reconciling cached generation's Parent: %w", err)
		}
	}

	if err := runtime.SetPendingGeneration(ctx.WorktreeStateDir, host, outputDir, gen, hd, now); err != nil {
		return stageResult{}, fmt.Errorf("recording pending generation for %s: %w", host, err)
	}

	return stageResult{Host: host, PendingGen: gen, CurrentGen: currentGen}, nil
}

// changeReview is the full reviewed Change Set for one pending activation --
// issue #35's "one human approval can execute a complete reviewed Change
// Set" AC: every runtime.ProposedChange runtime.DiffProposedChanges finds
// between current and pending, alongside its runtime.ClassifyChange verdict
// (same index).
type changeReview struct {
	Host         string
	Changes      []runtime.ProposedChange
	Requirements []runtime.ConfirmationRequirement
}

// buildChangeReview runs runtime.DiffProposedChanges then
// runtime.ClassifyChange over every result -- the exact classification
// cmd/omca/activate.go's runActivate performs before checking
// RequireConfirmation, surfaced here as data for a confirmation screen
// instead of a CLI error message.
func buildChangeReview(host string, currentGen, pendingGen domain.Generation) changeReview {
	changes := runtime.DiffProposedChanges(currentGen, pendingGen, host)
	reqs := make([]runtime.ConfirmationRequirement, len(changes))
	for i, c := range changes {
		reqs[i] = runtime.ClassifyChange(c)
	}
	return changeReview{Host: host, Changes: changes, Requirements: reqs}
}

// approveAll builds the confirmed set runtime.RequireConfirmation needs by
// marking EVERY change in r.Changes confirmed at once -- issue #35's "one
// human approval can execute a complete reviewed Change Set": a single
// keypress (Model's approveReview) reviews the WHOLE screen this Change Set
// renders and only then calls this, mirroring the CLI's own
// multiple-"--confirm"-flags-reviewed-together-before-one-"omca
// activate"-call semantics (cmd/omca/activate.go's own doc comment), just
// interactively instead of via repeated flags.
func (r changeReview) approveAll() map[runtime.ConfirmationKey]bool {
	confirmed := make(map[runtime.ConfirmationKey]bool, len(r.Changes))
	for _, c := range r.Changes {
		confirmed[c.Key()] = true
	}
	return confirmed
}

// composeFreshForActivate mirrors cmd/omca/activate.go's
// composeFreshCompileRequest EXACTLY: the same profiles.Compose call, the
// same fresh per-host detect+observe loop over pendingGen.Spec.Hosts' own
// key set (a shared multi-host generation's recorded sourceDigest covers
// every host it names, so a valid freshness recomputation must too).
// Duplicated here, not imported, because composeFreshCompileRequest is
// unexported in cmd/omca (package main) -- see this file's and doc.go's
// doc comments for the full trade-off.
func composeFreshForActivate(ctx ActionContext, pendingGen domain.Generation, now time.Time) (runtime.CompileRequest, error) {
	composed, err := composeDesiredState(ctx, now)
	if err != nil {
		return runtime.CompileRequest{}, err
	}

	hostIDs := make([]string, 0, len(pendingGen.Spec.Hosts))
	for h := range pendingGen.Spec.Hosts {
		hostIDs = append(hostIDs, h)
	}
	sort.Strings(hostIDs)

	hosts := make([]runtime.HostCompileInput, 0, len(hostIDs))
	for _, h := range hostIDs {
		hd, obs, err := detectAndObserveHost(ctx, h)
		if err != nil {
			return runtime.CompileRequest{}, err
		}
		hosts = append(hosts, runtime.HostCompileInput{Detection: hd, Observations: obs, OMCABinaryPath: omcaCommandPathForAction(ctx.ShimDir)})
	}

	return runtime.CompileRequest{
		Worktree:   ctx.Worktree,
		Hosts:      hosts,
		Profiles:   composed.Profiles,
		Activation: composed.Activation,
		Exceptions: composed.Exceptions,
		Now:        now,
	}, nil
}

// activateHost runs host's Activation transaction via runtime.
// ActivateAndVerify -- the SAME activate-then-verify-then-auto-rollback-on-
// failure path cmd/omca/activate.go's runActivate calls -- given every
// required confirmation has already been supplied (review.approveAll(),
// called by Model.approveReview only after the operator reviewed the WHOLE
// Change Set screen and pressed the one approval key). Mirrors runActivate's
// own body one-for-one: check RequireConfirmation, recompose a fresh
// CompileRequest, call ActivateAndVerify.
func activateHost(ctx ActionContext, review changeReview, pendingGen domain.Generation, confirmed map[runtime.ConfirmationKey]bool, now time.Time) (runtime.ActivateAndVerifyResult, error) {
	if confErr := runtime.RequireConfirmation(review.Changes, confirmed); confErr != nil {
		return runtime.ActivateAndVerifyResult{}, confErr
	}

	fresh, err := composeFreshForActivate(ctx, pendingGen, now)
	if err != nil {
		return runtime.ActivateAndVerifyResult{}, fmt.Errorf("recomposing fresh desired state: %w", err)
	}

	generationsRoot := filepath.Join(ctx.WorktreeStateDir, "generations")
	return runtime.ActivateAndVerify(runtime.ActivateRequest{
		WorktreeStateDir: ctx.WorktreeStateDir,
		Host:             review.Host,
		Fresh:            fresh,
		Now:              now,
	}, generationsRoot)
}

// rollbackHost restores host's parent generation via runtime.Rollback --
// the exact same call cmd/omca/rollback.go's runRollback makes (detection
// is used only for the CurrentRecord sidecar SetCurrentGeneration writes,
// per Rollback's own doc comment).
func rollbackHost(ctx ActionContext, host string, now time.Time) (runtime.RollbackResult, error) {
	hd, err := hostcontext.DetectHost(stdcontext.Background(), ctx.Env, host)
	if err != nil {
		return runtime.RollbackResult{}, fmt.Errorf("detecting %s: %w", host, err)
	}
	generationsRoot := filepath.Join(ctx.WorktreeStateDir, "generations")
	return runtime.Rollback(ctx.WorktreeStateDir, generationsRoot, host, hd, now)
}

// restartStatusForHost mirrors cmd/omca/doctor.go's checkRestartRequired
// signal EXACTLY: OMCA_RUN_ID plus which host's own native-home environment
// variable is set in ctx.Env, generalized PER HOST (issue #35's own AC:
// "shows restart_required per host") rather than doctor's single,
// ambiguity-refusing sessionHostFromEnv, which only ever names ONE host or
// gives up entirely. ok is false when this host's own managed-session
// signal is not present in ctx.Env (no OMCA_RUN_ID, or this host's own
// native-home env var is unset) -- exactly like checkRestartRequired's
// identical "nothing to report" case: a TUI invoked directly by a human
// (the overwhelmingly common case) has no live managed session to compare
// against for any host, and reporting a misleading verdict for a question
// that genuinely does not apply would violate this project's own "no false
// green" doctor.go quality bar.
func restartStatusForHost(ctx ActionContext, host string) (runtime.RestartStatus, bool) {
	runID := ctx.Env.Get("OMCA_RUN_ID")
	if runID == "" {
		return runtime.RestartStatus{}, false
	}
	envVar, err := runtime.NativeHomeEnvVar(host)
	if err != nil || ctx.Env.Get(envVar) == "" {
		return runtime.RestartStatus{}, false
	}
	status, err := runtime.DetectRestartRequired(ctx.WorktreeStateDir, host, runID)
	if err != nil {
		return runtime.RestartStatus{Host: host, SessionGenerationID: runID, Detail: err.Error()}, true
	}
	return status, true
}

// restartStatusesForHosts computes restartStatusForHost for every host in
// hosts, in order, returning only the ones with a real signal to report
// (ok==true) -- issue #35's own "restart_required per host" AC, applied
// across a whole list of hosts rather than doctor's single ambiguous
// finding.
func restartStatusesForHosts(ctx ActionContext, hosts []string) []runtime.RestartStatus {
	var out []runtime.RestartStatus
	for _, h := range hosts {
		if s, ok := restartStatusForHost(ctx, h); ok {
			out = append(out, s)
		}
	}
	return out
}

// refreshArtifact rebuilds the worktree's report.Artifact fresh -- mirroring
// cmd/omca/reportbuild.go's buildArtifactForCLI EXACTLY (the same
// detect-observe-compose-Build pipeline `omca`'s own TUI entry point,
// cmd/omca/tui.go's runTUI, already uses to build the Artifact this
// package's Model starts from) -- called after a successful stage/
// activate/rollback so the TUI's rendered views reflect what just changed
// on disk, never a stale snapshot from process startup. Duplicated, not
// imported, for the same reason as this file's other cmd/omca-mirrored
// helpers: buildArtifactForCLI is unexported in cmd/omca (package main).
func refreshArtifact(ctx ActionContext, now time.Time) (report.Artifact, error) {
	hosts := make([]report.HostInput, 0, len(hostcontext.DetectedHostIDs))
	for _, host := range hostcontext.DetectedHostIDs {
		hd, err := hostcontext.DetectHost(stdcontext.Background(), ctx.Env, host)
		if err != nil {
			return report.Artifact{}, fmt.Errorf("detecting %s: %w", host, err)
		}
		input := report.HostInput{Detection: hd}
		if hd.Installed {
			obs, err := observe.Observe(observe.Request{Detection: hd, WorktreeRoot: ctx.Worktree.Root})
			if err != nil {
				return report.Artifact{}, fmt.Errorf("observing %s: %w", host, err)
			}
			input.Observations = obs
		}
		hosts = append(hosts, input)
	}

	repo, repoErr := knowledge.Default()
	if repoErr != nil {
		// Degrade honestly, matching buildArtifactForCLI's identical stance:
		// a Knowledge Pack repository failure never hard-fails a refresh, it
		// just means the refreshed Artifact resolves no Knowledge Pack.
		repo = knowledge.Repository{}
	}

	req := report.BuildRequest{
		Worktree:         ctx.Worktree,
		WorktreeStateDir: ctx.WorktreeStateDir,
		Hosts:            hosts,
		Repository:       repo,
		Now:              now,
	}
	if composed, err := composeDesiredState(ctx, now); err == nil {
		req.Profiles = composed.Profiles
		req.Activation = composed.Activation
		req.Exceptions = composed.Exceptions
	}

	return report.Build(req)
}
