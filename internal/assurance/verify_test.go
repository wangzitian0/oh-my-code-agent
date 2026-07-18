package assurance

import (
	"path/filepath"
	"testing"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
	"github.com/wangzitian0/oh-my-code-agent/internal/effective"
	"github.com/wangzitian0/oh-my-code-agent/internal/knowledge"
	"github.com/wangzitian0/oh-my-code-agent/internal/observe"
	"github.com/wangzitian0/oh-my-code-agent/internal/qualify"

	hostcontext "github.com/wangzitian0/oh-my-code-agent/internal/context"
)

// syntheticCeilings is a test-only ceiling table this file uses whenever it
// needs to prove [VerifyGraphWithCeilings]'s upgrade mechanism can produce
// E2/E3 in principle, without needing to mutate (or fork) the real,
// committed [Ceilings] table (which stays honestly capped at E1 for every
// real concept row today -- see ceiling_test.go's
// TestCeilings_ConceptRowsCapAtE1_HostRowsCapAtE3).
var syntheticCeilings = []CeilingEntry{
	{Host: "codex", Concept: "skill", Ceiling: domain.EvidenceLevelResolved, IntrospectionSurface: "test", Reason: "test", Citation: "test"},
	{Host: "codex", Concept: "instruction", Ceiling: domain.EvidenceLevelHostReported, IntrospectionSurface: "test", Reason: "test", Citation: "test"},
}

func qualifiedCapOps() domain.CapabilityOps {
	return domain.CapabilityOps{Resolve: domain.CapabilityExact}
}

func unqualifiedCapOps() domain.CapabilityOps {
	return domain.CapabilityOps{Resolve: domain.CapabilityUnknown}
}

func resolvedEntry(concept string, level domain.EvidenceLevel, guarantee domain.GuaranteeLevel, confirmed bool) effective.EffectiveEntry {
	return effective.EffectiveEntry{
		Concept:   concept,
		LogicalID: "id-1",
		Provenance: effective.Provenance{
			Program:        "prog.replace",
			Operator:       "REPLACE",
			SelectedSource: "a",
			ActiveSources:  []string{"a"},
		},
		EvidenceLevel: level,
		Guarantee:     guarantee,
		Confirmed:     confirmed,
		Reason:        "test entry",
	}
}

// trivialEntry mirrors internal/effective/merge.go's ResolveGroup trivial
// "only one source, or every source already agrees" fast path: no
// Provenance.Program/Operator is ever set on that path (merge.go's own
// doc comment), regardless of the Knowledge Pack's capability.
func trivialEntry(concept string, level domain.EvidenceLevel) effective.EffectiveEntry {
	return effective.EffectiveEntry{
		Concept:   concept,
		LogicalID: "id-1",
		Provenance: effective.Provenance{
			SelectedSource: "a",
			ActiveSources:  []string{"a"},
		},
		EvidenceLevel: level,
		Guarantee:     domain.GuaranteeObserved,
		Reason:        "only one physical source",
	}
}

// TestVerifyEntry_ClampsToCeiling proves an entry claiming more evidence
// than the committed ceiling allows is honestly capped down, never left
// standing -- this is issue #26's "missing introspection yields an honest
// ceiling, not an inferred level" acceptance criterion made concrete for a
// single entry.
func TestVerifyEntry_ClampsToCeiling(t *testing.T) {
	entry := trivialEntry("skill", domain.EvidenceLevelHostReported) // claims E3
	got := verifyEntry("claude-code", entry, unqualifiedCapOps(), Ceilings)
	if got.EvidenceLevel != domain.EvidenceLevelParsed {
		t.Errorf("EvidenceLevel = %s, want E1 (the real, committed ceiling for claude-code/skill)", got.EvidenceLevel)
	}
}

// TestVerifyEntry_UpgradesToE2_WhenQualifiedResolutionRan proves the other
// half: a real merge operator that actually ran against a qualified resolve
// capability is worth E2, exactly reporting.md §4's definition, when the
// ceiling table permits it.
func TestVerifyEntry_UpgradesToE2_WhenQualifiedResolutionRan(t *testing.T) {
	entry := resolvedEntry("skill", domain.EvidenceLevelParsed, domain.GuaranteeObserved, false)
	got := verifyEntry("codex", entry, qualifiedCapOps(), syntheticCeilings)
	if got.EvidenceLevel != domain.EvidenceLevelResolved {
		t.Errorf("EvidenceLevel = %s, want E2", got.EvidenceLevel)
	}
}

