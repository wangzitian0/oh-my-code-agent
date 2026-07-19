package tui

import (
	"strings"
	"testing"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
)

// TestGenerationsView_MatchesGolden mirrors overview_test.go's
// TestOverviewView_MatchesGolden for the Generations view. The fixture
// artifact's host has both a real Current and a real Pending generation
// (fixture_test.go bootstraps both, deliberately from different
// Observation subsets), so this golden file exercises the populated path
// for both pointers, not just the "none yet" branch.
func TestGenerationsView_MatchesGolden(t *testing.T) {
	a := loadFixtureArtifact(t)
	m := NewModel(a).SetActive(ViewGenerations)
	compareGolden(t, "generations.golden", m.View())
}

func TestRenderGenerations_NoHostDebug(t *testing.T) {
	out := RenderGenerations(emptyArtifactForTest())
	if !contains(out, "No host debug data") {
		t.Errorf("expected a 'no host debug data' line for an empty Artifact, got:\n%s", out)
	}
}

// TestRenderGenerationPointer_TotalOrderForTiedEntries is a regression test
// (Copilot review finding on this PR): renderGenerationPointer's sort
// previously ordered only by (Included, Concept), which is not a total
// order -- two entries sharing both (a real, common shape: e.g. two
// excluded skills) tied, and sort.Slice makes no guarantee about tie order,
// risking golden-file churn across Go versions/runs. Two entries here tie
// on Included and Concept but differ in Reason (the tiebreaker chain's
// final, and only displayed, field) -- proving the chain actually reaches
// far enough to produce a deterministic order, not just leaving it to
// chance.
func TestRenderGenerationPointer_TotalOrderForTiedEntries(t *testing.T) {
	sources := []domain.GenerationSourceEntry{
		{Concept: "skill", Included: false, Scope: "user", Host: "codex", Source: "same", Reason: "zulu: excluded by policy"},
		{Concept: "skill", Included: false, Scope: "user", Host: "codex", Source: "same", Reason: "alpha: excluded by policy"},
	}
	var b strings.Builder
	renderGenerationPointer(&b, "Current", "gen-1", sources)
	out := b.String()

	alphaIdx := strings.Index(out, "alpha: excluded by policy")
	zuluIdx := strings.Index(out, "zulu: excluded by policy")
	if alphaIdx == -1 || zuluIdx == -1 {
		t.Fatalf("expected both tied entries' Reason text in output, got:\n%s", out)
	}
	if alphaIdx > zuluIdx {
		t.Errorf("tied entries (same Included+Concept) are not in deterministic Reason order: got %q before %q, want alpha before zulu:\n%s", "zulu", "alpha", out)
	}
}
