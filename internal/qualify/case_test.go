package qualify

import (
	"path/filepath"
	"testing"
)

const testdataCaseDir = "testdata/fixtures/codex/0.0.0-test/sample-case"

func TestLoadCase(t *testing.T) {
	c, err := LoadCase(testdataCaseDir)
	if err != nil {
		t.Fatalf("LoadCase: %v", err)
	}
	if c.Host != "codex" || c.Version != "0.0.0-test" || c.Name != "sample-case" {
		t.Errorf("c = %+v, unexpected host/version/name", c)
	}
	if len(c.ExpectedObservations) != 2 {
		t.Errorf("len(ExpectedObservations) = %d, want 2", len(c.ExpectedObservations))
	}
	if len(c.ExpectedEffective.Entries) != 1 {
		t.Errorf("len(ExpectedEffective.Entries) = %d, want 1", len(c.ExpectedEffective.Entries))
	}
	if c.InputDir() != filepath.Join(testdataCaseDir, "input") {
		t.Errorf("InputDir() = %q, want %q", c.InputDir(), filepath.Join(testdataCaseDir, "input"))
	}
}

func TestLoadCaseHostMismatch(t *testing.T) {
	// Reuse the real case's files, but under a directory path claiming a
	// different host than invocation.yaml declares.
	dir := t.TempDir()
	mismatchDir := filepath.Join(dir, "claude-code", "0.0.0-test", "sample-case")
	if err := copyTree(testdataCaseDir, mismatchDir); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadCase(mismatchDir); err == nil {
		t.Error("LoadCase(host mismatch) error = nil, want error")
	}
}

func TestParseCasePath(t *testing.T) {
	host, version, name, err := parseCasePath("fixtures/codex/0.144.5/instructions-collision")
	if err != nil {
		t.Fatalf("parseCasePath: %v", err)
	}
	if host != "codex" || version != "0.144.5" || name != "instructions-collision" {
		t.Errorf("parseCasePath = %q, %q, %q, want codex, 0.144.5, instructions-collision", host, version, name)
	}
}

func TestParseCasePathTooShort(t *testing.T) {
	if _, _, _, err := parseCasePath("codex"); err == nil {
		t.Error("parseCasePath(too short) error = nil, want error")
	}
}
