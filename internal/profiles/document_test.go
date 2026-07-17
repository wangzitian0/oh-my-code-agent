package profiles

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
)

// TestDecodeYAMLDocument_MatchesJSONTagsNotFieldNames proves the pitfall
// this package's document.go doc comment calls out by name: a naive
// yaml.Unmarshal(data, &profile) would silently zero-value every field,
// because gopkg.in/yaml.v3 does not read `json:"..."` tags. This test
// writes a real YAML file using exactly the on-disk shape docs/product/
// requirements.md §4.1 documents (apiVersion/kind/metadata/spec, camelCase
// keys) and asserts every field actually landed in the typed
// domain.Profile — not just that decoding didn't error.
func TestDecodeYAMLDocument_MatchesJSONTagsNotFieldNames(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "profile.yaml")
	yamlDoc := `
apiVersion: omca.dev/v1alpha1
kind: Profile
metadata:
  id: company:example
spec:
  assets:
    skills:
      - id: code-review
        intent: AVAILABLE
      - id: deep-refactor
        intent: DEFAULT
        hosts: [claude-code]
    mcpServers:
      - id: codegraph
        intent: DEFAULT
        hosts: [codex]
  policy:
    permissions:
      sandbox:
        intent: DEFAULT
        value: workspace-write
`
	if err := os.WriteFile(path, []byte(yamlDoc), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	var p domain.Profile
	if err := decodeYAMLDocument(path, &p); err != nil {
		t.Fatalf("decodeYAMLDocument: %v", err)
	}

	if p.APIVersion != domain.SupportedAPIVersion {
		t.Errorf("APIVersion = %q, want %q", p.APIVersion, domain.SupportedAPIVersion)
	}
	if p.Kind != "Profile" {
		t.Errorf("Kind = %q, want Profile", p.Kind)
	}
	if p.Metadata.ID != "company:example" {
		t.Errorf("Metadata.ID = %q, want company:example", p.Metadata.ID)
	}
	if len(p.Spec.Assets.Skills) != 2 {
		t.Fatalf("Skills = %d entries, want 2", len(p.Spec.Assets.Skills))
	}
	deepRefactor := p.Spec.Assets.Skills[1]
	if deepRefactor.ID != "deep-refactor" || deepRefactor.Intent != domain.IntentDefault {
		t.Errorf("deep-refactor = %+v, want id=deep-refactor intent=DEFAULT", deepRefactor)
	}
	if len(deepRefactor.Hosts) != 1 || deepRefactor.Hosts[0] != "claude-code" {
		t.Errorf("deep-refactor.Hosts = %v, want [claude-code]", deepRefactor.Hosts)
	}
	if len(p.Spec.Assets.MCPServers) != 1 || p.Spec.Assets.MCPServers[0].ID != "codegraph" {
		t.Fatalf("MCPServers = %+v, want one entry codegraph", p.Spec.Assets.MCPServers)
	}
	perm, ok := p.Spec.Policy.Permissions["sandbox"]
	if !ok || perm.Value != "workspace-write" {
		t.Errorf("Policy.Permissions[sandbox] = %+v, want value=workspace-write", perm)
	}

	if err := domain.ValidateProfile(p); err != nil {
		t.Errorf("ValidateProfile on the decoded document: %v", err)
	}
}

// TestDecodeYAMLDocument_InvalidYAML_NamesFile proves a malformed YAML
// document produces an error naming the file path, not a bare parser
// error a caller would have to guess the source of (issue #16 AC).
func TestDecodeYAMLDocument_InvalidYAML_NamesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "broken.yaml")
	if err := os.WriteFile(path, []byte("key: [unterminated"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	var p domain.Profile
	err := decodeYAMLDocument(path, &p)
	if err == nil {
		t.Fatal("expected an error for malformed YAML")
	}
	if got := err.Error(); !strings.Contains(got, path) {
		t.Errorf("error %q does not name file path %q", got, path)
	}
}

// TestDiscoverYAMLFiles_MissingDirIsNotAnError proves a directory absent
// from disk entirely (the common case for most of docs/architecture/
// README.md §7's layout on a fresh machine) yields no files and no error,
// not a failure — LoadProfiles/LoadBindings/LoadExceptions all depend on
// this to tolerate the layout's many optional directories.
func TestDiscoverYAMLFiles_MissingDirIsNotAnError(t *testing.T) {
	files, err := discoverYAMLFiles(filepath.Join(t.TempDir(), "does-not-exist"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("files = %v, want none", files)
	}
}

// TestDiscoverYAMLFiles_RecursiveAndSorted proves discoverYAMLFiles walks
// nested category directories (docs/architecture/README.md §7's
// profiles/{personal,company,team,task}/ layout) and returns a
// deterministically sorted result, ignoring non-YAML files.
func TestDiscoverYAMLFiles_RecursiveAndSorted(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "team", "b.yaml"), "b: 1")
	mustWriteFile(t, filepath.Join(root, "company", "a.yml"), "a: 1")
	mustWriteFile(t, filepath.Join(root, "README.md"), "not yaml")

	files, err := discoverYAMLFiles(root)
	if err != nil {
		t.Fatalf("discoverYAMLFiles: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("files = %v, want 2 entries", files)
	}
	want := []string{
		filepath.Join(root, "company", "a.yml"),
		filepath.Join(root, "team", "b.yaml"),
	}
	for i, w := range want {
		if files[i] != w {
			t.Errorf("files[%d] = %q, want %q", i, files[i], w)
		}
	}
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
