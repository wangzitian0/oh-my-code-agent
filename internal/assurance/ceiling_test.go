package assurance

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	hostcontext "github.com/wangzitian0/oh-my-code-agent/internal/context"
	"github.com/wangzitian0/oh-my-code-agent/internal/ontology"
)

// repoRoot locates this repository's root relative to this source file's
// own location, the same runtime.Caller trick internal/effective/
// fixture_test.go and internal/report/build_test.go use.
func repoRoot() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..")
}

func testConceptRegistry(t *testing.T) *ontology.Registry {
	t.Helper()
	reg, err := ontology.LoadRegistry(filepath.Join(repoRoot(), "ontology", "concepts"))
	if err != nil {
		t.Fatalf("ontology.LoadRegistry: %v", err)
	}
	return reg
}

// TestCeilings_CoverEveryHostConceptCell proves the ceiling table is
// complete: every canonical host this package's own consumer
// (internal/context.DetectedHostIDs) knows how to detect, crossed with
// every loaded ontology concept plus the HostConceptClaim pseudo-concept,
// has exactly one row -- no cell is silently missing (which would fall
// back to clampToCeiling's conservative E1 default, masking a gap instead
// of surfacing it) and none is duplicated (which would make CeilingFor's
// first-match lookup order-dependent).
func TestCeilings_CoverEveryHostConceptCell(t *testing.T) {
	reg := testConceptRegistry(t)
	concepts := append(append([]string{}, reg.IDs()...), HostConceptClaim)

	seen := map[[2]string]int{}
	for _, c := range Ceilings {
		seen[[2]string{c.Host, c.Concept}]++
	}

	for _, host := range hostcontext.DetectedHostIDs {
		for _, concept := range concepts {
			key := [2]string{host, concept}
			switch seen[key] {
			case 0:
				t.Errorf("Ceilings has no row for (host=%q, concept=%q)", host, concept)
			case 1:
				// exactly right
			default:
				t.Errorf("Ceilings has %d rows for (host=%q, concept=%q), want exactly 1", seen[key], host, concept)
			}
		}
	}

	wantRows := len(hostcontext.DetectedHostIDs) * len(concepts)
	if len(Ceilings) != wantRows {
		t.Errorf("len(Ceilings) = %d, want %d (%d hosts x %d concepts, including %q)", len(Ceilings), wantRows, len(hostcontext.DetectedHostIDs), len(concepts), HostConceptClaim)
	}
}

// TestCeilings_RowsAreValidAndCited proves every row is structurally sound
// and carries the citable justification docs/architecture/evidence-
// ceiling.md §1 requires before a Ceiling may be trusted at all.
func TestCeilings_RowsAreValidAndCited(t *testing.T) {
	for _, c := range Ceilings {
		t.Run(c.Host+"/"+c.Concept, func(t *testing.T) {
			if !c.Ceiling.Valid() {
				t.Errorf("Ceiling %q is not a valid domain.EvidenceLevel", c.Ceiling)
			}
			if c.Ceiling.Rank() > 3 {
				t.Errorf("Ceiling %q exceeds E3 -- issue #26 scopes this table to E0-E3 only", c.Ceiling)
			}
			if c.IntrospectionSurface == "" {
				t.Error("IntrospectionSurface is empty; use the literal \"none documented\" when no surface exists")
			}
			if c.Reason == "" {
				t.Error("Reason is empty -- every ceiling must cite the specific finding it is grounded in")
			}
			if c.Citation == "" {
				t.Error("Citation is empty -- every ceiling must name its committed source(s)")
			}
			if c.ResolveCapability != "" && !c.ResolveCapability.Valid() {
				t.Errorf("ResolveCapability %q is not a valid domain.CapabilityLevel", c.ResolveCapability)
			}
		})
	}
}

// TestCeilings_ConceptRowsCapAtE1_HostRowsCapAtE3 grounds this table's
// values against what this repository can actually prove today (not what
// this test would like to be true): every real ontology-concept row
// (instruction/skill/mcp_server, both hosts) is capped at E1 because both
// committed Knowledge Packs declare resolve: UNKNOWN for all three
// concepts (knowledge/hosts/{codex,claude-code}/*/manifest.json) and
// fixtures/README.md establishes no safe introspection surface exists;
// only the HostConceptClaim ("host") rows reach E3, via each host's real,
// already-safety-proven --version probe.
func TestCeilings_ConceptRowsCapAtE1_HostRowsCapAtE3(t *testing.T) {
	reg := testConceptRegistry(t)
	for _, host := range hostcontext.DetectedHostIDs {
		for _, concept := range reg.IDs() {
			ceiling, ok := CeilingFor(Ceilings, host, concept)
			if !ok {
				t.Fatalf("no ceiling row for (%s, %s)", host, concept)
			}
			if ceiling != "E1" {
				t.Errorf("CeilingFor(%s, %s) = %s, want E1 (both real Knowledge Packs declare resolve: UNKNOWN and no introspection surface is documented)", host, concept, ceiling)
			}
		}
		hostCeiling, ok := CeilingFor(Ceilings, host, HostConceptClaim)
		if !ok {
			t.Fatalf("no ceiling row for (%s, %s)", host, HostConceptClaim)
		}
		if hostCeiling != "E3" {
			t.Errorf("CeilingFor(%s, %s) = %s, want E3 (a real --version probe is a native introspection surface)", host, HostConceptClaim, hostCeiling)
		}
	}
}

// TestCeilingFor_UndeclaredCell proves CeilingFor's documented "no row
// means no match" contract -- callers (clampToCeiling) are the ones who
// decide an undeclared cell degrades to E1, not this lookup itself, which
// must not silently invent a value.
func TestCeilingFor_UndeclaredCell(t *testing.T) {
	if _, ok := CeilingFor(Ceilings, "codex", "not-a-real-concept"); ok {
		t.Error("CeilingFor matched an undeclared cell")
	}
}

// TestCeilings_MatchThisDoc proves internal/assurance/ceiling.go and
// docs/architecture/evidence-ceiling.md never name a different Ceiling for
// the same (host, concept) cell -- the doc.go-documented invariant this
// package's whole review workflow depends on: a human reviews the doc, a
// test enforces that the code agrees with what was reviewed.
func TestCeilings_MatchThisDoc(t *testing.T) {
	docPath := filepath.Join(repoRoot(), "docs", "architecture", "evidence-ceiling.md")
	raw, err := os.ReadFile(docPath)
	if err != nil {
		t.Fatalf("reading %s: %v", docPath, err)
	}
	doc := string(raw)

	for _, c := range Ceilings {
		hostMarker := fmt.Sprintf("`%s`", c.Host)
		conceptMarker := fmt.Sprintf("`%s`", c.Concept)
		levelMarker := fmt.Sprintf("**%s**", c.Ceiling)

		found := false
		for _, line := range strings.Split(doc, "\n") {
			if !strings.HasPrefix(strings.TrimSpace(line), "|") {
				continue
			}
			if strings.Contains(line, hostMarker) && strings.Contains(line, conceptMarker) {
				found = true
				if !strings.Contains(line, levelMarker) {
					t.Errorf("doc row for (%s, %s) does not contain %q:\n%s", c.Host, c.Concept, levelMarker, line)
				}
				break
			}
		}
		if !found {
			t.Errorf("docs/architecture/evidence-ceiling.md has no table row naming both %s and %s", hostMarker, conceptMarker)
		}
	}
}
