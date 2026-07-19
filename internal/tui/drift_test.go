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

// TestSanitizeDefaultTierText_StripsPrecedenceProgramDetail is a regression
// test (Copilot review finding on this PR): internal/effective/merge.go's
// own Conflict.Reason can read, verbatim, "precedence program %q declares
// operator %s for concept %q ..." -- exactly the kind of native precedence-
// program/merge-operator detail this default Drift view's own doc comment
// (and docs/architecture/reporting.md §9) says must never surface here. Two
// real message shapes from internal/effective/merge.go are used as the
// table's inputs, plus a plain string that must pass through unchanged.
func TestSanitizeDefaultTierText_StripsPrecedenceProgramDetail(t *testing.T) {
	leaking := []string{
		`precedence program "codex-default" declares operator FIRST_WINS for concept "instruction", but the Knowledge Pack's resolve capability is "PARTIAL" (not EXACT/COMPATIBLE); 2 sources disagree and this package refuses to guess a winner`,
		`mcp_server "shared-tools" has 3 candidate sources and no qualified resolver could select a winner: no usable precedence program for concept "mcp_server": 3 sources disagree and either no PrecedenceProgram is declared or its operator ("DENY_WINS") is not one of the nine closed docs/ontology/README.md §3.1 operators -- an UNKNOWN/undeclared operator is never guessed past`,
		"qualify a precedence program for this concept in the host's Knowledge Pack so the resolver can select a winner, or resolve the collision with an explicit Profile/Exception",
	}
	for _, s := range leaking {
		got := sanitizeDefaultTierText(s)
		if contains(got, "precedence program") || contains(got, "operator") {
			t.Errorf("sanitizeDefaultTierText(%q) = %q, still leaks precedence-program/operator detail", s, got)
		}
	}

	plain := "2 project(s) affected, no resolver-level detail here"
	if got := sanitizeDefaultTierText(plain); got != plain {
		t.Errorf("sanitizeDefaultTierText(%q) = %q, want the plain string passed through unchanged", plain, got)
	}
}
