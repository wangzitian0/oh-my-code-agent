package effective

import (
	"testing"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
	"github.com/wangzitian0/oh-my-code-agent/internal/resolve"
)

func TestGraphs_Query_AllThreePlanes(t *testing.T) {
	observation := obs("instruction", "home/CLAUDE.md", "user", "home", nil)
	observation.Spec.Disposition = domain.DispositionActive

	effectiveGraph, err := ComputeEffectiveGraph("claude-code", "2.1.211", []domain.Observation{observation}, domain.HostKnowledge{}, Options{}, nil)
	if err != nil {
		t.Fatalf("ComputeEffectiveGraph: %v", err)
	}

	desired := resolve.ResolvedState{
		Host: "claude-code",
		Assets: []resolve.ResolvedAsset{
			{Kind: resolve.KindSkill, ID: "deploy", Active: true, Reason: "REQUIRED by profile policy"},
		},
	}

	g := Graphs{
		Host:      "claude-code",
		Observed:  ObservedGraph{Observations: []domain.Observation{observation}},
		Effective: effectiveGraph,
		Desired:   DesiredGraph{ResolvedState: desired},
	}

	t.Run("observed", func(t *testing.T) {
		res, ok := g.Query(PlaneObserved, "instruction", "home/CLAUDE.md")
		if !ok {
			t.Fatal("expected an Observed result")
		}
		if !res.Active {
			t.Error("Active = false, want true (DispositionActive)")
		}
	})

	t.Run("effective", func(t *testing.T) {
		res, ok := g.Query(PlaneEffective, "instruction", "home|home/CLAUDE.md")
		if !ok {
			t.Fatal("expected an Effective result")
		}
		if res.Provenance.SelectedSource != "home/CLAUDE.md" {
			t.Errorf("SelectedSource = %q", res.Provenance.SelectedSource)
		}
	})

	t.Run("desired", func(t *testing.T) {
		res, ok := g.Query(PlaneDesired, "skill", "deploy")
		if !ok {
			t.Fatal("expected a Desired result")
		}
		if !res.Active {
			t.Error("Active = false, want true")
		}
	})

	t.Run("missing", func(t *testing.T) {
		if _, ok := g.Query(PlaneEffective, "instruction", "does-not-exist"); ok {
			t.Error("expected no result for a missing logical ID")
		}
	})

	t.Run("unknown plane", func(t *testing.T) {
		if _, ok := g.Query(Plane("NOT_A_PLANE"), "instruction", "x"); ok {
			t.Error("expected no result for an unknown plane")
		}
	})
}

func TestObservedGraph_ByConcept(t *testing.T) {
	og := ObservedGraph{Observations: []domain.Observation{
		obs("instruction", "a", "user", "a", nil),
		obs("skill", "b", "user", "b", nil),
	}}
	if len(og.ByConcept("instruction")) != 1 {
		t.Errorf("ByConcept(instruction) = %d, want 1", len(og.ByConcept("instruction")))
	}
}
