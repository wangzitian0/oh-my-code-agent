package runtime

import (
	"fmt"
	"os"
	"time"

	hostcontext "github.com/wangzitian0/oh-my-code-agent/internal/context"
	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
)

// ActivationStep names one step boundary in the Activation transaction
// Activate runs, in the exact order docs/architecture/runtime.md §5.4
// documents ("validate pending -> ensure source digests still match ->
// ... -> atomically switch current -> ... -> append Ledger entry" -- "close
// or detach the current host session" and "launch the host"/"verify" are a
// caller's job, not this package's: this package owns only the pointer
// transaction itself, see Activate's own doc comment). ActivateRequest.OnStep,
// when set, is invoked immediately BEFORE the named step runs.
type ActivationStep string

const (
	// StepValidatePending is about to read and validate the pending
	// generation.
	StepValidatePending ActivationStep = "validate-pending"
	// StepCASCheck is about to recompute a fresh source digest and compare
	// it against the pending generation's recorded one.
	StepCASCheck ActivationStep = "cas-check"
	// StepSwitchCurrent is about to atomically repoint "current" at the
	// pending generation's directory.
	StepSwitchCurrent ActivationStep = "switch-current"
	// StepAppendLedger is about to append the "activated" Ledger entry.
	StepAppendLedger ActivationStep = "append-ledger"
)

// StepHook is a fault-injection point Activate calls immediately before
// running the named step. A non-nil return aborts the transaction at
// exactly that boundary -- activate_test.go's TestActivate_Atomic_
// CrashInjection_EveryStepBoundary uses this to simulate "the process died
// between step N and step N+1" at every meaningful boundary, exhaustively
// and deterministically. That test's own doc comment explains the deliberate
// choice not to ALSO build a real-subprocess SIGKILL test: the one OS-level
// guarantee this mechanism assumes (os.Rename is atomic on the same
// filesystem) is POSIX's own guarantee, already relied on unproven-by-a-
// real-kill-test elsewhere in this package (current.go's setGenerationPointer
// doc comment), and re-proving it here would test the operating system, not
// this code. A nil OnStep (the normal production case) never calls this at
// all.
type StepHook func(step ActivationStep) error

func runHook(hook StepHook, step ActivationStep) error {
	if hook == nil {
		return nil
	}
	return hook(step)
}

// CASMismatchError reports that Activate refused to proceed because a fresh
// recomputation of the pending generation's own desired-state inputs no
// longer produces the sourceDigest the pending manifest recorded --
// docs/product/requirements.md FR-9's "concurrent source changes invalidate
// an existing Plan through digest checks" and the M2 AC "source-digest
// compare-and-swap: a concurrent manual change invalidates pending."
//
// Activate never deletes or rewrites the pending pointer itself on this
// path: generations are immutable and content-addressed (readonly.go), and
// silently discarding a caller's pending pointer would be a surprising,
// destructive side effect for a function whose contract is "activate, or
// explain why not." The caller (a future CLI/TUI/MCP activation path) is
// expected to recompile a fresh pending generation (runtime.Compile +
// SetPendingGeneration) and retry.
type CASMismatchError struct {
	Host                string
	PendingGenerationID string
	PendingSourceDigest string
	FreshSourceDigest   string
}

func (e *CASMismatchError) Error() string {
	return fmt.Sprintf("runtime: Activate: %s: pending generation %s's source digest %s no longer matches a fresh recomputation (%s) -- the real environment changed since pending was compiled; recompile pending and try again", e.Host, e.PendingGenerationID, e.PendingSourceDigest, e.FreshSourceDigest)
}

