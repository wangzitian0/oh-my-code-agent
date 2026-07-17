package runtime

import (
	"fmt"
	"os"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
)

// RestartStatus reports, for one host, whether a running session is
// superseded by "current" -- docs/architecture/runtime.md §5.5: "Activation
// advances the worktree's current pointer, but a running session keeps the
// generation it was launched with... After activation, omca status and omca
// doctor report sessions still running on a superseded generation and which
// hosts require a restart. restart_required is therefore per host, not per
// worktree."
type RestartStatus struct {
	// Host is the canonical host ID this status is about.
	Host string
	// SessionGenerationID is the generation a running session for Host was
	// launched with -- normally OMCA_RUN_ID (docs/architecture/runtime.md
	// §7.1, internal/shim.Plan.Exec/cmd/omca/run.go's runIsolated), supplied
	// by the caller (this package never reads an environment variable
	// itself).
	SessionGenerationID string
	// CurrentGenerationID is a fresh read of Host's "current" generation at
	// the moment DetectRestartRequired ran. Empty when Host has no current
	// generation recorded at all (see Detail in that case).
	CurrentGenerationID string
	// RestartRequired is true when SessionGenerationID no longer equals
	// CurrentGenerationID: some other activation moved "current" out from
	// under this running session.
	RestartRequired bool
	// Detail is a human-readable explanation, always non-empty.
	Detail string
}

// DetectRestartRequired compares sessionGenerationID -- the generation ID a
// running session for host was launched with -- against a fresh read of
// host's "current" generation under worktreeStateDir, and reports whether
// that session is now running on a superseded generation.
//
// This is deliberately a different question from checkStaleGeneration
// (cmd/omca/doctor.go): that check asks "does the CURRENT generation still
// match what fresh native inputs would compile right now" (has the world
// drifted from what was last compiled); this function asks "is a SPECIFIC
// ALREADY-RUNNING session still pointed at whatever the worktree calls
// current, after some OTHER activation moved current out from under it."
// Neither implies the other: current can be perfectly fresh while a session
// launched five activations ago is still running the oldest of them, and
// current can be stale while every running session still matches it exactly
// (nothing has been activated since they launched, even though the native
// environment has drifted).
//
// This function has no process-tracking of its own -- it never asks the OS
// "is a process for this generation still alive," only "does this
// generation ID still match current." A caller that has independently
// established a session for host is running (e.g. an MCP server subprocess
// that inherited OMCA_RUN_ID from the host process that spawned it, see
// internal/mcp's wiring) supplies that generation ID; this function is pure
// beyond the one CurrentGenerationDir/ReadGenerationManifest read.
func DetectRestartRequired(worktreeStateDir, host, sessionGenerationID string) (RestartStatus, error) {
	if worktreeStateDir == "" {
		return RestartStatus{}, fmt.Errorf("runtime: DetectRestartRequired: worktreeStateDir is required")
	}
	if err := domain.ValidateHostID(host); err != nil {
		return RestartStatus{}, fmt.Errorf("runtime: DetectRestartRequired: %w", err)
	}
	if sessionGenerationID == "" {
		return RestartStatus{}, fmt.Errorf("runtime: DetectRestartRequired: sessionGenerationID is required")
	}

	currentDir, err := CurrentGenerationDir(worktreeStateDir, host)
	if err != nil {
		if os.IsNotExist(err) {
			return RestartStatus{
				Host:                host,
				SessionGenerationID: sessionGenerationID,
				RestartRequired:     false,
				Detail:              fmt.Sprintf("%s has no current generation recorded in this worktree; cannot determine whether session generation %s is superseded", host, sessionGenerationID),
			}, nil
		}
		return RestartStatus{}, fmt.Errorf("runtime: DetectRestartRequired: current-generation pointer for %s is corrupt: %w", host, err)
	}
	currentGen, err := ReadGenerationManifest(currentDir)
	if err != nil {
		return RestartStatus{}, fmt.Errorf("runtime: DetectRestartRequired: current generation manifest for %s at %s is unreadable: %w", host, currentDir, err)
	}

	required := currentGen.Metadata.ID != sessionGenerationID
	detail := fmt.Sprintf("%s session is running on current generation %s; no restart required", host, sessionGenerationID)
	if required {
		detail = fmt.Sprintf("%s session was launched with generation %s, but current is now %s; restart %s to pick up the activated change", host, sessionGenerationID, currentGen.Metadata.ID, host)
	}

	return RestartStatus{
		Host:                host,
		SessionGenerationID: sessionGenerationID,
		CurrentGenerationID: currentGen.Metadata.ID,
		RestartRequired:     required,
		Detail:              detail,
	}, nil
}
