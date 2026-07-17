package report

import (
	"testing"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
	"github.com/wangzitian0/oh-my-code-agent/internal/effective"
	"github.com/wangzitian0/oh-my-code-agent/internal/resolve"
)

func TestParsePlane(t *testing.T) {
	cases := map[string]Plane{
		"native": PlaneNative, "NATIVE": PlaneNative,
		"observed":  PlaneObserved,
		"desired":   PlaneDesired,
		"effective": PlaneEffective, "host_effective": PlaneEffective, "HOST-EFFECTIVE": PlaneEffective,
		"current": PlaneCurrent,
		"pending": PlanePending,
	}
	for in, want := range cases {
		got, err := ParsePlane(in)
		if err != nil {
			t.Errorf("ParsePlane(%q): %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("ParsePlane(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParsePlane_Unknown(t *testing.T) {
	if _, err := ParsePlane("bogus"); err == nil {
		t.Error("expected an error for an unknown plane name")
	}
}

func comparePlanesTestArtifact() Artifact {
	return Artifact{
		Debug: map[string]HostDebug{
			"codex": {
				Candidates: []effective.Candidate{
					{Concept: "mcp_server", Ref: "codex-home/config.toml#mcp_servers.solo", Disposition: domain.DispositionActive},
					{Concept: "mcp_server", Ref: "codex-home/config.toml#mcp_servers.gone", Disposition: domain.DispositionExcluded},
				},
				Graph: effective.EffectiveGraph{
					Entries: []effective.EffectiveEntry{
						{Concept: "mcp_server", LogicalID: "solo", Provenance: effective.Provenance{SelectedSource: "codex-home/config.toml#mcp_servers.solo"}},
					},
				},
				Desired: resolve.ResolvedState{
					Assets: []resolve.ResolvedAsset{
						{Kind: resolve.KindMCPServer, ID: "solo", Active: true, Reason: "profile requires it"},
						{Kind: resolve.KindMCPServer, ID: "gone", Active: false, Reason: "excluded by policy"},
					},
				},
				CurrentSources: []domain.GenerationSourceEntry{
					{Concept: "mcp_server", Source: "codex-home/config.toml#mcp_servers.solo", Included: true, Reason: "active in current generation"},
				},
				PendingSources: []domain.GenerationSourceEntry{
					{Concept: "mcp_server", Source: "codex-home/config.toml#mcp_servers.solo", Included: true, Reason: "carried into pending"},
					{Concept: "mcp_server", Source: "gone", Included: false, Reason: "excluded by policy"},
				},
			},
		},
	}
}

func TestComparePlanes_UnknownHost(t *testing.T) {
	a := comparePlanesTestArtifact()
	if _, ok := ComparePlanes(a, "claude-code", PlaneNative, PlaneCurrent); ok {
		t.Error("ComparePlanes returned ok=true for a host with no Debug data")
	}
}

func TestComparePlanes_NativeVsCurrent(t *testing.T) {
	a := comparePlanesTestArtifact()
	result, ok := ComparePlanes(a, "codex", PlaneNative, PlaneCurrent)
	if !ok {
		t.Fatal("ComparePlanes: ok=false")
	}
	if result.PlaneA != PlaneNative || result.PlaneB != PlaneCurrent {
		t.Errorf("PlaneA/PlaneB = %q/%q", result.PlaneA, result.PlaneB)
	}
	// codex-home/.../solo is Active natively and Included in current -> agree.
	// codex-home/.../gone is Excluded natively (Active=false) and NOT
	// included in current (there is no "gone" row in CurrentSources at
	// all) -> both absent-from-current and native-inactive, so this
	// specific pair does not differ; the interesting row is "solo".
	var soloRow *CompareRow
	for i := range result.Rows {
		if result.Rows[i].ID == "codex-home/config.toml#mcp_servers.solo" {
			soloRow = &result.Rows[i]
		}
	}
	if soloRow == nil {
		t.Fatal("no row for the native solo candidate")
	}
	if soloRow.Differs {
		t.Errorf("solo row differs unexpectedly: %+v", soloRow)
	}
}

func TestComparePlanes_CurrentVsPending_DetectsDifference(t *testing.T) {
	a := comparePlanesTestArtifact()
	result, ok := ComparePlanes(a, "codex", PlaneCurrent, PlanePending)
	if !ok {
		t.Fatal("ComparePlanes: ok=false")
	}
	var goneRow *CompareRow
	for i := range result.Rows {
		if result.Rows[i].ID == "gone" {
			goneRow = &result.Rows[i]
		}
	}
	if goneRow == nil {
		t.Fatal("no row for 'gone' (present in pending, absent from current)")
	}
	if !goneRow.Differs {
		t.Errorf("'gone' row should differ (absent in current, present-but-inactive in pending): %+v", goneRow)
	}
	if goneRow.A != nil {
		t.Errorf("A (current) should be nil for 'gone': %+v", goneRow.A)
	}
	if goneRow.B == nil || goneRow.B.Active {
		t.Errorf("B (pending) should be present and inactive for 'gone': %+v", goneRow.B)
	}
}

func TestComparePlanes_DesiredVsEffective(t *testing.T) {
	a := comparePlanesTestArtifact()
	result, ok := ComparePlanes(a, "codex", PlaneDesired, PlaneEffective)
	if !ok {
		t.Fatal("ComparePlanes: ok=false")
	}
	if len(result.Rows) == 0 {
		t.Fatal("no rows")
	}
}
