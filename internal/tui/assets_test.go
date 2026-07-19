package tui

import (
	"testing"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
)

// TestAssetsView_MatchesGolden mirrors overview_test.go's
// TestOverviewView_MatchesGolden for the Assets view.
func TestAssetsView_MatchesGolden(t *testing.T) {
	a := loadFixtureArtifact(t)
	m := NewModel(a).SetActive(ViewAssets)
	compareGolden(t, "assets.golden", m.View())
}

func TestRenderAssets_NoHostDebug(t *testing.T) {
	out := RenderAssets(emptyArtifactForTest())
	if !contains(out, "No host debug data") {
		t.Errorf("expected a 'no host debug data' line for an empty Artifact, got:\n%s", out)
	}
}

// TestBucketFor_CoversEveryDisposition proves bucketFor (assets.go) is
// total over every domain.SourceDisposition value reporting.md §3 names,
// not just the four this view happens to bucket by name — a disposition
// this switch has never seen must still land in bucketUnknown rather than
// falling through to a zero-valued bucketActive by accident (Go's default
// zero value for assetBucket IS bucketActive, since it's iota 0 — the
// exact silent-miscategorization bug a change to disposition.go's enum
// could introduce here without this test).
func TestBucketFor_CoversEveryDisposition(t *testing.T) {
	cases := []struct {
		d    domain.SourceDisposition
		want assetBucket
	}{
		{domain.DispositionActive, bucketActive},
		{domain.DispositionAvailable, bucketAvailable},
		{domain.DispositionExcluded, bucketExcluded},
		{domain.DispositionDenied, bucketExcluded},
		{domain.DispositionShadowed, bucketExcluded},
		{domain.DispositionUnknown, bucketUnknown},
		{domain.DispositionDiscovered, bucketUnknown},
		{domain.DispositionImported, bucketUnknown},
		{domain.DispositionOrphaned, bucketUnknown},
		{domain.DispositionOpaque, bucketUnknown},
	}
	for _, c := range cases {
		if got := bucketFor(c.d); got != c.want {
			t.Errorf("bucketFor(%s) = %v, want %v", c.d, got, c.want)
		}
	}
}
