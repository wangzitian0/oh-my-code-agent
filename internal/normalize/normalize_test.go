package normalize

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/wangzitian0/oh-my-code-agent/internal/ontology"
)

// TestMergeOperatorsFor_MatchesOntologyLoaderDirectly proves
// MergeOperatorsFor's answer for "skill" is exactly what
// internal/ontology's own loader reports for "skill" — not a value that
// merely happens to agree today because someone copied it once. If
// normalize.go ever grows a local map of concept -> merge operators instead
// of calling ontology.Concept, this test still passes only by coincidence
// for as long as nobody edits ontology/concepts/skill.json; the import
// -boundary test below is what actually catches that regression.
func TestMergeOperatorsFor_MatchesOntologyLoaderDirectly(t *testing.T) {
	want, ok := ontology.Concept("skill")
	if !ok {
		t.Fatal("expected internal/ontology to resolve the skill concept")
	}

	got, ok := MergeOperatorsFor("skill")
	if !ok {
		t.Fatal("expected MergeOperatorsFor(\"skill\") to resolve")
	}
	if len(got) != len(want.MergeOperators) {
		t.Fatalf("MergeOperatorsFor(skill) = %v, want %v", got, want.MergeOperators)
	}
	for i := range got {
		if got[i] != want.MergeOperators[i] {
			t.Fatalf("MergeOperatorsFor(skill)[%d] = %q, want %q", i, got[i], want.MergeOperators[i])
		}
	}

	if _, ok := MergeOperatorsFor("does-not-exist"); ok {
		t.Error("expected MergeOperatorsFor for an unknown concept to report false")
	}
}

func TestLogicalIdentityFor_MatchesOntologyLoaderDirectly(t *testing.T) {
	want, ok := ontology.Concept("mcp_server")
	if !ok {
		t.Fatal("expected internal/ontology to resolve the mcp_server concept")
	}
	got, ok := LogicalIdentityFor("mcp_server")
	if !ok {
		t.Fatal("expected LogicalIdentityFor(\"mcp_server\") to resolve")
	}
	if got.Rule != want.LogicalIdentity.Rule {
		t.Fatalf("LogicalIdentityFor(mcp_server).Rule = %q, want %q", got.Rule, want.LogicalIdentity.Rule)
	}
}

// TestPackageImportsOntology_NotADuplicateSourceOfTruth is the actual
// import-boundary proof: it shells out to `go list` and checks that the
// compiled internal/normalize package really does import
// internal/ontology. A future change that deletes the ontology.Concept
// call and replaces it with a hard-coded map would still make the two
// value-equality tests above pass today, but it would make normalize stop
// importing ontology, and this test would catch that structural drift
// directly rather than relying on the fixture values never being copied
// out of sync.
func TestPackageImportsOntology_NotADuplicateSourceOfTruth(t *testing.T) {
	out, err := exec.Command("go", "list", "-f", "{{join .Imports \"\\n\"}}", ".").Output()
	if err != nil {
		t.Skipf("skipping import-boundary check: `go list` unavailable or failed: %v", err)
	}
	imports := strings.Split(strings.TrimSpace(string(out)), "\n")
	for _, imp := range imports {
		if strings.HasSuffix(imp, "/internal/ontology") {
			return
		}
	}
	t.Fatalf("internal/normalize does not import internal/ontology (imports: %v); concept facts must be looked up through ontology's loader, not duplicated locally", imports)
}
