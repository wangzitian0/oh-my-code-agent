package runtime

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/wangzitian0/oh-my-code-agent/internal/observe"
)

// buildBisectFixtureRequest compiles a CompileRequest for a codex fixture
// tree with three distinct candidate Skill sources plus the always-present
// repository Instruction -- enough candidates (>= 4 observations) to
// exercise Bisect's "one at a time" sequencing meaningfully.
func buildBisectFixtureRequest(t *testing.T, now time.Time) CompileRequest {
	t.Helper()
	tr := newCodexFixtureTree(t)
	mustWriteFile(t, filepath.Join(tr.WorktreeRoot, "AGENTS.md"), "# instructions\n")
	mustWriteFile(t, filepath.Join(tr.CodexHome, "skills", "alpha", "SKILL.md"), "# alpha\n")
	mustWriteFile(t, filepath.Join(tr.CodexHome, "skills", "bravo", "SKILL.md"), "# bravo\n")
	mustWriteFile(t, filepath.Join(tr.CodexHome, "skills", "charlie", "SKILL.md"), "# charlie\n")

	obs, err := observe.Observe(tr.request("0.144.5"))
	if err != nil {
		t.Fatalf("observe.Observe: %v", err)
	}
	if len(obs) < 4 {
		t.Fatalf("fixture only produced %d observation(s), want at least 4 (1 instruction + 3 skills)", len(obs))
	}

	return CompileRequest{
		Worktree: tr.worktree(t),
		Hosts: []HostCompileInput{
			{Detection: tr.detection("0.144.5"), Observations: obs},
		},
		Now: now,
	}
}

// sortedCandidateIDs mirrors Bisect's own candidate ordering exactly, so
// tests can independently predict CandidateID's expected sequence.
func sortedCandidateIDs(req CompileRequest) []string {
	ids := make([]string, 0, len(req.Hosts[0].Observations))
	for _, o := range req.Hosts[0].Observations {
		ids = append(ids, o.Metadata.ID)
	}
	sort.Strings(ids)
	return ids
}

// TestBisect_DryRun_NeverCompilesOrActivatesAnything is this PR's own
// mandatory safety AC (round-3 pre-dispatch audit): "omca bisect ships with
// a mandatory --dry-run mode that never compiles or activates anything --
// only reports which disposable generations it would build and in what
// order." It plans a bisect sequence for a GenerationsRoot that does not
// exist yet, and proves that path is still never created.
func TestBisect_DryRun_NeverCompilesOrActivatesAnything(t *testing.T) {
	now := time.Date(2026, 7, 18, 9, 0, 0, 0, time.UTC)
	req := buildBisectFixtureRequest(t, now)
	wantCandidates := sortedCandidateIDs(req)

	generationsRoot := filepath.Join(t.TempDir(), "would-never-exist", "generations")

	plan, err := Bisect(BisectRequest{Compile: req, GenerationsRoot: generationsRoot, DryRun: true})
	if err != nil {
		t.Fatalf("Bisect (dry run): %v", err)
	}
	if !plan.DryRun {
		t.Error("plan.DryRun = false, want true")
	}
	if plan.Host != "codex" {
		t.Errorf("plan.Host = %q, want %q", plan.Host, "codex")
	}
	if len(plan.Steps) != len(wantCandidates) {
		t.Fatalf("len(plan.Steps) = %d, want %d", len(plan.Steps), len(wantCandidates))
	}
	for i, step := range plan.Steps {
		if step.Index != i+1 {
			t.Errorf("Steps[%d].Index = %d, want %d", i, step.Index, i+1)
		}
		if step.CandidateID != wantCandidates[i] {
			t.Errorf("Steps[%d].CandidateID = %q, want %q", i, step.CandidateID, wantCandidates[i])
		}
		if step.GenerationID == "" {
			t.Errorf("Steps[%d].GenerationID is empty even for a dry run (should still be computable without compiling)", i)
		}
		if step.Compiled {
			t.Errorf("Steps[%d].Compiled = true on a DryRun plan, want false", i)
		}
		if step.OutputDir != "" {
			t.Errorf("Steps[%d].OutputDir = %q on a DryRun plan, want empty", i, step.OutputDir)
		}
	}

	if _, err := os.Stat(generationsRoot); !os.IsNotExist(err) {
		t.Errorf("GenerationsRoot %s exists after a DryRun Bisect call (want: never created); stat err = %v", generationsRoot, err)
	}
	if _, err := os.Stat(filepath.Dir(generationsRoot)); !os.IsNotExist(err) {
		t.Errorf("GenerationsRoot's parent exists after a DryRun Bisect call (want: nothing under it ever created); stat err = %v", err)
	}
}

