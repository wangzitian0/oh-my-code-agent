package ontology

import (
	"path/filepath"
	"strings"
	"testing"
)

func testRegistry(t *testing.T) *Registry {
	t.Helper()
	reg, err := LoadRegistry(filepath.Join("..", "..", "ontology", "concepts"))
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	return reg
}

func TestLoadRegistry_LoadsAllThreeConcepts(t *testing.T) {
	reg := testRegistry(t)
	ids := reg.IDs()
	want := []string{"instruction", "mcp_server", "skill"}
	if len(ids) != len(want) {
		t.Fatalf("IDs() = %v, want %v", ids, want)
	}
	for i, id := range want {
		if ids[i] != id {
			t.Errorf("IDs()[%d] = %q, want %q", i, ids[i], id)
		}
	}
}

func TestRegistry_Skill_LogicalIdentityAndMergeOperators(t *testing.T) {
	reg := testRegistry(t)
	skill, ok := reg.Concept("skill")
	if !ok {
		t.Fatal("expected a skill concept")
	}
	if skill.NonEquivalence == "" {
		t.Error("skill.NonEquivalence must not be empty")
	}
	wantFields := []string{"name", "source.kind"}
	if len(skill.LogicalIdentity.Fields) != len(wantFields) {
		t.Fatalf("skill.LogicalIdentity.Fields = %v, want %v", skill.LogicalIdentity.Fields, wantFields)
	}
	for i, f := range wantFields {
		if skill.LogicalIdentity.Fields[i] != f {
			t.Errorf("skill.LogicalIdentity.Fields[%d] = %q, want %q", i, skill.LogicalIdentity.Fields[i], f)
		}
	}

	foundUnionByID := false
	for _, op := range skill.MergeOperators {
		if err := ValidateMergeOperator(op); err != nil {
			t.Errorf("skill declares invalid merge operator: %v", err)
		}
		if op == OpUnionByID {
			foundUnionByID = true
		}
	}
	if !foundUnionByID {
		t.Errorf("skill.MergeOperators = %v, want it to include UNION_BY_ID (docs/ontology/README.md §3.2 examples)", skill.MergeOperators)
	}
}

func TestRegistry_Instruction_UsesConcatOrdered(t *testing.T) {
	reg := testRegistry(t)
	instruction, ok := reg.Concept("instruction")
	if !ok {
		t.Fatal("expected an instruction concept")
	}
	found := false
	for _, op := range instruction.MergeOperators {
		if op == OpConcatOrdered {
			found = true
		}
	}
	if !found {
		t.Errorf("instruction.MergeOperators = %v, want it to include CONCAT_ORDERED (docs/ontology/README.md §3.2: Codex AGENTS.md example)", instruction.MergeOperators)
	}
}

func TestRegistry_MCPServer_LogicalIdentity(t *testing.T) {
	reg := testRegistry(t)
	mcp, ok := reg.Concept("mcp_server")
	if !ok {
		t.Fatal("expected an mcp_server concept")
	}
	wantFields := []string{"transport", "id"}
	if len(mcp.LogicalIdentity.Fields) != len(wantFields) {
		t.Fatalf("mcp_server.LogicalIdentity.Fields = %v, want %v", mcp.LogicalIdentity.Fields, wantFields)
	}
	for i, f := range wantFields {
		if mcp.LogicalIdentity.Fields[i] != f {
			t.Errorf("mcp_server.LogicalIdentity.Fields[%d] = %q, want %q", i, mcp.LogicalIdentity.Fields[i], f)
		}
	}
}

func TestConcept_PackageLevelLookup(t *testing.T) {
	// Uses the package-level default loader (runtime.Caller-relative path),
	// not the explicit test registry, proving the zero-configuration path
	// normalize.go depends on actually resolves.
	skill, ok := Concept("skill")
	if !ok {
		t.Fatal("expected package-level Concept(\"skill\") to resolve")
	}
	if skill.ID != "skill" {
		t.Errorf("skill.ID = %q, want \"skill\"", skill.ID)
	}

	if _, ok := Concept("does-not-exist"); ok {
		t.Error("expected Concept(\"does-not-exist\") to report false")
	}
}

func TestLoadRegistry_MissingDir(t *testing.T) {
	if _, err := LoadRegistry(filepath.Join("testdata", "does-not-exist")); err == nil {
		t.Error("expected an error loading a nonexistent concepts directory")
	}
}

func TestLoadRegistry_RejectsMalformedConcepts(t *testing.T) {
	cases := []string{
		"missing-conceptid",
		"missing-identity",
		"missing-nonequivalence",
		"missing-mergeoperators",
		"bad-operator",
		"invalid-json",
		"duplicate-conceptid",
	}
	for _, dir := range cases {
		t.Run(dir, func(t *testing.T) {
			if _, err := LoadRegistry(filepath.Join("testdata", dir)); err == nil {
				t.Errorf("LoadRegistry(testdata/%s) = nil error, want a rejection", dir)
			}
		})
	}
}

// TestLoadRegistry_DuplicateConceptID_FailsClosed is the named regression:
// two concept files declaring the same conceptId must reject the whole
// load, never silently let the later file (in directory-listing order)
// override the earlier one depending on filesystem enumeration order.
func TestLoadRegistry_DuplicateConceptID_FailsClosed(t *testing.T) {
	_, err := LoadRegistry(filepath.Join("testdata", "duplicate-conceptid"))
	if err == nil {
		t.Fatal("expected an error for two concept files declaring the same conceptId")
	}
	if !strings.Contains(err.Error(), "already declared") {
		t.Errorf("error = %v, want it to mention the duplicate declaration", err)
	}
}
