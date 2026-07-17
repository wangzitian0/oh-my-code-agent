package runtime

import (
	"fmt"
	"path/filepath"
	"time"

	hostcontext "github.com/wangzitian0/oh-my-code-agent/internal/context"
	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
)

// BootstrapRequest is everything Bootstrap needs to compile one host's
// minimal bootstrap generation. Every value is caller-supplied and
// explicit -- like internal/observe.Request and internal/context.Environment,
// this package never reads an environment variable, a clock, or any other
// ambient state itself (see doc.go).
type BootstrapRequest struct {
	// Detection is the host this generation is being compiled for --
	// normally one entry from context.Detect's Report.Hosts. Only
	// Detection.Host, .Version, and .Surface are read; NativeHomes is not
	// (this package never reads native filesystem state directly -- that is
	// what produced Observations below).
	Detection hostcontext.HostDetection

	// Worktree is the worktree this generation belongs to (normally
	// context.DetectWorktree's result). Worktree.ID becomes
	// Generation.metadata.worktree and folds into GenerationID; Worktree.Root
	// is used to compute each copied Instructions file's path relative to
	// the repository root.
	Worktree hostcontext.Worktree

	// Observations is exactly this host's already-computed inventory
	// (normally observe.Observe's result for Detection.Host) -- both
	// user-global and repository-scope records. This is the compiler's one
	// source of "what exists, to explain every exclusion" (issue #13).
	Observations []domain.Observation

	// Now is the wall-clock time recorded in Generation.metadata.createdAt.
	// It is injected, never read via time.Now() internally, so a test can
	// assert exact manifest content and so GenerationID (which deliberately
	// excludes Now, see generationid.go) is provably clock-independent.
	Now time.Time

	// Parent is the previous generation ID this one supersedes, or nil for
	// a first/bootstrap generation (domain.GenerationMetadata.Parent).
	Parent *string

	// OMCABinaryPath is the absolute command a generation should register
	// as its MCP server's launch command (docs/architecture/runtime.md §3's
	// "the bootstrap generation contains... the OMCA MCP server";
	// docs/product/requirements.md FR-7). It is optional: a caller that
	// leaves it empty (every test in this package predating issue #15 does,
	// and any future caller that has no meaningful command to supply)
	// simply gets a generation with no MCP registration written into the
	// per-host config -- the exact scope cut doc.go's "What is deliberately
	// NOT in the generated tree yet" section documented, closed by a caller
	// that actually supplies this value (cmd/omca/env.go, cmd/omca/run.go).
	//
	// This is deliberately NOT a snapshot of the currently-running OMCA
	// binary's own resolved filesystem path (os.Executable()): that path
	// changes across every rebuild (and, worse, on every single `go run`
	// invocation -- verified during this PR's own development), which would
	// make GenerationID churn on every rebuild if this field participated
	// in it, or silently register a stale/nonexistent command if it did
	// not. Instead, cmd/omca passes the worktree's own stable PATH-shim
	// entry (worktreeStateDir/shims/omca -- cmd/omca/env.go's
	// shimEntryNames, refreshed to point at whatever omca binary is
	// currently running on every `omca env`/`omca run` call, exactly like
	// the existing codex/claude shim entries). That path is a deterministic
	// function of the worktree's own state directory alone -- it never
	// changes across an OMCA rebuild -- so it deliberately does NOT
	// participate in GenerationID (generationid.go): a generation compiled
	// yesterday and reused today under EnsureGeneration's content-addressed
	// cache still resolves the SAME command, which (by construction of the
	// shim refresh) always points at whatever omca binary is current at
	// invocation time -- arguably more correct than freezing a specific
	// build's path inside an old generation would be.
	OMCABinaryPath string
}

