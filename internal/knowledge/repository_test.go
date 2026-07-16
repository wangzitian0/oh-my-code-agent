package knowledge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const codexManifestJSON = `{
  "apiVersion": "omca.dev/v1alpha1",
  "kind": "HostKnowledge",
  "metadata": {
    "id": "codex:cli:0.144",
    "host": "codex",
    "surface": "cli",
    "versionRange": ">=0.144.0 <0.145.0",
    "status": "FRESH"
  },
  "evidence": [ { "id": "codex-doc", "kind": "official-doc" } ],
  "capabilities": { "skill": { "discover": "PARTIAL", "resolve": "UNKNOWN" } }
}`

const claudeManifestJSON = `{
  "apiVersion": "omca.dev/v1alpha1",
  "kind": "HostKnowledge",
  "metadata": {
    "id": "claude-code:cli:2.1",
    "host": "claude-code",
    "surface": "cli",
    "versionRange": ">=2.1.0 <2.2.0",
    "status": "FRESH"
  },
  "evidence": [ { "id": "claude-doc", "kind": "official-doc" } ],
  "capabilities": { "skill": { "discover": "PARTIAL", "resolve": "UNKNOWN" } }
}`

func writePackDir(t *testing.T, root, relDir, content string) string {
	t.Helper()
	dir := filepath.Join(root, relDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return writeManifest(t, dir, content)
}

func TestLoadRepository_LoadsAllPacks(t *testing.T) {
	root := t.TempDir()
	writePackDir(t, root, filepath.Join("codex", "cli", "0.144"), codexManifestJSON)
	writePackDir(t, root, filepath.Join("claude-code", "cli", "2.1"), claudeManifestJSON)

	repo, err := LoadRepository(root)
	if err != nil {
		t.Fatalf("LoadRepository: %v", err)
	}
	packs := repo.Packs()
	if len(packs) != 2 {
		t.Fatalf("len(Packs()) = %d, want 2", len(packs))
	}
	// Packs() is sorted by metadata.id: "claude-code:cli:2.1" < "codex:cli:0.144".
	if packs[0].Knowledge.Metadata.ID != "claude-code:cli:2.1" {
		t.Errorf("Packs()[0].ID = %q, want %q", packs[0].Knowledge.Metadata.ID, "claude-code:cli:2.1")
	}
	if packs[1].Knowledge.Metadata.ID != "codex:cli:0.144" {
		t.Errorf("Packs()[1].ID = %q, want %q", packs[1].Knowledge.Metadata.ID, "codex:cli:0.144")
	}
}

func TestLoadRepository_IgnoresOtherFiles(t *testing.T) {
	root := t.TempDir()
	writePackDir(t, root, filepath.Join("codex", "cli", "0.144"), codexManifestJSON)
	dir := filepath.Join(root, "codex", "cli", "0.144")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("not a pack"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "sources.yaml"), []byte("not a pack either"), 0o644); err != nil {
		t.Fatal(err)
	}

	repo, err := LoadRepository(root)
	if err != nil {
		t.Fatalf("LoadRepository: %v", err)
	}
	if len(repo.Packs()) != 1 {
		t.Fatalf("len(Packs()) = %d, want 1 (non-manifest files must be ignored)", len(repo.Packs()))
	}
}

func TestLoadRepository_DuplicateIDFailsClosed(t *testing.T) {
	root := t.TempDir()
	writePackDir(t, root, filepath.Join("codex", "cli", "0.144"), codexManifestJSON)
	// A second directory declaring the identical metadata.id.
	writePackDir(t, root, filepath.Join("codex", "cli", "0.144-dup"), codexManifestJSON)

	if _, err := LoadRepository(root); err == nil {
		t.Fatal("LoadRepository: want error for two packs declaring the same metadata.id")
	}
}

func TestLoadRepository_PropagatesLoadPackError(t *testing.T) {
	root := t.TempDir()
	writePackDir(t, root, filepath.Join("codex", "cli", "broken"), `{ not valid json`)

	if _, err := LoadRepository(root); err == nil {
		t.Fatal("LoadRepository: want error when one pack file is structurally invalid")
	}
}

