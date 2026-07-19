package tui

import "testing"

// TestOverviewView_MatchesGolden constructs a Model from the committed
// fixture artifact (fixture_test.go's loadFixtureArtifact), selects the
// Overview view, and calls the bubbletea Model's own View() method — a
// pure function from model state to a rendered string (doc.go's "Library
// choice") — comparing the result against testdata/golden/overview.golden.
func TestOverviewView_MatchesGolden(t *testing.T) {
	a := loadFixtureArtifact(t)
	m := NewModel(a).SetActive(ViewOverview)
	compareGolden(t, "overview.golden", m.View())
}

// TestRenderOverview_NoHosts proves the empty-Artifact degrade path
// directly (not exercised by the real fixture, which always has hosts):
// an Artifact with no Hosts renders an honest "no host" line rather than
// panicking on an empty per-host loop or silently printing nothing.
func TestRenderOverview_NoHosts(t *testing.T) {
	out := RenderOverview(emptyArtifactForTest())
	if !contains(out, "No host was observed") {
		t.Errorf("expected a 'no host' line for an empty Artifact, got:\n%s", out)
	}
}