// validate rejects a request this package cannot compile: an unrecognized
// or unimplemented host, a worktree with no resolved identity/root, a
// missing injected clock value, or an observation that does not actually
// belong to the host/version/surface this generation targets (a caller
// composition bug -- e.g. accidentally passing both hosts' Observe output
// into one BootstrapRequest, or observations gathered under a stale host
// version after an upgrade -- that would otherwise silently leak one host's
// sources into another host's generation, or let GenerationID digest
// req.Detection.Version while the actual observed sources came from a
// different version, producing a manifest that cannot be reproduced or
// verified from its own stated inputs).
func (req BootstrapRequest) validate() error {
	if err := domain.ValidateHostID(req.Detection.Host); err != nil {
		return fmt.Errorf("runtime: BootstrapRequest: %w", err)
	}
	if _, err := NativeHomeDirName(req.Detection.Host); err != nil {
		return fmt.Errorf("runtime: BootstrapRequest: %w", err)
	}
	if req.Worktree.ID == "" {
		return fmt.Errorf("runtime: BootstrapRequest: Worktree.ID is required")
	}
	if req.Worktree.Root == "" {
		return fmt.Errorf("runtime: BootstrapRequest: Worktree.Root is required")
	}
	if req.Now.IsZero() {
		return fmt.Errorf("runtime: BootstrapRequest: Now is required (this package never reads the clock implicitly)")
	}
	if req.OMCABinaryPath != "" && !filepath.IsAbs(req.OMCABinaryPath) {
		return fmt.Errorf("runtime: BootstrapRequest: OMCABinaryPath %q is not absolute", req.OMCABinaryPath)
	}
	if err := validateObservationsBelongToHost(req.Observations, req.Detection.Host, req.Detection.Version, req.surface()); err != nil {
		return fmt.Errorf("runtime: BootstrapRequest: %w", err)
	}
	return nil
}

// surface returns req.Detection.Surface, defaulting to "cli" exactly like
// internal/observe.Observe does for the same field.
func (req BootstrapRequest) surface() string {
	return surfaceOf(req.Detection)
}

// surfaceOf returns detection.Surface, defaulting to "cli" exactly like
// internal/observe.Observe does for the same field. This is the general
// half of what used to be BootstrapRequest.surface() alone -- factored out
// so compile_full.go's CompileRequest (one hostcontext.HostDetection per
// host, no single "the" Detection field to hang a method off) computes the
// identical default without redefining it.
func surfaceOf(detection hostcontext.HostDetection) string {
	if detection.Surface != "" {
		return detection.Surface
	}
	return defaultSurface
}

// validateObservationsBelongToHost rejects an Observation that does not
// actually belong to the host/version/surface a generation is being
// compiled for -- the composition-bug check BootstrapRequest.validate()
// originally ran inline. Factored out so CompileRequest's per-host
// validation (compile_full.go) can run the exact same check for each of its
// several hosts without redefining it: both entry points have a "some
// Observations were computed for the wrong host" caller-composition bug to
// catch, and it is exactly the same bug either way (e.g. accidentally
// passing both hosts' Observe output into one host's slot, or observations
// gathered under a stale host version after an upgrade -- either would
// otherwise silently leak one host's sources into another host's
// generation, or let a generation ID digest a Detection.Version while the
// actual observed sources came from a different version, producing a
// manifest that cannot be reproduced or verified from its own stated
// inputs).
func validateObservationsBelongToHost(observations []domain.Observation, host, version, surface string) error {
	for i, o := range observations {
		if o.Spec.Host.ID != host {
			return fmt.Errorf("Observations[%d] is for host %q, want %q (every observation must belong to the host this generation is being compiled for)", i, o.Spec.Host.ID, host)
		}
		if o.Spec.Host.Version != version {
			return fmt.Errorf("Observations[%d] was gathered under host version %q, want %q (every observation must match the version this generation is being compiled for)", i, o.Spec.Host.Version, version)
		}
		if o.Spec.Surface != surface {
			return fmt.Errorf("Observations[%d] is for surface %q, want %q (every observation must match the surface this generation is being compiled for)", i, o.Spec.Surface, surface)
		}
	}
	return nil
}
