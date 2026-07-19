package main

import (
	stdcontext "context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	hostcontext "github.com/wangzitian0/oh-my-code-agent/internal/context"
	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
	"github.com/wangzitian0/oh-my-code-agent/internal/knowledge"
	"github.com/wangzitian0/oh-my-code-agent/internal/mcp"
	"github.com/wangzitian0/oh-my-code-agent/internal/observe"
	"github.com/wangzitian0/oh-my-code-agent/internal/profiles"
	"github.com/wangzitian0/oh-my-code-agent/internal/report"
	"github.com/wangzitian0/oh-my-code-agent/internal/runtime"
)

// sessionHostFromEnv determines which host actually launched this `omca mcp
// serve` process, for mcp.ComputeStatusRequest.SessionHost (issue #19's
// restart_required wiring) -- a documented judgment call, not a value this
// project passes explicitly anywhere today: internal/runtime.
// NativeHomeEnvVar names a distinct environment variable per host
// (CODEX_HOME, CLAUDE_CONFIG_DIR), and every managed launch path this
// project has (cmd/omca/run.go's runIsolated, internal/shim.Plan.Exec) sets
// exactly one of them, pointing into the generation directory that host was
// launched with, before exec'ing the host binary that in turn spawns this
// process as its own MCP server subprocess (docs/architecture/runtime.md
// §3/§7.1). Seeing one of these variables set is therefore a reliable-in-
// practice (if not schema-guaranteed) signal of which host this session
// belongs to; seeing none (or, defensively, both) means SessionHost stays
// empty and restart_required is left unreported rather than guessed.
func sessionHostFromEnv(env hostcontext.Environment) string {
	var found string
	for _, host := range hostcontext.DetectedHostIDs {
		envVar, err := runtime.NativeHomeEnvVar(host)
		if err != nil {
			continue
		}
		if env.Get(envVar) == "" {
			continue
		}
		if found != "" {
			// Both native-home variables are set (should not happen through
			// any managed launch path this project has) -- ambiguous,
			// report unknown rather than guessing.
			return ""
		}
		found = host
	}
	return found
}

// capabilityFuncForMCP builds the mcp.CapabilityFunc omca_propose's
// capability gate calls: a fresh (host, concept) -> domain.CapabilityOps
// lookup against this machine's real Knowledge Pack repository and a fresh
// detection of host's installed surface/version, exactly the same
// knowledge.Repository.Resolve(...).CapabilityFor(...) path internal/
// report/build.go's own Resolve+findPack uses -- called fresh on every
// omca_propose/omca_stage "tools/call" (mcp.CapabilityFunc's own doc
// comment), never cached across calls. Any failure along the way (cwd/
// worktree detection, host detection, Knowledge Pack repository load, an
// uninstalled or unqualified host) degrades to ok=false, which the
// capability gate treats as "not proven" -- fail closed, never an implicit
// pass.
func capabilityFuncForMCP() mcp.CapabilityFunc {
	return func(host, concept string) (domain.CapabilityOps, bool) {
		cwd, err := os.Getwd()
		if err != nil {
			return domain.CapabilityOps{}, false
		}
		wt, err := hostcontext.DetectWorktree(cwd)
		if err != nil {
			return domain.CapabilityOps{}, false
		}
		stateRoot, err := realStateRoot()
		if err != nil {
			return domain.CapabilityOps{}, false
		}
		shimDir := shimDirPath(worktreeStateDirPath(stateRoot, wt.ID))
		detectEnv := envWithFilteredPath(hostcontext.RealEnvironment(), shimDir)

		hd, err := hostcontext.DetectHost(stdcontext.Background(), detectEnv, host)
		if err != nil || !hd.Installed {
			return domain.CapabilityOps{}, false
		}
		repo, err := knowledge.Default()
		if err != nil {
			return domain.CapabilityOps{}, false
		}
		resolution := repo.Resolve(host, hd.Surface, hd.Version)
		if !resolution.Qualified {
			return domain.CapabilityOps{}, false
		}
		return resolution.CapabilityFor(concept), true
	}
}