// TestBisect_Real_BuildsSequentialDisposableGenerationsNeverActivated proves
// the real (non-dry-run) path's own two headline properties: it actually
// compiles one generation per candidate, importing candidates strictly one
// at a time in stable order, and it NEVER activates any of them -- no
// "current"/"pending" pointer for the host is ever written, and nothing is
// ever ledgered, matching "never activating any of them as current."
func TestBisect_Real_BuildsSequentialDisposableGenerationsNeverActivated(t *testing.T) {
	now := time.Date(2026, 7, 18, 9, 0, 0, 0, time.UTC)
	req := buildBisectFixtureRequest(t, now)
	wantCandidates := sortedCandidateIDs(req)

	worktreeStateDir := t.TempDir()
	generationsRoot := filepath.Join(worktreeStateDir, "generations")

	plan, err := Bisect(BisectRequest{Compile: req, GenerationsRoot: generationsRoot, DryRun: false})
	if err != nil {
		t.Fatalf("Bisect: %v", err)
	}
	restoreWritable(t, generationsRoot)

	if plan.DryRun {
		t.Error("plan.DryRun = true on a real Bisect call, want false")
	}
	if len(plan.Steps) != len(wantCandidates) {
		t.Fatalf("len(plan.Steps) = %d, want %d", len(plan.Steps), len(wantCandidates))
	}

	seenGenIDs := map[string]bool{}
	for i, step := range plan.Steps {
		if step.CandidateID != wantCandidates[i] {
			t.Errorf("Steps[%d].CandidateID = %q, want %q", i, step.CandidateID, wantCandidates[i])
		}
		if !step.Compiled {
			t.Errorf("Steps[%d].Compiled = false on a real Bisect call, want true", i)
		}
		if step.OutputDir == "" {
			t.Errorf("Steps[%d].OutputDir is empty on a real Bisect call", i)
		}
		gen, err := ReadGenerationManifest(step.OutputDir)
		if err != nil {
			t.Fatalf("Steps[%d]: ReadGenerationManifest(%s): %v", i, step.OutputDir, err)
		}
		if gen.Metadata.ID != step.GenerationID {
			t.Errorf("Steps[%d]: compiled generation ID %q does not match planned ID %q", i, gen.Metadata.ID, step.GenerationID)
		}
		if seenGenIDs[gen.Metadata.ID] {
			t.Errorf("Steps[%d]: generation ID %q repeats an earlier step's ID -- each step must import strictly more candidates than the last", i, gen.Metadata.ID)
		}
		seenGenIDs[gen.Metadata.ID] = true
		if gen.Metadata.Parent != nil {
			t.Errorf("Steps[%d]: a disposable bisect generation recorded a Parent (%q) -- bisect generations must never claim a place in a real activation lineage", i, *gen.Metadata.Parent)
		}
	}

	// Never activated: no current/pending pointer, no Ledger entry.
	if _, err := CurrentGenerationDir(worktreeStateDir, "codex"); !os.IsNotExist(err) {
		t.Errorf("CurrentGenerationDir after Bisect: want os.IsNotExist, got %v", err)
	}
	if _, err := PendingGenerationDir(worktreeStateDir, "codex"); !os.IsNotExist(err) {
		t.Errorf("PendingGenerationDir after Bisect: want os.IsNotExist, got %v", err)
	}
	entries, err := ReadLedger(worktreeStateDir, "codex")
	if err != nil {
		t.Fatalf("ReadLedger: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("ReadLedger after Bisect = %+v, want no entries (bisect never ledgers a transition)", entries)
	}
}

// TestBisect_MultiHostRequest_ReturnsError proves Bisect refuses a
// multi-host CompileRequest rather than silently bisecting the wrong host
// or an arbitrary one -- Bisect is single-host by contract, matching
// `omca bisect <host>`'s own grammar.
func TestBisect_MultiHostRequest_ReturnsError(t *testing.T) {
	now := time.Date(2026, 7, 18, 9, 0, 0, 0, time.UTC)
	req := buildBisectFixtureRequest(t, now)
	req.Hosts = append(req.Hosts, req.Hosts[0])

	if _, err := Bisect(BisectRequest{Compile: req, DryRun: true}); err == nil {
		t.Fatal("Bisect with a two-host CompileRequest: want error, got nil")
	}
}

// TestBisect_ZeroCandidates_ReturnsEmptyPlan proves an installed host with
// nothing observed at all bisects to an empty, non-error plan rather than a
// confusing failure.
func TestBisect_ZeroCandidates_ReturnsEmptyPlan(t *testing.T) {
	now := time.Date(2026, 7, 18, 9, 0, 0, 0, time.UTC)
	tr := newCodexFixtureTree(t)
	req := CompileRequest{
		Worktree: tr.worktree(t),
		Hosts:    []HostCompileInput{{Detection: tr.detection("0.144.5"), Observations: nil}},
		Now:      now,
	}

	plan, err := Bisect(BisectRequest{Compile: req, DryRun: true})
	if err != nil {
		t.Fatalf("Bisect with zero candidates: %v", err)
	}
	if len(plan.Steps) != 0 {
		t.Errorf("len(plan.Steps) = %d, want 0", len(plan.Steps))
	}
}

// TestBisect_IdempotentAcrossTwoRealCalls proves calling Bisect twice with
// the identical request (the same scenario a user re-running `omca bisect`
// to double-check a result would hit) never fails trying to overwrite an
// already-compiled, already-read-only generation directory -- the same
// idempotency guarantee EnsureGeneration already gives every other Compile
// caller.
func TestBisect_IdempotentAcrossTwoRealCalls(t *testing.T) {
	now := time.Date(2026, 7, 18, 9, 0, 0, 0, time.UTC)
	req := buildBisectFixtureRequest(t, now)

	worktreeStateDir := t.TempDir()
	generationsRoot := filepath.Join(worktreeStateDir, "generations")

	plan1, err := Bisect(BisectRequest{Compile: req, GenerationsRoot: generationsRoot, DryRun: false})
	if err != nil {
		t.Fatalf("Bisect (1st): %v", err)
	}
	restoreWritable(t, generationsRoot)

	plan2, err := Bisect(BisectRequest{Compile: req, GenerationsRoot: generationsRoot, DryRun: false})
	if err != nil {
		t.Fatalf("Bisect (2nd, same request): %v", err)
	}

	if len(plan1.Steps) != len(plan2.Steps) {
		t.Fatalf("plan1 has %d steps, plan2 has %d", len(plan1.Steps), len(plan2.Steps))
	}
	for i := range plan1.Steps {
		if plan1.Steps[i].GenerationID != plan2.Steps[i].GenerationID {
			t.Errorf("Steps[%d]: GenerationID differs across two identical Bisect calls: %q != %q", i, plan1.Steps[i].GenerationID, plan2.Steps[i].GenerationID)
		}
	}
}
