package tui

import "testing"

// TestDriftView_MatchesGolden mirrors overview_test.go's
// TestOverviewView_MatchesGolden for the Drift view. The fixture artifact
// is built from the mcp-merge corpus's real multi-source "shared-tools"
// collision (fixture_test.go's doc comment), so this golden file has at
// least one real SOURCE_DRIFT action card, not an empty "no drift" line —
// proving the Drift view actually renders root-cause/remediation/impact
// content, not just its own empty-state branch.
func TestDriftView_MatchesGolden(t *testing.T) {
	a := loadFixtureArtifact(t)
	m := NewModel(a).SetActive(ViewDrift)
	compareGolden(t, "drift.golden", m.View())
}

func TestRenderDrift_NoActionCards(t *testing.T) {
	out := RenderDrift(emptyArtifactForTest())
	if !contains(out, "No drift") {
		t.Errorf("expected a 'no drift' line for an empty Artifact, got:\n%s", out)
	}
}