// mergeHostActivation folds src's Enable/Disable selections into dst
// additively -- resolve.inSelection only ever checks list membership, so a
// duplicate entry across two merged sources is harmless.
func mergeHostActivation(dst, src domain.HostActivation) domain.HostActivation {
	dst.Enable.Skills = append(dst.Enable.Skills, src.Enable.Skills...)
	dst.Enable.MCPServers = append(dst.Enable.MCPServers, src.Enable.MCPServers...)
	dst.Disable.Skills = append(dst.Disable.Skills, src.Disable.Skills...)
	dst.Disable.MCPServers = append(dst.Disable.MCPServers, src.Disable.MCPServers...)
	return dst
}

// compileFuncForMCP builds the mcp.CompileFunc omca_stage calls, once
// ComputeStage has already fully re-validated a proposal at AUTO_STAGE: a
// fresh re-composition of the worktree's real desired state (internal/
// profiles.Compose, the identical composeFreshCompileRequest/
// buildArtifactForCLI pattern this file's siblings already use), hostActivations
// merged on top of the composed Activation's own spec.hosts, compiled via
// internal/runtime.Compile (reusing the compiled generation, by content-
// addressed ID, if this exact desired state was already compiled once
// before -- EnsureGeneration's own idempotency precedent, current.go), and
// recorded as EVERY named host's "pending" pointer via
// runtime.SetPendingGeneration -- never runtime.SetCurrentGeneration,
// which is how this function keeps "current" untouched (mcp.CompileFunc's
// own doc comment's MUST list).
//
// Metadata.Parent (issue #68): this function compiles ONE shared generation
// for potentially several named hosts at once (hostIDs can be
// ["codex", "claude-code"] in a single call), but domain.GenerationMetadata.
// Parent is a single *string on that one generation, not per-host, while
// each named host has its own independent "current" generation
// (currentByHost, below, keyed the same way). The reviewed decision (this
// issue's own recommended default, matching this project's "record real
// state, don't guess or silently drop information" bias --
// internal/profiles.AmbiguousIdentityError's identical stance of surfacing
// ambiguity as data rather than picking arbitrarily): a host with no
// current generation yet (its first-ever activation) does not count against
// agreement, since there is nothing for it to diverge from. If every named
// host that DOES have a current generation agrees on the same ID, that ID
// becomes Parent. If two or more named hosts' current generations genuinely
// differ, Parent is left nil, exactly like before this fix -- the
// pre-existing, honest "nothing to roll back to" refusal
// (internal/runtime/rollback.go's own nil-Parent doc comment), not a
// regression. This closes "at least the common single-host-changed-at-a-
// time case" (issue #68's exit gate), which is also the overwhelmingly
// common real usage pattern -- one host reactivated at a time -- while
// deliberately leaving the genuinely-diverged multi-host case unresolved
// rather than guessing. A future per-host Parent (e.g. a map instead of one
// *string) would close that case too, but that is a
// domain.GenerationMetadata schema change belonging to whoever next needs
// it, not an unreviewed judgment call folded into this fix.
func compileFuncForMCP(stderr io.Writer) mcp.CompileFunc {
	return func(hostActivations map[string]domain.HostActivation) (domain.Generation, map[string]domain.Generation, error) {
		cwd, err := os.Getwd()
		if err != nil {
			return domain.Generation{}, nil, err
		}
		wt, err := hostcontext.DetectWorktree(cwd)
		if err != nil {
			return domain.Generation{}, nil, err
		}
		stateRoot, err := realStateRoot()
		if err != nil {
			return domain.Generation{}, nil, err
		}
		worktreeStateDir := worktreeStateDirPath(stateRoot, wt.ID)
		shimDir := shimDirPath(worktreeStateDir)
		generationsDir := filepath.Join(worktreeStateDir, "generations")

		configRoot, err := realConfigRoot()
		if err != nil {
			return domain.Generation{}, nil, err
		}
		profileDirs, bindingDirs, exceptionDirs := compositionDirsFor(configRoot, wt.Root)
		now := time.Now()
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
			return domain.Generation{}, nil, fmt.Errorf("composing desired state: %w", err)
		}

		mergedActivation := composition.Activation
		if mergedActivation.Spec.Hosts == nil {
			mergedActivation.Spec.Hosts = map[string]domain.HostActivation{}
		}
		hostIDs := make([]string, 0, len(hostActivations))
		for h, ha := range hostActivations {
			hostIDs = append(hostIDs, h)
			mergedActivation.Spec.Hosts[h] = mergeHostActivation(mergedActivation.Spec.Hosts[h], ha)
		}
		sort.Strings(hostIDs)

		mergedActivation.APIVersion = domain.SupportedAPIVersion
		mergedActivation.Kind = "Activation"
		mergedActivation.Metadata.Worktree = wt.ID

		realEnv := hostcontext.RealEnvironment()
		detectEnv := envWithFilteredPath(realEnv, shimDir)

		currentByHost := map[string]domain.Generation{}
		detections := make(map[string]hostcontext.HostDetection, len(hostIDs))
		hosts := make([]runtime.HostCompileInput, 0, len(hostIDs))
		for _, h := range hostIDs {
			hd, err := hostcontext.DetectHost(stdcontext.Background(), detectEnv, h)
			if err != nil {
				return domain.Generation{}, nil, fmt.Errorf("detecting %s: %w", h, err)
			}
			if !hd.Installed {
				return domain.Generation{}, nil, fmt.Errorf("host %q is not installed -- cannot compile a pending generation for it", h)
			}
			detections[h] = hd
			obs, err := observe.Observe(observe.Request{Detection: hd, WorktreeRoot: wt.Root})
			if err != nil {
				return domain.Generation{}, nil, fmt.Errorf("observing %s: %w", h, err)
			}
			hosts = append(hosts, runtime.HostCompileInput{Detection: hd, Observations: obs, OMCABinaryPath: omcaCommandPath(shimDir)})

			curDir, cerr := runtime.CurrentGenerationDir(worktreeStateDir, h)
			switch {
			case cerr == nil:
				g, rerr := runtime.ReadGenerationManifest(curDir)
				if rerr != nil {
					// The "current" pointer for h resolves to a real path,
					// but its manifest is missing/corrupt -- a different,
					// worse situation than "h has never been activated"
					// (the os.IsNotExist(cerr) branch below), and treating
					// it the same would silently derive Parent as if h had
					// no prior generation at all. Fail loudly instead,
					// mirroring this function's own cache-hit manifest
					// check a few lines below ("only a genuinely missing
					// manifest is a real cache miss worth compiling into") --
					// a Copilot review finding on issue #68's own PR.
					return domain.Generation{}, nil, fmt.Errorf("host %q has a current-generation pointer at %s but its manifest is unreadable, refusing to guess its Parent: %w", h, curDir, rerr)
				}
				currentByHost[h] = g
			case os.IsNotExist(cerr):
				// h has never been activated in this worktree -- genuinely
				// no current generation, not an error.
			default:
				return domain.Generation{}, nil, fmt.Errorf("host %q: resolving current-generation pointer: %w", h, cerr)
			}
		}

		// Parent: see this function's own doc comment for the full reviewed
		// decision. Walk hostIDs (sorted, so this is deterministic) and
		// collect the current generation ID of every named host that has
		// one; a host with no entry in currentByHost is skipped (first-ever
		// activation, does not count against agreement). Agreement across
		// every host that DOES have one -> that ID becomes Parent. Any two
		// disagreeing -> Parent stays nil, the same honest
		// "nothing to roll back to" state Rollback already refuses on.
		var parent *string
		diverged := false
		for _, h := range hostIDs {
			g, ok := currentByHost[h]
			if !ok {
				continue
			}
			if parent == nil {
				id := g.Metadata.ID
				parent = &id
			} else if *parent != g.Metadata.ID {
				diverged = true
				break
			}
		}
		if diverged {
			parent = nil
		}

		// Durably persist the merged Enable/Disable selection before
		// compiling, not just in memory: activate.go's own
		// composeFreshCompileRequest recomposes desired state fresh from
		// this exact file (profiles.Compose's ActivationPath) on a LATER,
		// independent `omca activate` call, and has no way to see a
		// selection this call only ever held in its own local variable. Left
		// unpersisted, that later CAS check recomputes a desired state that
		// disagrees with what was actually staged here, and Activate
		// rejects the pending generation as stale -- a real, pre-existing
		// production gap surfaced (and fixed at its root,
		// profiles.PersistActivation) while building issue #35's TUI action
		// layer, whose own stageAssetActivation does the identical fix.
		//
		// Persisted only here, after every detect/observe/Parent-resolution
		// step above has already succeeded -- not immediately after merging
		// -- so a common, easy-to-hit failure (a named host not installed)
		// never durably mutates the worktree's activation.yaml on a call
		// that goes on to fail anyway (the identical ordering fix Copilot
		// flagged on this same PR's TUI-side stageAssetActivation).
		if err := profiles.PersistActivation(worktreeStateDir, mergedActivation); err != nil {
			return domain.Generation{}, nil, fmt.Errorf("persisting worktree activation: %w", err)
		}

		req := runtime.CompileRequest{
			Worktree:   wt,
			Hosts:      hosts,
			Profiles:   composition.Profiles,
			Activation: mergedActivation,
			Exceptions: composition.Exceptions,
			Now:        now,
			Invocation: "omca_stage",
			Parent:     parent,
		}

		genID, err := runtime.CompileGenerationID(req)
		if err != nil {
			return domain.Generation{}, nil, fmt.Errorf("computing generation ID: %w", err)
		}
		outputDir := filepath.Join(generationsDir, runtime.DirSafeID(genID))

		// Note: genID (and therefore outputDir) deliberately excludes Parent
		// (generationid.go's own doc comment -- Parent never changes the
		// compiled artifact tree's content), so a cache hit below can find a
		// manifest recorded with a DIFFERENT Parent than this call just
		// freshly computed (e.g. identical desired state re-staged after
		// some other activation moved "current" on). runtime.
		// ReconcileGenerationParent below corrects the on-disk manifest to
		// this call's freshly-computed Parent when they disagree -- a real
		// Copilot review finding on issue #68's own PR: an uncorrected stale
		// Parent makes a later Rollback restore an unexpected, older
		// generation instead of the one this activation actually preceded.
		gen, err := runtime.ReadGenerationManifest(outputDir)
		if err != nil {
			if !os.IsNotExist(err) {
				// outputDir exists but its manifest is present and
				// unreadable/invalid -- the same "refuse to overwrite a
				// broken content-addressed path" invariant
				// runtime.EnsureGeneration already enforces elsewhere.
				// Recompiling here would silently paper over corruption at
				// a path this compiler does not own once something else
				// has written into it; only a genuinely missing manifest
				// (ENOENT, "nothing compiled here yet") is a real cache
				// miss worth compiling into.
				return domain.Generation{}, nil, fmt.Errorf("existing generation directory %s is present but its manifest failed validation, refusing to overwrite a content-addressed path: %w", outputDir, err)
			}
			gen, err = runtime.Compile(req, outputDir)
			if err != nil {
				return domain.Generation{}, nil, fmt.Errorf("compiling: %w", err)
			}
		} else {
			gen, err = runtime.ReconcileGenerationParent(outputDir, parent)
			if err != nil {
				return domain.Generation{}, nil, fmt.Errorf("reconciling cached generation's Parent: %w", err)
			}
		}

		for _, h := range hostIDs {
			if err := runtime.SetPendingGeneration(worktreeStateDir, h, outputDir, gen, detections[h], now); err != nil {
				return domain.Generation{}, nil, fmt.Errorf("recording pending generation for %s: %w", h, err)
			}
			fmt.Fprintf(stderr, "omca: mcp: omca_stage: %s -> pending generation %s (%s)\n", h, gen.Metadata.ID, outputDir)
		}

		return gen, currentByHost, nil
	}
}

