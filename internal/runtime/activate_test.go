package runtime

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
)

// activateFixture is one host's compiled generation plus the CompileRequest
// that produced it, ready to be set as either "current" or "pending" and
// handed to Activate as ActivateRequest.Fresh.
type activateFixture struct {
	req       CompileRequest
	gen       domain.Generation
	outputDir string
}

// compileFixture compiles profiles into a fresh generation directory under
// worktreeStateDir/generations, mirroring EnsureGeneration's own content-
// addressed path convention (generationsRoot/<DirSafeID(id)>) so Rollback's
// parent-resolution logic (which relies on that exact convention) works
// against these fixtures too.
func compileFixture(t *testing.T, worktreeStateDir string, profiles []domain.Profile, parent *string, now time.Time) activateFixture {
	t.Helper()
	req := buildSimpleCompileRequest(t, "# instructions\n", profiles, domain.Activation{}, nil, now)
	req.Parent = parent
	genID, err := CompileGenerationID(req)
	if err != nil {
		t.Fatalf("CompileGenerationID: %v", err)
	}
	outputDir := filepath.Join(worktreeStateDir, "generations", DirSafeID(genID))
	gen, err := Compile(req, outputDir)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	restoreWritable(t, outputDir)
	return activateFixture{req: req, gen: gen, outputDir: outputDir}
}

// requiredSkillProfile builds a minimal, valid Profile REQUIRING one skill,
// varying the compiled Sources/sourceDigest across two otherwise-identical
// requests -- used wherever a test needs two fixtures that compile to
// genuinely different content.
func requiredSkillProfile(id, skillID string) domain.Profile {
	return domain.Profile{
		APIVersion: domain.SupportedAPIVersion,
		Kind:       "Profile",
		Metadata:   domain.Metadata{ID: id},
		Spec: domain.ProfileSpec{
			Assets: domain.ProfileAssets{
				Skills: []domain.AssetRef{{ID: skillID, Intent: domain.IntentRequired}},
			},
		},
	}
}

// TestActivate_FirstActivation_SwitchesCurrentClearsPendingAndLedgers is the
// simplest real activation: no prior "current" for this host. Activate must
// switch current to the pending generation, clear the pending pointer, and
// append a Ledger "activated" entry.
func TestActivate_FirstActivation_SwitchesCurrentClearsPendingAndLedgers(t *testing.T) {
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	worktreeStateDir := t.TempDir()

	fx := compileFixture(t, worktreeStateDir, []domain.Profile{requiredSkillProfile("company:example", "code-review")}, nil, now)
	if err := SetPendingGeneration(worktreeStateDir, "codex", fx.outputDir, fx.gen, fx.req.Hosts[0].Detection, now); err != nil {
		t.Fatalf("SetPendingGeneration: %v", err)
	}

	result, err := Activate(ActivateRequest{WorktreeStateDir: worktreeStateDir, Host: "codex", Fresh: fx.req, Now: now})
	if err != nil {
		t.Fatalf("Activate: %v", err)
	}
	if result.PreviousGenerationID != "" {
		t.Errorf("PreviousGenerationID = %q, want empty (no prior current)", result.PreviousGenerationID)
	}
	if result.ActivatedGenerationID != fx.gen.Metadata.ID {
		t.Errorf("ActivatedGenerationID = %q, want %q", result.ActivatedGenerationID, fx.gen.Metadata.ID)
	}

	gotCurrent, err := CurrentGenerationDir(worktreeStateDir, "codex")
	if err != nil {
		t.Fatalf("CurrentGenerationDir: %v", err)
	}
	if gotCurrent != fx.outputDir {
		t.Errorf("CurrentGenerationDir = %q, want %q", gotCurrent, fx.outputDir)
	}

	if _, err := PendingGenerationDir(worktreeStateDir, "codex"); !os.IsNotExist(err) {
		t.Errorf("PendingGenerationDir after activation: want os.IsNotExist, got %v", err)
	}

	entries, err := ReadLedger(worktreeStateDir, "codex")
	if err != nil {
		t.Fatalf("ReadLedger: %v", err)
	}
	found := false
	for _, e := range entries {
		if e.Kind == "activated" && e.GenerationID == fx.gen.Metadata.ID {
			found = true
		}
	}
	if !found {
		t.Errorf("ledger has no 'activated' entry for %s: %+v", fx.gen.Metadata.ID, entries)
	}
}

