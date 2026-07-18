package runtime

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
)

// VerificationResult is [VerifyActivation]'s outcome for one host's current
// generation.
type VerificationResult struct {
	// Host is the canonical host ID verified.
	Host string
	// GenerationID is the generation that was verified -- host's "current"
	// generation at the moment VerifyActivation ran.
	GenerationID string
	// Passed is true when every recorded artifact digest still matches the
	// on-disk content.
	Passed bool
	// FailedArtifacts lists every artifact path (relative to the
	// generation directory) that failed verification -- either missing/
	// unreadable, or present with content that no longer digests to what
	// the manifest recorded. Empty when Passed.
	FailedArtifacts []string
	// Detail is a human-readable summary, always non-empty.
	Detail string
}

// VerifyActivation re-reads host's CURRENT generation directory under
// worktreeStateDir and recomputes every artifact digest
// [domain.GenerationHostEntry.Artifacts] recorded for host at compile time
// (internal/runtime/compile_full.go's Compile, which stamps each rendered
// file with domain.CanonicalDigest(content) at the moment it writes it),
// comparing each one against a fresh read of the same path today.
//
// This is docs/architecture/runtime.md's MVP acceptance scenario item 7,
// "activate it after restart and verify the new effective state," and the
// M5 exit gate line "failed verification leaves a recoverable previous
// generation" -- read literally as "prove the generation Activate just
// switched 'current' to is still, byte for byte, the generation Compile
// actually produced."
//
// This is a DIFFERENT question from Activate's own CAS check
// (freshSourceDigest, activate.go): the CAS check runs BEFORE the switch
// and asks "do the pending generation's own DESIRED-STATE INPUTS still
// match a fresh recomputation" (has the world drifted since compile time);
// VerifyActivation runs AFTER the switch and asks "does the generation
// directory Activate just made 'current' still contain, on disk, exactly
// what its own manifest says it does" (has the compiled OUTPUT itself been
// corrupted, partially written, or tampered with since compile time,
// including by whatever raced the switch itself). Neither implies the
// other -- freshSourceDigest never reads a single byte from
// worktreeStateDir/generations, and VerifyActivation never re-observes
// anything outside the generation directory itself.
//
// VerifyActivation performs no writes and no subprocess execution: it is a
// pure filesystem read confined entirely to
// worktreeStateDir/generations/<current generation>, the same OMCA-owned,
// already-isolated tree Compile itself wrote -- never a real host's native
// home, and never anything requiring a live host process. This is what
// makes it safe to run unconditionally after every real Activate call (see
// [ActivateAndVerify]), not just in a test against a fixture.
//
// A host with no current generation, an unreadable manifest, or a manifest
// naming no host entry for host is a genuine verification failure, not a
// silently-skipped no-op: there is nothing recoverable to report as
// "verified," and a caller (ActivateAndVerify) that skipped verification
// for exactly the cases where something is already wrong would defeat the
// whole point of this function.
//
// # Why artifact-digest integrity, not internal/assurance's EffectiveGraph
//
// internal/assurance.VerifyGraph re-derives EvidenceLevel for an
// ALREADY-COMPUTED effective.EffectiveGraph against the committed evidence-
// ceiling table -- a report-time concern (internal/report/build.go is its
// only production caller) that answers "how much do we trust this already-
// resolved value," not "did this specific activation actually take." Using
// it here would require re-observing the activated generation's own
// compiled tree as a synthetic host home (pointing a fresh
// hostcontext.HostDetection's NativeHomes at
// generationDir/hosts/<host>/<surface>/<nativeHomeDir>), resolving a
// HostKnowledge Pack for it, and computing a fresh EffectiveGraph -- real,
// valuable, but a materially larger, differently-scoped verification
// surface (internal/runtime has no existing dependency on
// internal/knowledge/internal/effective/internal/assurance at all) than
// this PR's own time and risk budget allows to introduce, test, and prove
// safe alongside Bisect in the same change.
//
// Artifact-digest integrity is deliberately the narrower, structurally
// simpler question this function answers instead: "is the generation
// directory Activate just switched 'current' to, byte for byte, still what
// Compile actually produced." It is a real, non-trivial check (proven by
// TestVerifyActivation_DetectsTamperedArtifact) and a genuine instance of
// "a compiled generation's effective state not matching what activation
// was supposed to produce" -- just a narrower slice of that question than
// a full EffectiveGraph re-derivation would cover (e.g. it cannot catch a
// compiler bug where Compile's own manifest claims a source is included
// but renders it somewhere internal/observe's rules would never discover
// it as that source again). That richer, EffectiveGraph-based check is a
// reasonable enhancement, tracked as a follow-up
// (github.com/wangzitian0/oh-my-code-agent/issues/70) rather than folded
// into this PR unreviewed.
func VerifyActivation(worktreeStateDir, host string, now time.Time) (VerificationResult, error) {
	if worktreeStateDir == "" {
		return VerificationResult{}, fmt.Errorf("runtime: VerifyActivation: worktreeStateDir is required")
	}
	if err := domain.ValidateHostID(host); err != nil {
		return VerificationResult{}, fmt.Errorf("runtime: VerifyActivation: %w", err)
	}
	if now.IsZero() {
		return VerificationResult{}, fmt.Errorf("runtime: VerifyActivation: now is required (this package never reads the clock implicitly)")
	}

	currentDir, err := CurrentGenerationDir(worktreeStateDir, host)
	if err != nil {
		return VerificationResult{}, fmt.Errorf("runtime: VerifyActivation: no current generation for %s to verify: %w", host, err)
	}
	gen, err := ReadGenerationManifest(currentDir)
	if err != nil {
		return VerificationResult{}, fmt.Errorf("runtime: VerifyActivation: current generation manifest for %s at %s is unreadable: %w", host, currentDir, err)
	}
	entry, ok := gen.Spec.Hosts[host]
	if !ok {
		return VerificationResult{}, fmt.Errorf("runtime: VerifyActivation: current generation %s for %s has no host entry for %q in its own manifest", gen.Metadata.ID, host, host)
	}

	var failed []string
	for _, a := range entry.Artifacts {
		full := filepath.Join(currentDir, a.Path)
		content, readErr := os.ReadFile(full)
		if readErr != nil {
			failed = append(failed, fmt.Sprintf("%s: %v", a.Path, readErr))
			continue
		}
		// Mirrors Compile's own digest computation exactly
		// (compile_full.go: domain.CanonicalDigest(string(f.Content))) --
		// reusing the identical call shape is what makes "same bytes, same
		// digest" a structural guarantee rather than two independently
		// maintained digest schemes that could drift apart from each
		// other for reasons that have nothing to do with the file actually
		// changing (the same reasoning activate.go's freshSourceDigest doc
		// comment gives for reusing hostSourcesFor/aggregateSources).
		digest, digestErr := domain.CanonicalDigest(string(content))
		if digestErr != nil {
			failed = append(failed, fmt.Sprintf("%s: computing digest: %v", a.Path, digestErr))
			continue
		}
		if digest != a.Digest {
			failed = append(failed, a.Path)
		}
	}

	if len(failed) > 0 {
		return VerificationResult{
			Host:            host,
			GenerationID:    gen.Metadata.ID,
			Passed:          false,
			FailedArtifacts: failed,
			Detail:          fmt.Sprintf("%d of %d artifact(s) for %s no longer match generation %s's recorded manifest digests: %v", len(failed), len(entry.Artifacts), host, gen.Metadata.ID, failed),
		}, nil
	}
	return VerificationResult{
		Host:         host,
		GenerationID: gen.Metadata.ID,
		Passed:       true,
		Detail:       fmt.Sprintf("%d artifact(s) for %s match generation %s's recorded manifest digests", len(entry.Artifacts), host, gen.Metadata.ID),
	}, nil
}

