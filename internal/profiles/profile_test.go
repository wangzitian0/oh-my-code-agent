package profiles

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
)

const validProfileYAML = `
apiVersion: omca.dev/v1alpha1
kind: Profile
metadata:
  id: %s
spec:
  assets:
    skills:
      - id: code-review
        intent: AVAILABLE
`

const invalidProfileYAML = `
apiVersion: omca.dev/v1alpha1
kind: Profile
metadata:
  id: company:broken
spec:
  assets:
    skills:
      - id: code-review
        intent: NOT_A_REAL_INTENT
`

// TestLoadProfiles_RealFilesOnDisk builds a fake ~/.config/omca/profiles/
// layout (personal/company/team/task, per docs/architecture/README.md §7)
// under t.TempDir() with real YAML files and proves LoadProfiles reads and
// validates every one of them — the actual file-loading path, not just an
// in-memory struct literal.
func TestLoadProfiles_RealFilesOnDisk(t *testing.T) {
	configRoot := filepath.Join(t.TempDir(), ".config", "omca", "profiles")
	mustWriteFile(t, filepath.Join(configRoot, "personal", "alice.yaml"), fmtProfile("personal:alice"))
	mustWriteFile(t, filepath.Join(configRoot, "company", "example.yaml"), fmtProfile("company:example"))
	// task/ is present in §7's layout but commonly empty; leave it absent
	// entirely to also prove a missing category directory is tolerated.

	got, err := LoadProfiles([]string{configRoot})
	if err != nil {
		t.Fatalf("LoadProfiles: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("LoadProfiles returned %d profiles, want 2: %+v", len(got), got)
	}
	ids := map[string]bool{got[0].Metadata.ID: true, got[1].Metadata.ID: true}
	if !ids["personal:alice"] || !ids["company:example"] {
		t.Errorf("LoadProfiles ids = %v, want personal:alice and company:example", ids)
	}
	for _, p := range got {
		if err := domain.ValidateProfile(p); err != nil {
			t.Errorf("loaded profile %s failed re-validation: %v", p.Metadata.ID, err)
		}
	}
}

// TestLoadProfiles_MissingDirectoriesAreSkipped proves passing several
// directories, most of which do not exist on disk (the common case: a
// fresh machine has no ~/.config/omca/profiles/company/ yet), still
// succeeds and returns only the Profiles that were actually present.
func TestLoadProfiles_MissingDirectoriesAreSkipped(t *testing.T) {
	root := t.TempDir()
	present := filepath.Join(root, "profiles", "personal")
	mustWriteFile(t, filepath.Join(present, "alice.yaml"), fmtProfile("personal:alice"))

	dirs := []string{
		present,
		filepath.Join(root, "profiles", "company"), // absent
		filepath.Join(root, "profiles", "team"),    // absent
		filepath.Join(root, "profiles", "task"),    // absent
	}
	got, err := LoadProfiles(dirs)
	if err != nil {
		t.Fatalf("LoadProfiles: %v", err)
	}
	if len(got) != 1 || got[0].Metadata.ID != "personal:alice" {
		t.Fatalf("LoadProfiles = %+v, want exactly [personal:alice]", got)
	}
}

// TestLoadProfiles_InvalidDocument_ActionableError is issue #16's literal
// AC: "invalid documents produce actionable errors naming file and field."
// The fixture's skill intent is not one of the closed REQUIRED/DEFAULT/
// AVAILABLE/DENIED enum, so domain.ValidateProfile rejects it; LoadProfiles
// must wrap that rejection with the file path, not let a bare
// "invalid intent" error surface with no indication of which file.
func TestLoadProfiles_InvalidDocument_ActionableError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "broken.yaml")
	mustWriteFile(t, path, invalidProfileYAML)

	_, err := LoadProfiles([]string{dir})
	if err == nil {
		t.Fatal("expected an error for an invalid Profile document")
	}
	msg := err.Error()
	if !strings.Contains(msg, path) {
		t.Errorf("error %q does not name the file path %q", msg, path)
	}
	if !strings.Contains(msg, "intent") {
		t.Errorf("error %q does not name the offending field (intent)", msg)
	}
}

// TestLoadProfiles_RepositoryLayout proves LoadProfiles works equally well
// against the flat <repository>/.omca/profiles/ layout (no personal/
// company/team/task subdirectories — docs/architecture/README.md §7's
// second layout block).
func TestLoadProfiles_RepositoryLayout(t *testing.T) {
	repoProfiles := filepath.Join(t.TempDir(), "repo", ".omca", "profiles")
	mustWriteFile(t, filepath.Join(repoProfiles, "order-service.yaml"), fmtProfile("project:order-service"))

	got, err := LoadProfiles([]string{repoProfiles})
	if err != nil {
		t.Fatalf("LoadProfiles: %v", err)
	}
	if len(got) != 1 || got[0].Metadata.ID != "project:order-service" {
		t.Fatalf("LoadProfiles = %+v, want exactly [project:order-service]", got)
	}
}

func fmtProfile(id string) string {
	return fmt.Sprintf(validProfileYAML, id)
}