// TestVerifyEntry_NeverDowngradesAnAlreadyStrongerLevel proves the E2
// upgrade is a floor, not an overwrite: an entry whose own candidates
// already carried E3 evidence must not be pulled down to E2.
func TestVerifyEntry_NeverDowngradesAnAlreadyStrongerLevel(t *testing.T) {
	entry := resolvedEntry("instruction", domain.EvidenceLevelHostReported, domain.GuaranteeObserved, true)
	got := verifyEntry("codex", entry, qualifiedCapOps(), syntheticCeilings)
	if got.EvidenceLevel != domain.EvidenceLevelHostReported {
		t.Errorf("EvidenceLevel = %s, want E3 (already at or above E2, must not be lowered by the upgrade step)", got.EvidenceLevel)
	}
}

// TestVerifyEntry_NoUpgrade_TrivialPathNeverProvenanceQualifies proves the
// trivial "only one source / everyone agrees" resolution path
// (internal/effective/merge.go's ResolveGroup) is never mistaken for a real
// qualified resolution just because the Knowledge Pack happens to declare a
// qualified resolve capability: Provenance.Program/Operator being empty
// means nothing was actually resolved beyond parsing agreement.
func TestVerifyEntry_NoUpgrade_TrivialPathNeverProvenanceQualifies(t *testing.T) {
	entry := trivialEntry("skill", domain.EvidenceLevelParsed)
	got := verifyEntry("codex", entry, qualifiedCapOps(), syntheticCeilings)
	if got.EvidenceLevel != domain.EvidenceLevelParsed {
		t.Errorf("EvidenceLevel = %s, want E1 (trivial path never upgrades even under a qualified capability)", got.EvidenceLevel)
	}
}

// TestVerifyEntry_NoUpgrade_ProgramSetButCapabilityUnqualified is the
// regression test for internal/effective/compose.go's ComposeConcept
// subtlety documented on qualifiedResolutionRan: ComposeConcept sets
// Provenance.Program for every CONCAT_ORDERED concept (e.g. instruction, on
// both of today's real, committed Knowledge Packs) UNCONDITIONALLY,
// regardless of whether the resolve capability is actually qualified.
// Checking Provenance.Program alone would wrongly upgrade this entry to E2
// despite capabilities.instruction.resolve: UNKNOWN on every real pack --
// exactly the "inferred level" issue #26 forbids.
func TestVerifyEntry_NoUpgrade_ProgramSetButCapabilityUnqualified(t *testing.T) {
	entry := resolvedEntry("instruction", domain.EvidenceLevelParsed, domain.GuaranteeAdvisory, false)
	got := verifyEntry("codex", entry, unqualifiedCapOps(), syntheticCeilings)
	if got.EvidenceLevel != domain.EvidenceLevelParsed {
		t.Errorf("EvidenceLevel = %s, want E1 (Provenance.Program alone must never imply a qualified resolution)", got.EvidenceLevel)
	}
}

// TestVerifyEntry_NeverUpgradesGuarantee is issue #26's own acceptance
// criterion made a direct, isolated test: "Verification never upgrades
// ADVISORY behavior to enforcement." An entry that qualifies for the E2
// evidence upgrade, and whose ceiling even permits E3, must still keep
// whatever Guarantee internal/effective originally computed -- Evidence and
// Guarantee are independent dimensions (reporting.md §5), and re-deriving
// one must never touch the other.
func TestVerifyEntry_NeverUpgradesGuarantee(t *testing.T) {
	for _, guarantee := range []domain.GuaranteeLevel{
		domain.GuaranteeHard, domain.GuaranteeReconciled, domain.GuaranteeAdvisory, domain.GuaranteeObserved,
	} {
		t.Run(string(guarantee), func(t *testing.T) {
			entry := resolvedEntry("instruction", domain.EvidenceLevelParsed, guarantee, false)
			got := verifyEntry("codex", entry, qualifiedCapOps(), syntheticCeilings)
			if got.Guarantee != guarantee {
				t.Errorf("Guarantee = %s, want unchanged %s -- verification must never upgrade (or otherwise change) Guarantee", got.Guarantee, guarantee)
			}
			// Sanity: the evidence upgrade this test's Guarantee check
			// piggybacks on actually fired (E1 -> E2; the upgrade step
			// only ever proves E2, never more, regardless of how high
			// syntheticCeilings' instruction ceiling (E3) would allow), so
			// this is proving Guarantee stayed put DESPITE evidence
			// moving, not merely that nothing happened at all.
			if got.EvidenceLevel != domain.EvidenceLevelResolved {
				t.Fatalf("test setup: EvidenceLevel = %s, want E2 so this test actually exercises an evidence change alongside the Guarantee check", got.EvidenceLevel)
			}
		})
	}
}