// ActivateAndVerifyResult is [ActivateAndVerify]'s success value.
type ActivateAndVerifyResult struct {
	Activation   ActivationResult
	Verification VerificationResult
	// RolledBack is true when a failed Verification triggered an automated
	// Rollback that itself succeeded.
	RolledBack bool
	// Rollback is non-nil exactly when RolledBack is true.
	Rollback *RollbackResult
}

// ActivateAndVerify runs [Activate], then immediately [VerifyActivation]
// against the generation Activate just switched "current" to, automatically
// triggering [Rollback] to the parent generation when verification fails --
// the M5 AC this PR (issue #28) exists to close: "failed post-activation
// verification triggers automated rollback to the parent; both events are
// ledgered."
//
// "Both events" are:
//
//  1. a "verification-failed" Ledger entry for the generation that failed
//     verification, appended BEFORE any rollback is attempted -- so the
//     failure itself is durably recorded even if the automated rollback
//     that follows cannot proceed (e.g. no parent generation exists yet,
//     see below);
//  2. Rollback's own "rolledback" entry (rollback.go), appended by Rollback
//     itself exactly as it already is for a manually invoked `omca
//     rollback`.
//
// A VerifyActivation call that itself errors (e.g. the current pointer is
// somehow corrupt immediately after a successful switch) is treated
// identically to Passed=false, never silently ignored: an inability to
// positively confirm the activated generation's integrity is not
// meaningfully different from a confirmed failure for the purpose of
// deciding whether to roll back -- this function's whole contract is "never
// leave a generation whose integrity could not be confirmed installed as
// 'current' without at least trying to recover."
//
// If the automated Rollback itself cannot proceed -- most notably, the
// activated generation has no parent recorded (e.g. this was the very
// first activation for host in this worktree) -- ActivateAndVerify returns
// a non-nil error explaining both the original verification failure and why
// no automatic recovery was possible, but the "verification-failed" Ledger
// entry (event 1, above) has already been durably recorded by that point:
// docs/project/roadmap.md's M5 exit gate line "failed verification leaves a
// recoverable previous generation" is a property of activations that HAVE a
// previous generation; a first activation with no predecessor is honestly
// outside what any rollback -- automated or manual -- can promise to
// recover, and this function never pretends otherwise.
//
// generationsRoot is the same caller-resolved, absolute
// worktreeStateDir/generations path every other Rollback caller already
// passes (see rollback.go's runRollback) -- required only on the failure
// path, but always validated up front by delegating straight to Rollback's
// own checks.
func ActivateAndVerify(req ActivateRequest, generationsRoot string) (ActivateAndVerifyResult, error) {
	actResult, err := Activate(req)
	if err != nil {
		return ActivateAndVerifyResult{}, err
	}

	verResult, verErr := VerifyActivation(req.WorktreeStateDir, req.Host, req.Now)
	if verErr != nil {
		verResult = VerificationResult{
			Host:         req.Host,
			GenerationID: actResult.ActivatedGenerationID,
			Passed:       false,
			Detail:       fmt.Sprintf("post-activation verification could not run: %v", verErr),
		}
	}

	result := ActivateAndVerifyResult{Activation: actResult, Verification: verResult}
	if verResult.Passed {
		return result, nil
	}

	// Event 1: the verification failure itself, ledgered BEFORE any
	// rollback attempt -- best-effort in the sense that its own failure
	// must never mask the real verification failure being reported, but
	// deliberately attempted unconditionally, even on a path where
	// rollback itself is about to fail too (e.g. no parent), matching
	// Activate's identical "record the rejected attempt, then return the
	// real error" discipline for a CAS rejection.
	ledgerGenID := verResult.GenerationID
	if ledgerGenID == "" {
		ledgerGenID = actResult.ActivatedGenerationID
	}
	if ledgerErr := AppendLedgerEntry(req.WorktreeStateDir, req.Host, LedgerEntry{
		Host:         req.Host,
		GenerationID: ledgerGenID,
		Kind:         "verification-failed",
		RecordedAt:   req.Now.UTC().Format(time.RFC3339),
		Detail:       verResult.Detail,
	}); ledgerErr != nil {
		return result, fmt.Errorf("runtime: ActivateAndVerify: %s: post-activation verification failed (%s) and recording that failure to the Ledger also failed: %w", req.Host, verResult.Detail, ledgerErr)
	}

	detection, ok := detectionForHost(req.Fresh.Hosts, req.Host)
	if !ok {
		return result, fmt.Errorf("runtime: ActivateAndVerify: %s: post-activation verification failed (%s) and automated rollback cannot proceed: Fresh.Hosts has no entry for %q", req.Host, verResult.Detail, req.Host)
	}

	// Event 2: Rollback appends its own "rolledback" entry on success.
	// Rollback takes its own activation lock (rollback.go) -- safe to call
	// here because Activate's defer already released its own lock before
	// returning above, so this is a fresh, independent acquisition, not a
	// re-entrant one.
	rbResult, rbErr := Rollback(req.WorktreeStateDir, generationsRoot, req.Host, detection, req.Now)
	if rbErr != nil {
		return result, fmt.Errorf("runtime: ActivateAndVerify: %s: post-activation verification failed (%s) and automated rollback also failed: %w", req.Host, verResult.Detail, rbErr)
	}
	result.RolledBack = true
	result.Rollback = &rbResult
	return result, nil
}
