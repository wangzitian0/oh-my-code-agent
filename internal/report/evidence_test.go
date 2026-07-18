package report

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/wangzitian0/oh-my-code-agent/internal/assurance"
	hostcontext "github.com/wangzitian0/oh-my-code-agent/internal/context"
	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
	"github.com/wangzitian0/oh-my-code-agent/internal/knowledge"
	"github.com/wangzitian0/oh-my-code-agent/internal/observe"
	"github.com/wangzitian0/oh-my-code-agent/internal/qualify"
)

// buildClaudeHostInput is buildCodexHostInput's claude-code counterpart,
// mirroring internal/effective/fixture_test.go's buildObserveRequest for
// this host.
func buildClaudeHostInput(t *testing.T, sb *qualify.Sandbox, c *qualify.Case) HostInput {
	t.Helper()
	detection := hostcontext.HostDetection{
		Host:       c.Host,
		Surface:    "cli",
		Installed:  true,
		Version:    c.Version,
		BinaryPath: "/fake/bin/claude", // HostVersionEvidence only needs a non-empty label for its Method string.
		NativeHomes: []hostcontext.NativeHome{
			{Name: "CLAUDE_CONFIG_DIR", Path: sb.ClaudeConfigDir},
		},
	}
	obs, err := observe.Observe(observe.Request{Detection: detection, WorktreeRoot: sb.Project})
	if err != nil {
		t.Fatalf("observe.Observe: %v", err)
	}
	return HostInput{Detection: detection, Observations: obs}
}

