package runtime

import (
	"fmt"
	"path/filepath"
	"time"

	hostcontext "github.com/wangzitian0/oh-my-code-agent/internal/context"
	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
)

// RollbackResult is Rollback's success value: which generation is now
// current, and which one it superseded.
type RollbackResult struct {
	Host                   string
	RestoredGenerationID   string // the parent generation, now current
	SupersededGenerationID string // the generation current named before rollback
	RolledBackAt           string
}

// Rollback restores host's parent generation as "current" -- the M2 AC
// "rollback restores the parent generation and is itself ledgered"
// (docs/architecture/runtime.md §5.4: "If verification fails, OMCA can
// restore the parent generation and relaunch") and FR-9's "reversible"
// requirement.
//
// It reads the CURRENT generation's own domain.Generation.Metadata.Parent
// field to learn what to restore -- there is no separate parent-tracking
// mechanism (internal/domain.Generation.Metadata.Parent's doc comment is
// exactly this field's job) -- resolves that generation ID back to its
// on-disk directory under generationsRoot using the same content-addressed
// path convention EnsureGeneration already establishes
// (generationsRoot/<DirSafeID(id)>, EnsureGeneration's own outputDir
// computation), atomically switches "current" to it (SetCurrentGeneration,
// the same atomic rename-based write Activate's own switch step uses), and
// appends a "rolledback" Ledger entry.
//
// generationsRoot is caller-supplied and never resolved internally, matching
// EnsureGeneration's and Compile's identical discipline; detection is the
// caller's current HostDetection (used only for the CurrentRecord sidecar
// SetCurrentGeneration writes -- Rollback does not itself detect anything).
//
// Rollback refuses to run (a clear error, never a guess) when: there is no
// current generation to roll back from, the current generation names no
// parent at all (Metadata.Parent is nil -- e.g. the very first activation
// in a worktree), or the named parent generation is not actually present at
// its expected content-addressed path (garbage-collected, or never compiled
// in this worktree's generationsRoot at all).
func Rollback(worktreeStateDir, generationsRoot, host string, detection hostcontext.HostDetection, now time.Time) (RollbackResult, error) {
	if worktreeStateDir == "" {
		return RollbackResult{}, fmt.Errorf("runtime: Rollback: worktreeStateDir is required")
	}
	if generationsRoot == "" {
		return RollbackResult{}, fmt.Errorf("runtime: Rollback: generationsRoot is required")
	}
	if !filepath.IsAbs(generationsRoot) {
		return RollbackResult{}, fmt.Errorf("runtime: Rollback: generationsRoot %q is not absolute", generationsRoot)
	}
	if err := domain.ValidateHostID(host); err != nil {
		return RollbackResult{}, fmt.Errorf("runtime: Rollback: %w", err)
	}
	if now.IsZero() {
		return RollbackResult{}, fmt.Errorf("runtime: Rollback: now is required (this package never reads the clock implicitly)")
	}

	currentDir, err := CurrentGenerationDir(worktreeStateDir, host)
	if err != nil {
		return RollbackResult{}, fmt.Errorf("runtime: Rollback: no current generation for %s to roll back from: %w", host, err)
	}
	currentGen, err := ReadGenerationManifest(currentDir)
	if err != nil {
		return RollbackResult{}, fmt.Errorf("runtime: Rollback: current generation manifest for %s at %s is unreadable: %w", host, currentDir, err)
	}
	if currentGen.Metadata.Parent == nil || *currentGen.Metadata.Parent == "" {
		return RollbackResult{}, fmt.Errorf("runtime: Rollback: current generation %s for %s has no parent generation recorded; nothing to roll back to", currentGen.Metadata.ID, host)
	}
	parentID := *currentGen.Metadata.Parent

	parentDir := filepath.Join(generationsRoot, DirSafeID(parentID))
	parentGen, err := ReadGenerationManifest(parentDir)
	if err != nil {
		return RollbackResult{}, fmt.Errorf("runtime: Rollback: parent generation %s for %s is not available at %s (garbage-collected, or never compiled into this worktree's generationsRoot): %w", parentID, host, parentDir, err)
	}
	if parentGen.Metadata.ID != parentID {
		// Should be unreachable, matching EnsureGeneration's identical
		// paranoia check: the directory's own name is derived from parentID,
		// so a manifest there naming a different ID means something outside
		// this package wrote into a content-addressed path it does not own.
		return RollbackResult{}, fmt.Errorf("runtime: Rollback: %s contains a manifest for generation %q, expected the parent %q", parentDir, parentGen.Metadata.ID, parentID)
	}

	if err := SetCurrentGeneration(worktreeStateDir, host, parentDir, parentGen, detection, now); err != nil {
		return RollbackResult{}, fmt.Errorf("runtime: Rollback: switching current for %s: %w", host, err)
	}

	if err := AppendLedgerEntry(worktreeStateDir, host, LedgerEntry{
		Host:         host,
		GenerationID: parentGen.Metadata.ID,
		Kind:         "rolledback",
		RecordedAt:   now.UTC().Format(time.RFC3339),
		Detail:       fmt.Sprintf("restored parent generation %q, superseding %q", parentID, currentGen.Metadata.ID),
	}); err != nil {
		return RollbackResult{}, fmt.Errorf("runtime: Rollback: appending ledger entry for %s: %w", host, err)
	}

	return RollbackResult{
		Host:                   host,
		RestoredGenerationID:   parentGen.Metadata.ID,
		SupersededGenerationID: currentGen.Metadata.ID,
		RolledBackAt:           now.UTC().Format(time.RFC3339),
	}, nil
}