// TestVerifyEntry_ConfirmedNeverPromotedByClamp proves Confirmed can only
// ever move true->false when a clamp lowers evidence below the E3+ bar it
// documents, never false->true: an entry whose original EvidenceLevel was
// strong enough for Confirmed=true must lose that claim if the ceiling
// clamps its verified evidence back down below E3.
func TestVerifyEntry_ConfirmedNeverPromotedByClamp(t *testing.T) {
	entry := resolvedEntry("skill", domain.EvidenceLevelHostReported, domain.GuaranteeHard, true)
	got := verifyEntry("claude-code", entry, unqualifiedCapOps(), Ceilings) // real ceiling: E1
	if got.Confirmed {
		t.Error("Confirmed = true after a clamp dropped EvidenceLevel below E3 -- Confirmed must be revoked, not left standing on evidence that no longer backs it")
	}
	if got.EvidenceLevel != domain.EvidenceLevelParsed {
		t.Fatalf("test setup: EvidenceLevel = %s, want E1", got.EvidenceLevel)
	}

	// And the upgrade path must never manufacture Confirmed=true on its
	// own: E2 is below the E3+ bar EffectiveEntry.Confirmed documents.
	upgraded := resolvedEntry("skill", domain.EvidenceLevelParsed, domain.GuaranteeObserved, false)
	got2 := verifyEntry("codex", upgraded, qualifiedCapOps(), syntheticCeilings)
	if got2.Confirmed {
		t.Error("Confirmed = true after only an E1->E2 upgrade -- E2 does not meet the E3+ bar Confirmed requires")
	}
}

func TestVerifyConflict_ClampsToCeiling(t *testing.T) {
	c := effective.Conflict{
		Concept:       "mcp_server",
		LogicalID:     "id-1",
		EvidenceLevel: domain.EvidenceLevelHostReported,
		Reason:        "test conflict",
	}
	got := verifyConflict("claude-code", c, Ceilings)
	if got.EvidenceLevel != domain.EvidenceLevelParsed {
		t.Errorf("EvidenceLevel = %s, want E1", got.EvidenceLevel)
	}
	if got.Reason != c.Reason || got.Concept != c.Concept || got.LogicalID != c.LogicalID {
		t.Error("verifyConflict changed a field other than EvidenceLevel")
	}
}

// TestClampToCeiling_UndeclaredCellDefaultsToE1 proves an (host, concept)
// pair this table has no row for degrades conservatively rather than being
// left uncapped.
func TestClampToCeiling_UndeclaredCellDefaultsToE1(t *testing.T) {
	got := clampToCeiling("codex", "not-a-real-concept", domain.EvidenceLevelExternallyProven, Ceilings)
	if got != domain.EvidenceLevelParsed {
		t.Errorf("clampToCeiling for an undeclared cell = %s, want E1", got)
	}
}

// TestVerifyGraphWithCeilings_PreservesEntryAndConflictCount proves
// VerifyGraph is a pure re-labeling pass: it never adds, drops, or
// reorders Entries/Conflicts.
func TestVerifyGraphWithCeilings_PreservesEntryAndConflictCount(t *testing.T) {
	graph := effective.EffectiveGraph{
		Host: "codex",
		Entries: []effective.EffectiveEntry{
			trivialEntry("skill", domain.EvidenceLevelParsed),
			trivialEntry("instruction", domain.EvidenceLevelDiscovered),
		},
		Conflicts: []effective.Conflict{
			{Concept: "mcp_server", LogicalID: "x", EvidenceLevel: domain.EvidenceLevelParsed, Reason: "r"},
		},
	}
	got := VerifyGraph("codex", graph, domain.HostKnowledge{})
	if len(got.Entries) != len(graph.Entries) {
		t.Errorf("len(Entries) = %d, want %d", len(got.Entries), len(graph.Entries))
	}
	if len(got.Conflicts) != len(graph.Conflicts) {
		t.Errorf("len(Conflicts) = %d, want %d", len(got.Conflicts), len(graph.Conflicts))
	}
}

// --- Real-fixture, real-Knowledge-Pack integration coverage ---

