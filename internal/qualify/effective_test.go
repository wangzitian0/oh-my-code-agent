package qualify

import (
	"os"
	"path/filepath"
	"testing"
)

const validEffectiveJSON = `{
  "apiVersion": "omca.dev/v1alpha1",
  "kind": "FixtureExpectedEffective",
  "host": {"id": "codex", "version": "0.144.5"},
  "concept": "instruction",
  "entries": [
    {
      "logicalId": "codex.instructions.user-vs-project",
      "sources": [
        {"scope": "user", "path": "home/AGENTS.md", "disposition": "ACTIVE"},
        {"scope": "workspace", "path": "project/AGENTS.md", "disposition": "ACTIVE"}
      ],
      "mergeOperator": "CONCAT_ORDERED",
      "selectedSource": "UNKNOWN",
      "guarantee": "ADVISORY",
      "evidenceLevel": "E1",
      "confirmed": false,
      "reason": "no safe non-interactive introspection path exists"
    }
  ]
}`

func writeEffective(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "expected-effective.json")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadExpectedEffectiveValid(t *testing.T) {
	doc, err := LoadExpectedEffective(writeEffective(t, validEffectiveJSON))
	if err != nil {
		t.Fatalf("LoadExpectedEffective: %v", err)
	}
	if len(doc.Entries) != 1 || doc.Entries[0].SelectedSource != Unknown {
		t.Errorf("doc = %+v, want one UNKNOWN entry", doc)
	}
}

func TestLoadExpectedEffectiveUnknownButConfirmedIsRejected(t *testing.T) {
	bad := `{
  "apiVersion": "omca.dev/v1alpha1",
  "kind": "FixtureExpectedEffective",
  "host": {"id": "codex", "version": "0.144.5"},
  "concept": "instruction",
  "entries": [
    {
      "logicalId": "x",
      "mergeOperator": "CONCAT_ORDERED",
      "selectedSource": "UNKNOWN",
      "guarantee": "ADVISORY",
      "evidenceLevel": "E1",
      "confirmed": true,
      "reason": "contradicts itself"
    }
  ]
}`
	if _, err := LoadExpectedEffective(writeEffective(t, bad)); err == nil {
		t.Error("LoadExpectedEffective(UNKNOWN+confirmed) error = nil, want error")
	}
}

func TestLoadExpectedEffectiveConfirmedRequiresStrongEvidence(t *testing.T) {
	bad := `{
  "apiVersion": "omca.dev/v1alpha1",
  "kind": "FixtureExpectedEffective",
  "host": {"id": "codex", "version": "0.144.5"},
  "concept": "instruction",
  "entries": [
    {
      "logicalId": "x",
      "mergeOperator": "CONCAT_ORDERED",
      "selectedSource": "home/AGENTS.md",
      "guarantee": "ADVISORY",
      "evidenceLevel": "E1",
      "confirmed": true,
      "reason": "claims confirmed off weak evidence"
    }
  ]
}`
	if _, err := LoadExpectedEffective(writeEffective(t, bad)); err == nil {
		t.Error("LoadExpectedEffective(confirmed with E1 evidence) error = nil, want error")
	}
}

func TestLoadExpectedEffectiveConfirmedWithStrongEvidenceIsAccepted(t *testing.T) {
	good := `{
  "apiVersion": "omca.dev/v1alpha1",
  "kind": "FixtureExpectedEffective",
  "host": {"id": "codex", "version": "0.144.5"},
  "concept": "instruction",
  "entries": [
    {
      "logicalId": "x",
      "mergeOperator": "CONCAT_ORDERED",
      "selectedSource": "home/AGENTS.md",
      "guarantee": "ADVISORY",
      "evidenceLevel": "E4",
      "confirmed": true,
      "reason": "behaviorally probed via an isolated canary session"
    }
  ]
}`
	if _, err := LoadExpectedEffective(writeEffective(t, good)); err != nil {
		t.Errorf("LoadExpectedEffective(confirmed with E4 evidence) error = %v, want nil", err)
	}
}

func TestLoadExpectedEffectiveUnknownConcept(t *testing.T) {
	bad := `{
  "apiVersion": "omca.dev/v1alpha1",
  "kind": "FixtureExpectedEffective",
  "host": {"id": "codex", "version": "0.144.5"},
  "concept": "not-a-real-concept",
  "entries": [
    {"logicalId": "x", "mergeOperator": "CONCAT_ORDERED", "selectedSource": "UNKNOWN", "guarantee": "ADVISORY", "evidenceLevel": "E1", "reason": "n/a"}
  ]
}`
	if _, err := LoadExpectedEffective(writeEffective(t, bad)); err == nil {
		t.Error("LoadExpectedEffective(unknown concept) error = nil, want error")
	}
}

func TestLoadExpectedEffectiveBadMergeOperator(t *testing.T) {
	bad := `{
  "apiVersion": "omca.dev/v1alpha1",
  "kind": "FixtureExpectedEffective",
  "host": {"id": "codex", "version": "0.144.5"},
  "concept": "instruction",
  "entries": [
    {"logicalId": "x", "mergeOperator": "MADE_UP_OP", "selectedSource": "UNKNOWN", "guarantee": "ADVISORY", "evidenceLevel": "E1", "reason": "n/a"}
  ]
}`
	if _, err := LoadExpectedEffective(writeEffective(t, bad)); err == nil {
		t.Error("LoadExpectedEffective(bad merge operator) error = nil, want error")
	}
}

func TestLoadExpectedEffectiveNoEntries(t *testing.T) {
	bad := `{
  "apiVersion": "omca.dev/v1alpha1",
  "kind": "FixtureExpectedEffective",
  "host": {"id": "codex", "version": "0.144.5"},
  "concept": "instruction",
  "entries": []
}`
	if _, err := LoadExpectedEffective(writeEffective(t, bad)); err == nil {
		t.Error("LoadExpectedEffective(no entries) error = nil, want error")
	}
}

func TestLoadExpectedEffectiveFileNotFound(t *testing.T) {
	if _, err := LoadExpectedEffective(filepath.Join(t.TempDir(), "missing.json")); err == nil {
		t.Error("LoadExpectedEffective(missing file) error = nil, want error")
	}
}