func TestLoadRepository_NonexistentRoot(t *testing.T) {
	if _, err := LoadRepository(filepath.Join(t.TempDir(), "does-not-exist")); err == nil {
		t.Fatal("LoadRepository: want error for a nonexistent root")
	}
}

func TestLoadRepository_EmptyDirectory(t *testing.T) {
	repo, err := LoadRepository(t.TempDir())
	if err != nil {
		t.Fatalf("LoadRepository: %v", err)
	}
	if len(repo.Packs()) != 0 {
		t.Errorf("Packs() = %v, want empty", repo.Packs())
	}
}

func testRepository(t *testing.T) Repository {
	t.Helper()
	root := t.TempDir()
	writePackDir(t, root, filepath.Join("codex", "cli", "0.144"), codexManifestJSON)
	writePackDir(t, root, filepath.Join("claude-code", "cli", "2.1"), claudeManifestJSON)
	repo, err := LoadRepository(root)
	if err != nil {
		t.Fatalf("LoadRepository: %v", err)
	}
	return repo
}

func TestResolve_QualifiedSingleMatch(t *testing.T) {
	repo := testRepository(t)

	res := repo.Resolve("codex", "cli", "0.144.5")
	if !res.Qualified {
		t.Fatalf("Resolve: Qualified = false, want true; Reason=%q", res.Reason)
	}
	if res.PackID != "codex:cli:0.144" {
		t.Errorf("PackID = %q, want %q", res.PackID, "codex:cli:0.144")
	}
	if res.Digest == "" {
		t.Error("Digest is empty for a qualified Resolution")
	}
	if res.Reason != "" {
		t.Errorf("Reason = %q, want empty for a qualified Resolution", res.Reason)
	}

	caps := res.CapabilityFor("skill")
	if caps.Discover != "PARTIAL" {
		t.Errorf("CapabilityFor(skill).Discover = %q, want %q", caps.Discover, "PARTIAL")
	}
}

// TestResolve_QualifiedButConceptNotDeclaredDegradesToObserved covers a
// qualified Resolution asked about a concept its matched Pack never
// declared (the test repository's codex:cli:0.144 pack only declares
// "skill"). A bare Go map lookup would silently return a zero-value
// CapabilityOps (ReconcileMode: "", not a valid enum member) instead of the
// documented ReconcileModeObserved degrade.
func TestResolve_QualifiedButConceptNotDeclaredDegradesToObserved(t *testing.T) {
	repo := testRepository(t)

	res := repo.Resolve("codex", "cli", "0.144.5")
	if !res.Qualified {
		t.Fatalf("Resolve: Qualified = false, want true; Reason=%q", res.Reason)
	}

	caps := res.CapabilityFor("hook")
	if caps.ReconcileMode != ReconcileModeObserved {
		t.Errorf("CapabilityFor(hook).ReconcileMode = %q, want %q (hook is not declared by the matched pack)", caps.ReconcileMode, ReconcileModeObserved)
	}
	if caps.Discover != "" {
		t.Errorf("CapabilityFor(hook).Discover = %q, want empty", caps.Discover)
	}
}

// TestResolve_NoQualifiedPack_DegradesToObserved is issue #11's acceptance
// criterion: "A host version outside every pack range resolves to 'no
// qualified pack' and downstream operations degrade to observed (test)."
func TestResolve_NoQualifiedPack_DegradesToObserved(t *testing.T) {
	repo := testRepository(t)

	res := repo.Resolve("codex", "cli", "0.999.0")
	if res.Qualified {
		t.Fatalf("Resolve: Qualified = true, want false for a version outside every pack's range")
	}
	if res.Reason == "" {
		t.Error("Reason is empty, want an explanation of why nothing qualified")
	}
	if res.PackID != "" || res.Digest != "" {
		t.Errorf("PackID=%q Digest=%q, want both empty for an unqualified Resolution", res.PackID, res.Digest)
	}

	caps := res.CapabilityFor("skill")
	if caps.ReconcileMode != ReconcileModeObserved {
		t.Errorf("CapabilityFor(skill).ReconcileMode = %q, want %q", caps.ReconcileMode, ReconcileModeObserved)
	}
	if caps.Discover != "" {
		t.Errorf("CapabilityFor(skill).Discover = %q, want empty (no optimistic guess from a mismatched pack)", caps.Discover)
	}
}