// runMCP implements `omca mcp serve` (issue #15, docs/architecture/
// runtime.md §6's OMCA MCP server): starts the stdio JSON-RPC 2.0 server
// (internal/mcp.Serve) against stdin/stdout, answering omca_status,
// omca_query, omca_propose, and omca_stage from the CURRENT process's
// environment. This is the exact command internal/runtime/compile.go's
// hostConfigFiles registers as a managed generation's MCP server entry — a
// host that launches this managed session spawns `<omcaBinaryPath> mcp
// serve` as a subprocess, and that subprocess inherits the launching
// process's environment (the same OMCA_RUN_ID/OMCA_STATE_DIR/
// OMCA_WORKTREE_ID/OMCA_CONTEXT_ID docs/architecture/runtime.md §7.1 shows
// `omca run`/the PATH shim setting before exec'ing the host binary), so
// reading them here — exactly like checkSessionManaged/checkPathBypass in
// doctor.go already read managed-session state from the environment — is
// how this process learns which worktree/generation it is answering for,
// without any argument or config file of its own, and it is the ONLY place
// that reads them: none of mcp.StatusFunc/ArtifactFunc/CapabilityFunc/
// CompileFunc take a worktree/run/generation argument at call time (see
// internal/mcp/query.go's QueryArguments doc comment), so nothing a
// tool-call argument names can ever redirect any of the four away from this
// one binding.
//
// omca_query's mcp.ArtifactFunc (and omca_propose/omca_stage's shared use of
// the same fresh report.Artifact, via ProposeContext) is wired to
// buildArtifactForCLI — the exact same detect-observe-compose-Build
// pipeline every `omca report`/`omca drift`/`omca explain`/... CLI command
// already runs fresh for its own single invocation
// (cmd/omca/reportbuild.go) — called fresh for every "tools/call"
// (mcp.ArtifactFunc's own doc comment: "never answer from a value computed
// once at startup"), never once at server startup: this process is
// long-lived, but the report it answers from must reflect whatever is on
// disk AT CALL TIME, exactly like omca_status's statusFn below already does
// for the generation it reports.
//
// stdin/stdout/stderr are accepted as explicit parameters (like every other
// runX function in this package) so the pre-Serve argument-validation path
// stays testable without a real subprocess; the stdio MCP loop itself
// (internal/mcp.Serve) is exercised directly against os.Stdin/os.Stdout only
// from main(), consistent with this project's "syscall.Exec-adjacent code
// gets a real subprocess test, decision logic gets an in-process one"
// precedent (cmd/omca/shim_test.go's doc comment).
func runMCP(stdin io.Reader, stdout, stderr io.Writer, args []string) int {
	if len(args) != 1 || args[0] != "serve" {
		fmt.Fprintln(stderr, "usage: omca mcp serve")
		return 2
	}

	env := hostcontext.RealEnvironment()
	sessionHost := sessionHostFromEnv(env)
	statusFn := func() (mcp.StatusResult, error) {
		return mcp.ComputeStatus(mcp.ComputeStatusRequest{
			WorktreeID:          env.Get("OMCA_WORKTREE_ID"),
			ContextID:           env.Get("OMCA_CONTEXT_ID"),
			WorktreeStateDir:    env.Get("OMCA_STATE_DIR"),
			Hosts:               hostcontext.DetectedHostIDs,
			SessionHost:         sessionHost,
			SessionGenerationID: env.Get("OMCA_RUN_ID"),
		})
	}
	queryFn := func() (report.Artifact, error) {
		return buildArtifactForCLI(stderr, time.Now())
	}
	capabilityFn := capabilityFuncForMCP()
	compileFn := compileFuncForMCP(stderr)

	registry := mcp.NewRegistry(
		mcp.StatusToolEntry(statusFn),
		mcp.QueryToolEntry(queryFn),
		mcp.ProposeToolEntry(queryFn, capabilityFn),
		mcp.StageToolEntry(queryFn, capabilityFn, compileFn),
	)

	if err := mcp.Serve(stdin, stdout, registry); err != nil {
		fmt.Fprintf(stderr, "omca: mcp: %v\n", err)
		return 1
	}
	return 0
}