func loadRealCaseGraph(t *testing.T, host, version, fixtureRel string) (effective.EffectiveGraph, domain.HostKnowledge) {
	t.Helper()
	root := repoRoot()
	dir := filepath.Join(root, "fixtures", host, version, fixtureRel)
	c, err := qualify.LoadCase(dir)
	if err != nil {
		t.Fatalf("qualify.LoadCase(%s): %v", dir, err)
	}
	sb, err := qualify.NewSandbox(t.TempDir(), host)
	if err != nil {
		t.Fatalf("qualify.NewSandbox: %v", err)
	}
	if err := sb.PopulateFromInput(c.InputDir()); err != nil {
		t.Fatalf("PopulateFromInput: %v", err)
	}

	det := hostcontext.HostDetection{Host: host, Surface: "cli", Installed: true, Version: c.Version}
	switch host {
	case "claude-code":
		det.NativeHomes = []hostcontext.NativeHome{{Name: "CLAUDE_CONFIG_DIR", Path: sb.ClaudeConfigDir}}
	case "codex":
		det.NativeHomes = []hostcontext.NativeHome{
			{Name: "CODEX_HOME", Path: sb.CodexHome},
			{Name: "HOME/.agents/skills", Path: filepath.Join(sb.Home, ".agents", "skills")},
		}
	}
	obs, err := observe.Observe(observe.Request{Detection: det, WorktreeRoot: sb.Project})
	if err != nil {
		t.Fatalf("observe.Observe: %v", err)
	}

	repo, err := knowledge.LoadRepository(filepath.Join(root, "knowledge", "hosts"))
	if err != nil {
		t.Fatalf("knowledge.LoadRepository: %v", err)
	}
	resolution := repo.Resolve(host, "cli", c.Version)
	var hk domain.HostKnowledge
	if resolution.Qualified {
		for _, p := range repo.Packs() {
			if p.Knowledge.Metadata.ID == resolution.PackID {
				hk = p.Knowledge
			}
		}
	}

	graph, err := effective.ComputeEffectiveGraph(host, c.Version, obs, hk, effective.Options{}, nil)
	if err != nil {
		t.Fatalf("effective.ComputeEffectiveGraph: %v", err)
	}
	return graph, hk
}

// TestVerifyGraph_RealFixtures_NeverExceedTheCommittedCeiling is this PR's
// central end-to-end proof, run against real, committed fixtures and real,
// committed Knowledge Packs (not synthetic data): for every host this
// project qualifies, over every one of PR-06's three required collision
// fixtures, no verified EffectiveEntry or Conflict claims more evidence
// than [Ceilings] honestly allows -- and, since both real Knowledge Packs
// declare resolve: UNKNOWN for instruction/skill/mcp_server today, every
// one of them is actually AT the E1 ceiling, not merely under it, grounding
// "an honest ceiling, not an inferred level" against real data end to end.
func TestVerifyGraph_RealFixtures_NeverExceedTheCommittedCeiling(t *testing.T) {
	cases := []struct{ host, version, fixture string }{
		{"codex", "0.144.5", "instructions-collision"},
		{"codex", "0.144.5", "skill-collision"},
		{"codex", "0.144.5", "mcp-merge"},
		{"claude-code", "2.1.211", "instructions-collision"},
		{"claude-code", "2.1.211", "skill-collision"},
		{"claude-code", "2.1.211", "mcp-merge"},
	}

	for _, tc := range cases {
		t.Run(tc.host+"/"+tc.fixture, func(t *testing.T) {
			graph, hk := loadRealCaseGraph(t, tc.host, tc.version, tc.fixture)
			verified := VerifyGraph(tc.host, graph, hk)

			checked := 0
			for _, e := range verified.Entries {
				ceiling, ok := CeilingFor(Ceilings, tc.host, e.Concept)
				if !ok {
					t.Fatalf("no committed ceiling for (%s, %s)", tc.host, e.Concept)
				}
				if e.EvidenceLevel.Rank() > ceiling.Rank() {
					t.Errorf("entry %s/%s: EvidenceLevel %s exceeds committed ceiling %s", e.Concept, e.LogicalID, e.EvidenceLevel, ceiling)
				}
				if e.EvidenceLevel != domain.EvidenceLevelParsed && e.EvidenceLevel != domain.EvidenceLevelDiscovered {
					t.Errorf("entry %s/%s: EvidenceLevel %s, want E0/E1 (both real Knowledge Packs declare resolve: UNKNOWN today)", e.Concept, e.LogicalID, e.EvidenceLevel)
				}
				checked++
			}
			for _, c := range verified.Conflicts {
				ceiling, ok := CeilingFor(Ceilings, tc.host, c.Concept)
				if !ok {
					t.Fatalf("no committed ceiling for (%s, %s)", tc.host, c.Concept)
				}
				if c.EvidenceLevel.Rank() > ceiling.Rank() {
					t.Errorf("conflict %s/%s: EvidenceLevel %s exceeds committed ceiling %s", c.Concept, c.LogicalID, c.EvidenceLevel, ceiling)
				}
				checked++
			}
			if checked == 0 {
				t.Fatal("no entries or conflicts were produced for this fixture -- test is not exercising anything")
			}
		})
	}
}