// TestActivate_SecondActivation_RecordsPreviousGeneration proves a second
// activation (a real current already present) reports PreviousGenerationID
// correctly and switches current to the new pending generation.
func TestActivate_SecondActivation_RecordsPreviousGeneration(t *testing.T) {
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	worktreeStateDir := t.TempDir()

	old := compileFixture(t, worktreeStateDir, []domain.Profile{requiredSkillProfile("company:example", "code-review")}, nil, now)
	if err := SetCurrentGeneration(worktreeStateDir, "codex", old.outputDir, old.gen, old.req.Hosts[0].Detection, now); err != nil {
		t.Fatalf("SetCurrentGeneration: %v", err)
	}

	parent := old.gen.Metadata.ID
	next := compileFixture(t, worktreeStateDir, []domain.Profile{requiredSkillProfile("company:example", "deep-refactor")}, &parent, now.Add(time.Minute))
	if next.gen.Metadata.ID == old.gen.Metadata.ID {
		t.Fatalf("fixture setup did not actually vary content between old and next generations")
	}
	if err := SetPendingGeneration(worktreeStateDir, "codex", next.outputDir, next.gen, next.req.Hosts[0].Detection, now.Add(time.Minute)); err != nil {
		t.Fatalf("SetPendingGeneration: %v", err)
	}

	result, err := Activate(ActivateRequest{WorktreeStateDir: worktreeStateDir, Host: "codex", Fresh: next.req, Now: now.Add(2 * time.Minute)})
	if err != nil {
		t.Fatalf("Activate: %v", err)
	}
	if result.PreviousGenerationID != old.gen.Metadata.ID {
		t.Errorf("PreviousGenerationID = %q, want %q", result.PreviousGenerationID, old.gen.Metadata.ID)
	}
	if result.ActivatedGenerationID != next.gen.Metadata.ID {
		t.Errorf("ActivatedGenerationID = %q, want %q", result.ActivatedGenerationID, next.gen.Metadata.ID)
	}

	gotCurrent, err := CurrentGenerationDir(worktreeStateDir, "codex")
	if err != nil {
		t.Fatalf("CurrentGenerationDir: %v", err)
	}
	if gotCurrent != next.outputDir {
		t.Errorf("CurrentGenerationDir = %q, want the new pending generation's dir %q", gotCurrent, next.outputDir)
	}
}

// TestActivate_NoPendingGeneration_ReturnsError proves Activate refuses,
// rather than guessing, when there is nothing pending for this host.
func TestActivate_NoPendingGeneration_ReturnsError(t *testing.T) {
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	worktreeStateDir := t.TempDir()
	req := buildSimpleCompileRequest(t, "# instructions\n", nil, domain.Activation{}, nil, now)

	_, err := Activate(ActivateRequest{WorktreeStateDir: worktreeStateDir, Host: "codex", Fresh: req, Now: now})
	if err == nil {
		t.Fatal("Activate with no pending generation: want error, got nil")
	}
}

