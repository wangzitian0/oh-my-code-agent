package runtime

import (
	"fmt"
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
	for i, o := range req.Observations {
		if o.Spec.Host.ID != req.Detection.Host {
			return fmt.Errorf("runtime: BootstrapRequest: Observations[%d] is for host %q, want %q (every observation must belong to the host this generation is being compiled for)", i, o.Spec.Host.ID, req.Detection.Host)
		}
		if o.Spec.Host.Version != req.Detection.Version {
			return fmt.Errorf("runtime: BootstrapRequest: Observations[%d] was gathered under host version %q, want %q (every observation must match the version this generation is being compiled for)", i, o.Spec.Host.Version, req.Detection.Version)
		}
		if o.Spec.Surface != req.surface() {
			return fmt.Errorf("runtime: BootstrapRequest: Observations[%d] is for surface %q, want %q (every observation must match the surface this generation is being compiled for)", i, o.Spec.Surface, req.surface())
		}
	}
	return nil
}

// surface returns req.Detection.Surface, defaulting to "cli" exactly like
// internal/observe.Observe does for the same field.
func (req BootstrapRequest) surface() string {
	if req.Detection.Surface != "" {
		return req.Detection.Surface
	}
	return defaultSurface
}