// ActivateRequest is everything Activate needs to run one host's Activation
// transaction.
type ActivateRequest struct {
	// WorktreeStateDir is the worktree's state root (current/pending/ledger
	// all live under it), caller-supplied and never resolved internally --
	// see EnsureGeneration's identical discipline.
	WorktreeStateDir string
	// Host is the canonical host ID whose "pending" pointer is being
	// activated. Activation is per host (docs/architecture/runtime.md §5.5:
	// "restart_required is therefore per host, not per worktree" -- the same
	// per-host granularity applies to activation itself, since each host has
	// its own independent current/pending pointer pair).
	Host string
	// Fresh is a freshly re-observed, freshly re-composed CompileRequest for
	// the SAME desired state (Profiles/Activation/Exceptions) and host set
	// the pending generation was compiled from, built by the caller
	// IMMEDIATELY before calling Activate (the same "caller supplies current
	// reality, this package never reads it implicitly" discipline
	// resolve.Resolve and observe.Observe already establish). Activate uses
	// it only to recompute a fresh sourceDigest for the CAS check
	// (freshSourceDigest, compile_full.go's hostSourcesFor/aggregateSources)
	// -- it never writes anything to disk from Fresh, and Fresh.Now is what
	// answers "as of right now."
	Fresh CompileRequest
	// Now is the wall-clock time recorded on the switched CurrentRecord and
	// the appended Ledger entry. Injected, never read via time.Now()
	// internally, matching every other timestamp in this package.
	Now time.Time
	// OnStep, if non-nil, is StepHook's fault-injection point -- see its own
	// doc comment. nil in every production call site.
	OnStep StepHook
}

// ActivationResult is Activate's success value: what changed, and to what.
type ActivationResult struct {
	Host                  string
	PreviousGenerationID  string // "" if this host had no current generation before activation
	ActivatedGenerationID string
	ActivatedAt           string
}

