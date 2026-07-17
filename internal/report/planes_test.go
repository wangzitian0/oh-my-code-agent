package report

import (
	"strings"
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

// TestAddSourceRows_CapabilityGapEntry_GetsSyntheticID is Bug 2's direct
// regression test: internal/runtime/compile.go's
// claudeConfigDirExclusionGapSources() (issue #47) produces
// GenerationSourceEntry values with Source == "" -- a capability-gap
// placeholder describing a whole exclusion class, not one discovered
// physical file. Before the fix, addSourceRows fell back to id =
// s.Concept, so this rendered as a row literally keyed ("mcp_server",
// "mcp_server") or ("skill", "skill") -- indistinguishable at a glance in
// `omca compare`/`omca diff` output from a real source whose path happened
// to equal the bare concept name. A Source-less entry must never produce a
// row whose ID equals its own bare Concept string.
func TestAddSourceRows_CapabilityGapEntry_GetsSyntheticID(t *testing.T) {
	gapSources := []domain.GenerationSourceEntry{
		{Concept: "mcp_server", Scope: "user", Included: false, Reason: "capability gap", CapabilityGap: true, TrackingIssue: "https://example.com/issue/47"},
		{Concept: "skill", Scope: "user", Included: false, Reason: "capability gap", CapabilityGap: true, TrackingIssue: "https://example.com/issue/47"},
	}
	rows := map[planeKey]PlaneRow{}
	addSourceRows(rows, gapSources, nil)

	if len(rows) != 2 {
		t.Fatalf("addSourceRows produced %d rows, want 2 (one per gap entry, no collision): %+v", len(rows), rows)
	}
	for k, row := range rows {
		if row.ID == row.Concept {
			t.Errorf("capability-gap row ID equals its own bare Concept %q -- indistinguishable from a real Source in human output; row: %+v", row.Concept, row)
		}
		if k.id == k.concept {
			t.Errorf("capability-gap planeKey{concept:%q, id:%q}: id must not equal concept", k.concept, k.id)
		}
		if !strings.HasPrefix(row.ID, "capability-gap:") {
			t.Errorf("capability-gap row ID %q is not unambiguously marked as a gap-tracking placeholder", row.ID)
		}
	}
}

// TestAddSourceRows_NonFragmentedConcepts_Unchanged is Bug 1's CURRENT/
// PENDING regression test: instruction/skill/hook/policy/plugin sources are
// already 1:1 file-to-Candidate (extract.go extracts exactly one Candidate
// per Observation for these concepts, unlike mcp_server), so the
// mcp_server-scoped Candidate cross-reference addSourceRows now performs
// must be a no-op for them -- each still produces exactly one row keyed by
// its own bare Source path, with Active/Detail unchanged.
func TestAddSourceRows_NonFragmentedConcepts_Unchanged(t *testing.T) {
	candidates := []effective.Candidate{
		{Concept: "instruction", Ref: "AGENTS.md"},
		{Concept: "skill", Ref: "skills/foo/SKILL.md"},
	}
	sources := []domain.GenerationSourceEntry{
		{Concept: "instruction", Source: "AGENTS.md", Included: true, Reason: "repository instructions"},
		{Concept: "skill", Source: "skills/foo/SKILL.md", Included: true, Reason: "activated skill"},
	}
	rows := map[planeKey]PlaneRow{}
	addSourceRows(rows, sources, candidates)

	if len(rows) != 2 {
		t.Fatalf("addSourceRows produced %d rows, want 2 (one per file, no fragmentation): %+v", len(rows), rows)
	}
	instrRow, ok := rows[planeKey{"instruction", "AGENTS.md"}]
	if !ok {
		t.Fatalf("no row keyed (instruction, AGENTS.md), got: %+v", rows)
	}
	if !instrRow.Active || instrRow.Detail != "repository instructions" {
		t.Errorf("instruction row unexpectedly changed: %+v", instrRow)
	}
	skillRow, ok := rows[planeKey{"skill", "skills/foo/SKILL.md"}]
	if !ok {
		t.Fatalf("no row keyed (skill, skills/foo/SKILL.md), got: %+v", rows)
	}
	if !skillRow.Active || skillRow.Detail != "activated skill" {
		t.Errorf("skill row unexpectedly changed: %+v", skillRow)
	}
}
