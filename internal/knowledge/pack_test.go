package knowledge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
)

// validManifestJSON is a minimal, schema-valid HostKnowledge document
// (matching internal/domain/testdata/hostknowledge-valid.json's shape),
// usable as-is or with one field substituted per test case.
const validManifestJSON = `{
  "apiVersion": "omca.dev/v1alpha1",
  "kind": "HostKnowledge",
  "metadata": {
    "id": "codex:cli:0.144",
    "host": "codex",
    "surface": "cli",
    "versionRange": ">=0.144.0 <0.145.0",
    "status": "FRESH"
  },
  "evidence": [
    { "id": "codex-environment-variables", "kind": "official-doc" }
  ],
  "capabilities": {
    "skill": { "discover": "PARTIAL" }
  }
}`

func writeManifest(t *testing.T, dir, content string) string {
	t.Helper()
	path := filepath.Join(dir, PackFileName)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadPack_Valid(t *testing.T) {
	dir := t.TempDir()
	path := writeManifest(t, dir, validManifestJSON)

	p, err := LoadPack(path)
	if err != nil {
		t.Fatalf("LoadPack: %v", err)
	}
	if p.Knowledge.Metadata.ID != "codex:cli:0.144" {
		t.Errorf("Metadata.ID = %q, want %q", p.Knowledge.Metadata.ID, "codex:cli:0.144")
	}
	if p.Path != path {
		t.Errorf("Path = %q, want %q", p.Path, path)
	}
	if !domain.IsCanonicalDigest(p.Digest) {
		t.Errorf("Digest = %q, not a canonical sha256 digest", p.Digest)
	}
}

func TestLoadPack_DigestIsReproducibleAndContentAddressed(t *testing.T) {
	dir := t.TempDir()
	path := writeManifest(t, dir, validManifestJSON)

	first, err := LoadPack(path)
	if err != nil {
		t.Fatal(err)
	}
	second, err := LoadPack(path)
	if err != nil {
		t.Fatal(err)
	}
	if first.Digest != second.Digest {
		t.Errorf("digest not reproducible: first=%q second=%q", first.Digest, second.Digest)
	}

	otherDir := t.TempDir()
	changed := strings.Replace(validManifestJSON, `"status": "FRESH"`, `"status": "DUE"`, 1)
	otherPath := writeManifest(t, otherDir, changed)
	other, err := LoadPack(otherPath)
	if err != nil {
		t.Fatal(err)
	}
	if other.Digest == first.Digest {
		t.Error("two Packs with different content produced the same digest")
	}
}

func TestLoadPack_RejectsFloatingLatestID(t *testing.T) {
	dir := t.TempDir()
	content := strings.Replace(validManifestJSON, `"id": "codex:cli:0.144"`, `"id": "latest"`, 1)
	path := writeManifest(t, dir, content)

	if _, err := LoadPack(path); err == nil {
		t.Fatal("LoadPack: want error for a floating \"latest\" metadata.id, got nil")
	}
}

func TestLoadPack_RejectsFloatingLatestVersionRange(t *testing.T) {
	dir := t.TempDir()
	content := strings.Replace(validManifestJSON, `">=0.144.0 <0.145.0"`, `"latest"`, 1)
	path := writeManifest(t, dir, content)

	_, err := LoadPack(path)
	if err == nil {
		t.Fatal("LoadPack: want error for a floating \"latest\" metadata.versionRange, got nil")
	}
	if !strings.Contains(err.Error(), "floating") {
		t.Errorf("error = %q, want it to explain the floating-reference rejection", err.Error())
	}
}

func TestLoadPack_RejectsFloatingVersionRangeCaseInsensitively(t *testing.T) {
	for _, tag := range []string{"LATEST", "Latest", "  latest  ", "stable", "HEAD"} {
		t.Run(tag, func(t *testing.T) {
			dir := t.TempDir()
			content := strings.Replace(validManifestJSON, `">=0.144.0 <0.145.0"`, `"`+tag+`"`, 1)
			path := writeManifest(t, dir, content)
			if _, err := LoadPack(path); err == nil {
				t.Fatalf("LoadPack: want error for floating tag %q, got nil", tag)
			}
		})
	}
}

func TestLoadPack_RejectsInvalidVersionRangeSyntax(t *testing.T) {
	dir := t.TempDir()
	content := strings.Replace(validManifestJSON, `">=0.144.0 <0.145.0"`, `"whatever version works"`, 1)
	path := writeManifest(t, dir, content)

	if _, err := LoadPack(path); err == nil {
		t.Fatal("LoadPack: want error for a versionRange that is not this package's comparator syntax")
	}
}

func TestLoadPack_RejectsStructurallyInvalid(t *testing.T) {
	dir := t.TempDir()
	content := strings.Replace(validManifestJSON, `"evidence": [
    { "id": "codex-environment-variables", "kind": "official-doc" }
  ],`, `"evidence": [],`, 1)
	path := writeManifest(t, dir, content)

	if _, err := LoadPack(path); err == nil {
		t.Fatal("LoadPack: want error for empty evidence (domain.ValidateHostKnowledge requires non-empty evidence)")
	}
}

func TestLoadPack_MissingFile(t *testing.T) {
	if _, err := LoadPack(filepath.Join(t.TempDir(), "does-not-exist.json")); err == nil {
		t.Fatal("LoadPack: want error for a missing file")
	}
}

func TestLoadPack_MalformedJSON(t *testing.T) {
	dir := t.TempDir()
	path := writeManifest(t, dir, `{ not valid json `)
	if _, err := LoadPack(path); err == nil {
		t.Fatal("LoadPack: want error for malformed JSON")
	}
}

func TestValidatePackReference(t *testing.T) {
	cases := []struct {
		value   string
		wantErr bool
	}{
		{"latest", true},
		{"LATEST", true},
		{" latest ", true},
		{"stable", true},
		{"head", true},
		{"codex:cli:0.144", false},
		{">=0.144.0 <0.145.0", false},
		{"", false},
	}
	for _, c := range cases {
		err := ValidatePackReference("field", c.value)
		if (err != nil) != c.wantErr {
			t.Errorf("ValidatePackReference(field, %q) error = %v, wantErr %v", c.value, err, c.wantErr)
		}
	}
}