// TestBuild_EveryEffectiveEntryCarriesEvidenceLevel_NeverExceedsCeiling is
// issue #26's acceptance criteria 1 and 3 proved end to end through the real
// report.Build pipeline (not internal/assurance in isolation): every
// EffectiveEntry and Conflict in every host's Debug graph carries a valid
// EvidenceLevel that never exceeds internal/assurance's committed,
// per-host-per-concept ceiling, and every host's Debug.Evidence carries a
// matching, individually valid domain.Evidence record for each one, plus an
// E3 HostConceptClaim record for the host binary's own confirmed version.
func TestBuild_EveryEffectiveEntryCarriesEvidenceLevel_NeverExceedsCeiling(t *testing.T) {
	root := repoRootForTest()
	repo, err := knowledge.LoadRepository(filepath.Join(root, "knowledge", "hosts"))
	if err != nil {
		t.Fatalf("LoadRepository: %v", err)
	}

	codexCase, codexSb := loadCaseSandbox(t, root, filepath.Join("fixtures", "codex", "0.144.5", "mcp-merge"))
	claudeCase, claudeSb := loadCaseSandbox(t, root, filepath.Join("fixtures", "claude-code", "2.1.211", "skill-collision"))

	artifact, err := Build(BuildRequest{
		Worktree: hostcontext.Worktree{ID: "worktree:sha256:evidence-test", Root: codexSb.Project},
		Hosts: []HostInput{
			buildCodexHostInput(t, codexSb, codexCase),
			buildClaudeHostInput(t, claudeSb, claudeCase),
		},
		Repository: repo,
		Now:        time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	if len(artifact.Debug) != 2 {
		t.Fatalf("len(Debug) = %d, want 2", len(artifact.Debug))
	}

	for host, hd := range artifact.Debug {
		checkedGraph := 0
		for _, e := range hd.Graph.Entries {
			checkedGraph++
			if !e.EvidenceLevel.Valid() {
				t.Errorf("%s: entry %s/%s has invalid EvidenceLevel %q", host, e.Concept, e.LogicalID, e.EvidenceLevel)
			}
			ceiling, ok := assurance.CeilingFor(assurance.Ceilings, host, e.Concept)
			if !ok {
				t.Fatalf("%s: no committed ceiling for concept %q", host, e.Concept)
			}
			if e.EvidenceLevel.Rank() > ceiling.Rank() {
				t.Errorf("%s: entry %s/%s EvidenceLevel %s exceeds committed ceiling %s", host, e.Concept, e.LogicalID, e.EvidenceLevel, ceiling)
			}
		}
		for _, c := range hd.Graph.Conflicts {
			checkedGraph++
			if !c.EvidenceLevel.Valid() {
				t.Errorf("%s: conflict %s/%s has invalid EvidenceLevel %q", host, c.Concept, c.LogicalID, c.EvidenceLevel)
			}
			ceiling, ok := assurance.CeilingFor(assurance.Ceilings, host, c.Concept)
			if !ok {
				t.Fatalf("%s: no committed ceiling for concept %q", host, c.Concept)
			}
			if c.EvidenceLevel.Rank() > ceiling.Rank() {
				t.Errorf("%s: conflict %s/%s EvidenceLevel %s exceeds committed ceiling %s", host, c.Concept, c.LogicalID, c.EvidenceLevel, ceiling)
			}
		}
		if checkedGraph == 0 {
			t.Fatalf("%s: no entries or conflicts in Debug.Graph -- test is not exercising anything", host)
		}

		if len(hd.Evidence) == 0 {
			t.Fatalf("%s: Debug.Evidence is empty", host)
		}
		gotHostVersionRecord := false
		for _, ev := range hd.Evidence {
			if err := domain.ValidateEvidence(ev); err != nil {
				t.Errorf("%s: Debug.Evidence record %+v: %v", host, ev, err)
			}
			if ev.Spec.Subject.Concept == assurance.HostConceptClaim {
				gotHostVersionRecord = true
				if ev.Spec.Level != domain.EvidenceLevelHostReported {
					t.Errorf("%s: host-version Evidence Level = %s, want E3", host, ev.Spec.Level)
				}
			}
		}
		if !gotHostVersionRecord {
			t.Errorf("%s: Debug.Evidence has no host-version (E3) record despite Detection.Installed=true and a resolved Version", host)
		}
	}
}

// TestBuild_VerificationNeverUpgradesAdvisoryGuarantee is issue #26's
// second acceptance criterion proved through the real report.Build
// pipeline: codex's instructions-collision fixture composes a
// CONCAT_ORDERED instruction entry, which internal/effective/compose.go
// always classifies GuaranteeAdvisory ("correctness depends on model or
// human compliance" -- reporting.md §5, exactly right for instruction text
// concatenation). Verification must not have silently strengthened that to
// RECONCILED/HARD while re-deriving the entry's EvidenceLevel.
func TestBuild_VerificationNeverUpgradesAdvisoryGuarantee(t *testing.T) {
	root := repoRootForTest()
	c, sb := loadCaseSandbox(t, root, filepath.Join("fixtures", "codex", "0.144.5", "instructions-collision"))
	hostInput := buildCodexHostInput(t, sb, c)

	repo, err := knowledge.LoadRepository(filepath.Join(root, "knowledge", "hosts"))
	if err != nil {
		t.Fatalf("LoadRepository: %v", err)
	}

	artifact, err := Build(BuildRequest{
		Worktree:   hostcontext.Worktree{ID: "worktree:sha256:advisory-test", Root: sb.Project},
		Hosts:      []HostInput{hostInput},
		Repository: repo,
		Now:        time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	entry, ok := artifact.Debug["codex"].Graph.Find("instruction", "instruction.composition")
	if !ok {
		t.Fatal("expected a composed instruction.composition entry in codex's Debug graph")
	}
	if entry.Guarantee != domain.GuaranteeAdvisory {
		t.Errorf("Guarantee = %s, want ADVISORY (unchanged by verification)", entry.Guarantee)
	}
}

// TestBuild_FingerprintStable_AcrossDifferentNow_DespiteEvidenceObservedAt
// is the regression test for a real bug this PR's own Evidence wiring
// introduced and then fixed: Debug[host].Evidence[i].Spec.ObservedAt is
// derived from req.Now (assurance.BuildEvidence/HostVersionEvidence), and
// Debug is part of what computeFingerprint digests, so two Build calls over
// identical logical inputs at genuinely different instants used to produce
// two different fingerprints — breaking Build's own documented
// reproducibility contract ("two Build calls over identical logical inputs
// at different instants produce different Metadata but the same
// Fingerprint"), exactly like cmd/omca's pre-existing
// TestRunReport_JSON_IsStableAndValid caught when this PR's Evidence field
// was first wired in. This test also proves the fix does not corrupt the
// returned Artifact itself: both calls' actual (non-fingerprint)
// Evidence.ObservedAt values must still reflect each call's own Now.
func TestBuild_FingerprintStable_AcrossDifferentNow_DespiteEvidenceObservedAt(t *testing.T) {
	root := repoRootForTest()
	repo, err := knowledge.LoadRepository(filepath.Join(root, "knowledge", "hosts"))
	if err != nil {
		t.Fatalf("LoadRepository: %v", err)
	}

	now1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	now2 := time.Date(2026, 6, 15, 12, 30, 0, 0, time.UTC)

	// One sandbox/HostInput reused for both Build calls: only Now differs
	// between them, so any fingerprint difference can only come from Now
	// itself (or something it feeds, like Evidence.ObservedAt) -- not from
	// two different temp-directory sandboxes producing different absolute
	// Observation paths, which would be a genuine logical-input difference,
	// not the reproducibility property this test is proving.
	c, sb := loadCaseSandbox(t, root, filepath.Join("fixtures", "codex", "0.144.5", "skill-collision"))
	hostInput := buildCodexHostInput(t, sb, c)
	worktree := hostcontext.Worktree{ID: "worktree:sha256:fp-test", Root: sb.Project}

	a1, err := Build(BuildRequest{Worktree: worktree, Hosts: []HostInput{hostInput}, Repository: repo, Now: now1})
	if err != nil {
		t.Fatalf("Build (now1): %v", err)
	}
	a2, err := Build(BuildRequest{Worktree: worktree, Hosts: []HostInput{hostInput}, Repository: repo, Now: now2})
	if err != nil {
		t.Fatalf("Build (now2): %v", err)
	}

	if a1.Report.Spec.Fingerprint != a2.Report.Spec.Fingerprint {
		t.Errorf("fingerprint differs across Now values alone: %q vs %q", a1.Report.Spec.Fingerprint, a2.Report.Spec.Fingerprint)
	}

	hd1, ok := a1.Debug["codex"]
	if !ok || len(hd1.Evidence) == 0 {
		t.Fatal("expected non-empty Debug[codex].Evidence in the first Build result")
	}
	for _, ev := range hd1.Evidence {
		wantObservedAt := now1.UTC().Format(time.RFC3339)
		if ev.Spec.ObservedAt != wantObservedAt {
			t.Errorf("computeFingerprint corrupted the returned Artifact: Evidence.ObservedAt = %q, want %q (this call's own Now)", ev.Spec.ObservedAt, wantObservedAt)
		}
	}
}