func TestResolve_NoQualifiedPack_UnknownHostHasNoMatchingPacks(t *testing.T) {
	repo := testRepository(t)
	res := repo.Resolve("opencode", "cli", "1.0.0")
	if res.Qualified {
		t.Fatal("Resolve: Qualified = true, want false for a host with zero loaded packs")
	}
}

func TestResolve_AmbiguousMultipleMatchesDegradesToObserved(t *testing.T) {
	root := t.TempDir()
	writePackDir(t, root, filepath.Join("codex", "cli", "0.144"), codexManifestJSON)
	overlapping := strings.Replace(codexManifestJSON, `"id": "codex:cli:0.144"`, `"id": "codex:cli:0.144-overlap"`, 1)
	writePackDir(t, root, filepath.Join("codex", "cli", "0.144-overlap"), overlapping)

	repo, err := LoadRepository(root)
	if err != nil {
		t.Fatalf("LoadRepository: %v", err)
	}

	res := repo.Resolve("codex", "cli", "0.144.5")
	if res.Qualified {
		t.Fatal("Resolve: Qualified = true, want false when two packs match the same host/surface/version ambiguously")
	}
	if !strings.Contains(res.Reason, "codex:cli:0.144") || !strings.Contains(res.Reason, "codex:cli:0.144-overlap") {
		t.Errorf("Reason = %q, want it to name both ambiguous pack IDs", res.Reason)
	}
	if res.CapabilityFor("skill").ReconcileMode != ReconcileModeObserved {
		t.Errorf("ambiguous match must still degrade to observed, got ReconcileMode=%q", res.CapabilityFor("skill").ReconcileMode)
	}
}

func TestResolve_SurfaceMustMatch(t *testing.T) {
	repo := testRepository(t)
	res := repo.Resolve("codex", "vscode", "0.144.5")
	if res.Qualified {
		t.Fatal("Resolve: Qualified = true, want false when the surface does not match any pack")
	}
}

func TestResolution_JSONOmitsUnexportedPack(t *testing.T) {
	repo := testRepository(t)
	res := repo.Resolve("codex", "cli", "0.144.5")

	raw, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var generic map[string]any
	if err := json.Unmarshal(raw, &generic); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"host", "surface", "version", "qualified", "packId", "digest"} {
		if _, ok := generic[key]; !ok {
			t.Errorf("Resolution JSON missing key %q: %s", key, raw)
		}
	}
	if _, ok := generic["pack"]; ok {
		t.Errorf("Resolution JSON leaks the unexported pack field: %s", raw)
	}
}

func TestDefault_LoadsRealCommittedPacks(t *testing.T) {
	repo, err := Default()
	if err != nil {
		t.Fatalf("Default: %v", err)
	}
	packs := repo.Packs()
	if len(packs) < 2 {
		t.Fatalf("Default() loaded %d packs, want at least 2 (codex:cli:0.144, claude-code:cli:2.1)", len(packs))
	}

	ids := make(map[string]bool, len(packs))
	for _, p := range packs {
		ids[p.Knowledge.Metadata.ID] = true
	}
	for _, want := range []string{"codex:cli:0.144", "claude-code:cli:2.1"} {
		if !ids[want] {
			t.Errorf("Default() packs = %v, want it to include %q", ids, want)
		}
	}

	res := repo.Resolve("codex", "cli", "0.144.5")
	if !res.Qualified {
		t.Errorf("Default() repository does not qualify codex 0.144.5 against its own committed codex:cli:0.144 pack; Reason=%q", res.Reason)
	}
	res2 := repo.Resolve("claude-code", "cli", "2.1.211")
	if !res2.Qualified {
		t.Errorf("Default() repository does not qualify claude-code 2.1.211 against its own committed claude-code:cli:2.1 pack; Reason=%q", res2.Reason)
	}
}