// Activate runs one host's Activation transaction: validate pending, ensure
// its recorded source digest still matches a fresh recomputation (the CAS
// check), atomically switch "current" to the pending generation, and append
// a Ledger entry -- docs/architecture/runtime.md §5.4's step list, minus the
// two steps that are a caller's job, not this function's: "close or detach
// the current host session" (a caller must stop/signal whatever process is
// still running against the OLD current generation before calling Activate;
// this package has no process-tracking of its own -- see restart.go's
// DetectRestartRequired for how a caller learns a session needs exactly
// that) and "launch the host" / "verify" (restarting the target host binary
// and confirming its effective state are cmd/omca's and, eventually, PR-22's
// jobs respectively -- this function's contract ends at a durably switched,
// ledgered "current" pointer).
//
// # Why this is atomic without a bespoke two-phase-commit protocol
//
// The one irreversible, externally-visible state change this function makes
// is the "switch-current" step: SetCurrentGeneration's underlying
// setGenerationPointer already writes through a temp-file-plus-os.Rename
// sequence, which POSIX guarantees is atomic for a same-filesystem rename
// (current.go's own doc comment). So CurrentGenerationDir, read at any
// instant before, during, or after a call to Activate, can only ever
// resolve to the pre-transaction generation directory or the
// post-transaction one -- never a symlink pointing at a partially-written or
// nonexistent path. Every step before the switch (validate, CAS check) only
// reads state and never mutates "current"; the one step after it (append
// Ledger entry) only ever appends a new line to an append-only file and
// never touches "current" again. A crash at any point up to and including
// the switch itself therefore leaves "current" resolving to a valid
// generation; a crash strictly after the switch but before the Ledger
// append leaves "current" already pointing at the new generation with no
// "activated" entry recorded for it yet -- a benign, recoverable gap (a
// later successful Activate call, or a dedicated repair path, can always
// append the missing entry; nothing reads "the Ledger's last entry" as the
// sole source of truth for what "current" names, this package's own
// pointer-read functions do). What Activate must NOT do, and does not do,
// is ever write a "activated" Ledger entry for a generation "current" does
// not actually (by then) point at, or leave "current" pointing at a
// directory this transaction never validated -- activate_test.go's
// crash-injection test proves both directions hold at every step boundary.
func Activate(req ActivateRequest) (ActivationResult, error) {
	if req.WorktreeStateDir == "" {
		return ActivationResult{}, fmt.Errorf("runtime: Activate: WorktreeStateDir is required")
	}
	if err := domain.ValidateHostID(req.Host); err != nil {
		return ActivationResult{}, fmt.Errorf("runtime: Activate: %w", err)
	}
	if req.Now.IsZero() {
		return ActivationResult{}, fmt.Errorf("runtime: Activate: Now is required (this package never reads the clock implicitly)")
	}

	// Step 1: validate pending.
	if err := runHook(req.OnStep, StepValidatePending); err != nil {
		return ActivationResult{}, fmt.Errorf("runtime: Activate: aborted at %s: %w", StepValidatePending, err)
	}
	pendingDir, err := PendingGenerationDir(req.WorktreeStateDir, req.Host)
	if err != nil {
		return ActivationResult{}, fmt.Errorf("runtime: Activate: no pending generation for %s: %w", req.Host, err)
	}
	pendingGen, err := ReadGenerationManifest(pendingDir)
	if err != nil {
		return ActivationResult{}, fmt.Errorf("runtime: Activate: pending generation manifest for %s at %s is unreadable: %w", req.Host, pendingDir, err)
	}

	var previousGenID string
	if prevDir, prevErr := CurrentGenerationDir(req.WorktreeStateDir, req.Host); prevErr == nil {
		if prevGen, readErr := ReadGenerationManifest(prevDir); readErr == nil {
			previousGenID = prevGen.Metadata.ID
		}
		// A previous "current" pointer that exists but is unreadable is not
		// fatal here (Activate can still proceed to establish a fresh
		// current); it just means PreviousGenerationID is left empty rather
		// than fabricated.
	} else if !os.IsNotExist(prevErr) {
		return ActivationResult{}, fmt.Errorf("runtime: Activate: current-generation pointer for %s is corrupt: %w", req.Host, prevErr)
	}

	// Step 2: CAS check -- ensure source digests still match.
	if err := runHook(req.OnStep, StepCASCheck); err != nil {
		return ActivationResult{}, fmt.Errorf("runtime: Activate: aborted at %s: %w", StepCASCheck, err)
	}
	freshDigest, err := freshSourceDigest(req.Fresh)
	if err != nil {
		return ActivationResult{}, fmt.Errorf("runtime: Activate: recomputing a fresh source digest for %s: %w", req.Host, err)
	}
	if freshDigest != pendingGen.Spec.SourceDigest {
		casErr := &CASMismatchError{
			Host:                req.Host,
			PendingGenerationID: pendingGen.Metadata.ID,
			PendingSourceDigest: pendingGen.Spec.SourceDigest,
			FreshSourceDigest:   freshDigest,
		}
		// Best-effort audit trail: a rejected activation attempt is still a
		// real, worth-recording event (docs/architecture/runtime.md §12
		// "native exclusions are explained rather than hidden" -- the same
		// "explain, don't hide" stance applied to a rejected transition).
		// Its own failure must never mask the real CAS error above.
		_ = AppendLedgerEntry(req.WorktreeStateDir, req.Host, LedgerEntry{
			Host:         req.Host,
			GenerationID: pendingGen.Metadata.ID,
			Kind:         "cas-rejected",
			RecordedAt:   req.Now.UTC().Format(time.RFC3339),
			Detail:       casErr.Error(),
		})
		return ActivationResult{}, casErr
	}

	// Step 3: atomically switch current.
	if err := runHook(req.OnStep, StepSwitchCurrent); err != nil {
		return ActivationResult{}, fmt.Errorf("runtime: Activate: aborted at %s: %w", StepSwitchCurrent, err)
	}
	detection, ok := detectionForHost(req.Fresh.Hosts, req.Host)
	if !ok {
		return ActivationResult{}, fmt.Errorf("runtime: Activate: Fresh.Hosts has no entry for host %q", req.Host)
	}
	if err := SetCurrentGeneration(req.WorktreeStateDir, req.Host, pendingDir, pendingGen, detection, req.Now); err != nil {
		return ActivationResult{}, fmt.Errorf("runtime: Activate: switching current for %s: %w", req.Host, err)
	}
	// The pending pointer named a change that is now current; clearing it
	// prevents a stale pending pointer from misleadingly suggesting there is
	// still an uncompiled/unactivated change waiting (pending.go's own doc
	// comment names clearing/replacing the pending pointer as explicitly
	// "PR-15's job, not this one's"). Best-effort: a failure to clear
	// pending does not undo an already-successful switch, and a later
	// SetPendingGeneration call simply overwrites whatever is left here.
	clearPendingGeneration(req.WorktreeStateDir, req.Host)

	// Step 4: append Ledger entry.
	if err := runHook(req.OnStep, StepAppendLedger); err != nil {
		return ActivationResult{}, fmt.Errorf("runtime: Activate: aborted at %s: %w", StepAppendLedger, err)
	}
	if err := AppendLedgerEntry(req.WorktreeStateDir, req.Host, LedgerEntry{
		Host:         req.Host,
		GenerationID: pendingGen.Metadata.ID,
		Kind:         "activated",
		RecordedAt:   req.Now.UTC().Format(time.RFC3339),
		Detail:       fmt.Sprintf("switched current from %q to %q", previousGenID, pendingGen.Metadata.ID),
	}); err != nil {
		return ActivationResult{}, fmt.Errorf("runtime: Activate: appending ledger entry for %s: %w", req.Host, err)
	}

	return ActivationResult{
		Host:                  req.Host,
		PreviousGenerationID:  previousGenID,
		ActivatedGenerationID: pendingGen.Metadata.ID,
		ActivatedAt:           req.Now.UTC().Format(time.RFC3339),
	}, nil
}

