package tui

import "testing"

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
