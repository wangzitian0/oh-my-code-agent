package report

import (
	"strings"
	"testing"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
	"github.com/wangzitian0/oh-my-code-agent/internal/drift"
)

func TestComputeCardID_StableAcrossImpactChanges(t *testing.T) {
	base := drift.ActionCard{
		RootCause:      "company baseline not represented",
		Remediation:    "rebuild",
		Category:       domain.DriftEffectiveDrift,
		AdapterVersion: "1.0.0",
	}
	changed := base
	changed.Impact = drift.Impact{Projects: 8, Hosts: 5, Artifacts: 40}
	changed.Matrix = []drift.Assertion{{DriftAssertion: domain.DriftAssertion{EntityID: "x", Field: "y", Category: domain.DriftEffectiveDrift, RootCause: base.RootCause}}}

	id1, err := computeCardID(base)
	if err != nil {
		t.Fatalf("computeCardID: %v", err)
	}
	id2, err := computeCardID(changed)
	if err != nil {
		t.Fatalf("computeCardID: %v", err)
	}
	if id1 != id2 {
		t.Errorf("ID changed when only Impact/Matrix changed: %q vs %q", id1, id2)
	}
	if !strings.HasPrefix(id1, cardIDPrefix) {
		t.Errorf("ID %q does not start with %q", id1, cardIDPrefix)
	}
}

func TestComputeCardID_DiffersByGroupKey(t *testing.T) {
	a := drift.ActionCard{RootCause: "rc-a", Category: domain.DriftConfigDrift}
	b := drift.ActionCard{RootCause: "rc-b", Category: domain.DriftConfigDrift}

	idA, err := computeCardID(a)
	if err != nil {
		t.Fatalf("computeCardID: %v", err)
	}
	idB, err := computeCardID(b)
	if err != nil {
		t.Fatalf("computeCardID: %v", err)
	}
	if idA == idB {
		t.Errorf("two cards with different root causes produced the same ID %q", idA)
	}
}

func TestBuildDriftCards_AssignsUniqueIDs(t *testing.T) {
	cards := []drift.ActionCard{
		{RootCause: "rc-1", Category: domain.DriftConfigDrift},
		{RootCause: "rc-2", Category: domain.DriftConfigDrift},
	}
	out, err := buildDriftCards(cards)
	if err != nil {
		t.Fatalf("buildDriftCards: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("len(out) = %d, want 2", len(out))
	}
	if out[0].ID == out[1].ID {
		t.Errorf("both cards got the same ID %q", out[0].ID)
	}
	if out[0].RootCause != "rc-1" || out[1].RootCause != "rc-2" {
		t.Errorf("card order/content not preserved: %+v", out)
	}
}