// TestActivate_CAS_ConcurrentProfileChange_Invalidates is the M2 AC's own
// CAS test: pending was compiled from profilesA, but by the time Activate
// runs, the caller's freshly-composed desired state (Fresh.Profiles) has
// since changed to profilesB -- a real "concurrent manual change" (someone
// edited a Profile file between compile and activate). Activate must refuse
// with a *CASMismatchError, leave "current" untouched (still absent here --
// this is this host's first activation attempt), leave the pending pointer
// untouched (Activate never deletes/rewrites pending on a CAS failure), and
// record a "cas-rejected" Ledger entry.
func TestActivate_CAS_ConcurrentProfileChange_Invalidates(t *testing.T) {
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	worktreeStateDir := t.TempDir()

	profilesA := []domain.Profile{requiredSkillProfile("company:example", "code-review")}
	fx := compileFixture(t, worktreeStateDir, profilesA, nil, now)
	if err := SetPendingGeneration(worktreeStateDir, "codex", fx.outputDir, fx.gen, fx.req.Hosts[0].Detection, now); err != nil {
		t.Fatalf("SetPendingGeneration: %v", err)
	}

	// Simulate a concurrent manual Profile change: by the time Activate
	// runs, the real, freshly-composed desired state names a DIFFERENT
	// required skill.
	freshReq := fx.req
	freshReq.Profiles = []domain.Profile{requiredSkillProfile("company:example", "totally-different-skill")}

	_, err := Activate(ActivateRequest{WorktreeStateDir: worktreeStateDir, Host: "codex", Fresh: freshReq, Now: now.Add(time.Minute)})
	if err == nil {
		t.Fatal("Activate with a concurrently-changed profile: want a CAS error, got nil")
	}
	var casErr *CASMismatchError
	if !errors.As(err, &casErr) {
		t.Fatalf("Activate error = %v (%T), want a *CASMismatchError", err, err)
	}
	if casErr.PendingGenerationID != fx.gen.Metadata.ID {
		t.Errorf("CASMismatchError.PendingGenerationID = %q, want %q", casErr.PendingGenerationID, fx.gen.Metadata.ID)
	}
	if casErr.PendingSourceDigest == casErr.FreshSourceDigest {
		t.Error("CASMismatchError reports identical digests; fixture did not actually vary compiled content")
	}

	// current must remain untouched (still absent -- this is a first
	// activation attempt).
	if _, err := CurrentGenerationDir(worktreeStateDir, "codex"); !os.IsNotExist(err) {
		t.Errorf("CurrentGenerationDir after a rejected CAS check: want os.IsNotExist, got %v", err)
	}
	// pending must remain untouched -- Activate never deletes/rewrites it on
	// a CAS failure.
	gotPending, err := PendingGenerationDir(worktreeStateDir, "codex")
	if err != nil {
		t.Fatalf("PendingGenerationDir: %v", err)
	}
	if gotPending != fx.outputDir {
		t.Errorf("PendingGenerationDir after a rejected CAS check = %q, want unchanged %q", gotPending, fx.outputDir)
	}

	entries, err := ReadLedger(worktreeStateDir, "codex")
	if err != nil {
		t.Fatalf("ReadLedger: %v", err)
	}
	foundRejected := false
	for _, e := range entries {
		if e.Kind == "cas-rejected" && e.GenerationID == fx.gen.Metadata.ID {
			foundRejected = true
		}
		if e.Kind == "activated" {
			t.Errorf("ledger has an 'activated' entry despite a rejected CAS check: %+v", e)
		}
	}
	if !foundRejected {
		t.Errorf("ledger has no 'cas-rejected' entry: %+v", entries)
	}
}