// detectionForHost returns the HostCompileInput.Detection for host among
// hosts, if present.
func detectionForHost(hosts []HostCompileInput, host string) (hostcontext.HostDetection, bool) {
	for _, h := range hosts {
		if h.Detection.Host == host {
			return h.Detection, true
		}
	}
	return hostcontext.HostDetection{}, false
}

// freshSourceDigest recomputes -- WITHOUT writing anything to disk -- the
// exact same sourceDigest Compile would record for req's desired state
// (Profiles/Activation/Exceptions) and host set, evaluated against req's own
// Observations/Now (i.e. whatever the caller freshly observed/composed
// immediately before calling Activate). This is Activate's CAS check's core:
// comparing this against a pending generation's own recorded
// Spec.SourceDigest answers "would compiling this exact desired state right
// now, against the environment as it is right now, still produce the same
// content" -- docs/architecture/runtime.md §5.4's "ensure source digests
// still match" step, read literally.
//
// It reuses hostSourcesFor/aggregateSources (compile_full.go) -- the same
// functions Compile itself calls -- rather than re-deriving a parallel
// digest scheme: two different digest computations over "the same" inputs
// could drift apart from Compile's own SourceDigest for reasons that have
// nothing to do with the real environment changing, which would make the
// CAS check either spuriously fire or spuriously pass. Reusing the identical
// code path is what makes "same inputs, same digest" a structural guarantee
// instead of a maintained-by-hand invariant across two implementations.
func freshSourceDigest(req CompileRequest) (string, error) {
	if err := req.validate(); err != nil {
		return "", err
	}
	permissions := mergePermissions(req.Profiles)
	perHost := make([]hostSourceEntry, 0, len(req.Hosts))
	for _, h := range req.Hosts {
		_, sources, err := hostSourcesFor(req, h, permissions)
		if err != nil {
			return "", err
		}
		perHost = append(perHost, hostSourceEntry{Host: h.Detection.Host, Sources: sources})
	}
	_, digest, err := aggregateSources(perHost)
	if err != nil {
		return "", err
	}
	return digest, nil
}