// TestActivate_Atomic_CrashInjection_EveryStepBoundary is the M2 AC's own
// crash-injection test: "leaves either the old or the new current, never a
// mix." For every meaningful step boundary in Activate's own documented
// sequence (validate pending -> CAS check -> switch current -> append
// Ledger entry), a fault injected via ActivateRequest.OnStep -- simulating
// the process dying at exactly that point -- must leave CurrentGenerationDir
// resolving to either the pre-transaction ("old") generation directory or
// the post-transaction ("new") one, never anything else (a symlink to a
// partially-written or nonexistent directory, or an error CurrentGenerationDir
// itself never otherwise returns), and must never leave the Ledger naming an
// "activated" generation that "current" does not actually point at.
//
// This is the exhaustive, deterministic form of the crash-injection proof:
// it covers every step boundary directly rather than hoping a single
// real SIGKILL happens to land at an interesting instruction. The one
// property this does NOT itself re-prove is that a same-filesystem
// os.Rename is atomic at the OS level -- that is POSIX's own guarantee,
// already relied on unproven-by-a-real-kill-test elsewhere in this package
// (current.go's setGenerationPointer doc comment simply cites it), and this
// project's established "spawn a real subprocess when that's the honest way
// to prove a claim" convention (internal/shim's SIGINT test) is reserved for
// claims about THIS repository's own logic, not for re-verifying a
// operating-system primitive several other tests already depend on. What
// this test proves -- and what a real kill at one unpredictable point could
// not prove nearly as rigorously -- is that Activate's OWN step sequencing
// around that already-atomic primitive never produces a torn or misleading
// observable state, at every step, every time.
func TestActivate_Atomic_CrashInjection_EveryStepBoundary(t *testing.T) {
	steps := []ActivationStep{StepValidatePending, StepCASCheck, StepSwitchCurrent, StepAppendLedger}

	for _, crashAt := range steps {
		t.Run(string(crashAt), func(t *testing.T) {
			now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
			worktreeStateDir := t.TempDir()

			old := compileFixture(t, worktreeStateDir, []domain.Profile{requiredSkillProfile("company:example", "code-review")}, nil, now)
			if err := SetCurrentGeneration(worktreeStateDir, "codex", old.outputDir, old.gen, old.req.Hosts[0].Detection, now); err != nil {
				t.Fatalf("SetCurrentGeneration: %v", err)
			}
			parent := old.gen.Metadata.ID
			next := compileFixture(t, worktreeStateDir, []domain.Profile{requiredSkillProfile("company:example", "deep-refactor")}, &parent, now.Add(time.Minute))
			if err := SetPendingGeneration(worktreeStateDir, "codex", next.outputDir, next.gen, next.req.Hosts[0].Detection, now.Add(time.Minute)); err != nil {
				t.Fatalf("SetPendingGeneration: %v", err)
			}

			injected := errors.New("simulated crash")
			hook := func(step ActivationStep) error {
				if step == crashAt {
					return injected
				}
				return nil
			}

			_, err := Activate(ActivateRequest{WorktreeStateDir: worktreeStateDir, Host: "codex", Fresh: next.req, Now: now.Add(2 * time.Minute), OnStep: hook})
			if err == nil {
				t.Fatalf("Activate with a fault injected at %s: want an error, got nil", crashAt)
			}

			gotCurrent, cerr := CurrentGenerationDir(worktreeStateDir, "codex")
			if cerr != nil {
				t.Fatalf("CurrentGenerationDir after a crash at %s: want a valid resolution, got error: %v", crashAt, cerr)
			}
			if gotCurrent != old.outputDir && gotCurrent != next.outputDir {
				t.Fatalf("CurrentGenerationDir after a crash at %s = %q, want either the old (%q) or the new (%q) generation directory", crashAt, gotCurrent, old.outputDir, next.outputDir)
			}

			entries, lerr := ReadLedger(worktreeStateDir, "codex")
			if lerr != nil {
				t.Fatalf("ReadLedger: %v", lerr)
			}
			for _, e := range entries {
				if e.Kind != "activated" {
					continue
				}
				wantDir := old.outputDir
				if e.GenerationID == next.gen.Metadata.ID {
					wantDir = next.outputDir
				}
				if gotCurrent != wantDir {
					t.Errorf("crash at %s: ledger has an 'activated' entry for generation %s, but current (%s) does not point at it (%s)", crashAt, e.GenerationID, gotCurrent, wantDir)
				}
			}

			// Specifically for a crash strictly before the switch step, the
			// transaction must not have touched "current" at all.
			if crashAt == StepValidatePending || crashAt == StepCASCheck {
				if gotCurrent != old.outputDir {
					t.Errorf("crash at %s (before the switch step): current = %q, want it unchanged at the old generation %q", crashAt, gotCurrent, old.outputDir)
				}
			}
		})
	}
}
