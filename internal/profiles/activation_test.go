package profiles

import (
	"path/filepath"
	"strings"
	"testing"
)

const activationYAML = `
apiVersion: omca.dev/v1alpha1
kind: Activation
metadata:
  worktree: worktree:sha256:0123456789abcdef
spec:
  enable:
    skills: [code-review]
    mcpServers: [codegraph]
  disable:
    skills: [release-production]
  hosts:
    claude-code:
      enable:
        skills: [ui-review]
    codex:
      disable:
        mcpServers: [internal-docs]
`

// TestLoadActivation_RealFileOnDisk loads the exact Activation document
// docs/product/requirements.md §4.3 shows, from a real file under a fake
// worktree state dir (docs/architecture/README.md §8's
// <worktree state dir>/desired/activation.yaml).
func TestLoadActivation_RealFileOnDisk(t *testing.T) {
	stateDir := t.TempDir()
	path := filepath.Join(stateDir, "desired", "activation.yaml")
	mustWriteFile(t, path, activationYAML)

	a, ok, err := LoadActivation(path)
	if err != nil {
		t.Fatalf("LoadActivation: %v", err)
	}
	if !ok {
		t.Fatal("ok = false, want true for a present file")
	}
	if a.Metadata.Worktree != "worktree:sha256:0123456789abcdef" {
		t.Errorf("Metadata.Worktree = %q", a.Metadata.Worktree)
	}
	claude, present := a.Spec.Hosts["claude-code"]
	if !present || len(claude.Enable.Skills) != 1 || claude.Enable.Skills[0] != "ui-review" {
		t.Errorf("hosts.claude-code.enable.skills = %+v, want [ui-review]", claude.Enable)
	}
	codex, present := a.Spec.Hosts["codex"]
	if !present || len(codex.Disable.MCPServers) != 1 || codex.Disable.MCPServers[0] != "internal-docs" {
		t.Errorf("hosts.codex.disable.mcpServers = %+v, want [internal-docs]", codex.Disable)
	}
}

// TestLoadActivation_MissingFileIsNotAnError proves "no worktree-specific
// choices recorded yet" is a normal, no-error outcome, not a failure.
func TestLoadActivation_MissingFileIsNotAnError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "desired", "activation.yaml")
	a, ok, err := LoadActivation(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error("ok = true, want false for a missing file")
	}
	if a.Metadata.Worktree != "" {
		t.Errorf("expected the zero-value Activation, got %+v", a)
	}
}

// TestLoadActivation_InvalidDocument_ActionableError proves an Activation
// naming an unknown host id (violating docs/product/requirements.md §4.3's
// closed host registry) fails closed with an error naming the file.
func TestLoadActivation_InvalidDocument_ActionableError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "activation.yaml")
	mustWriteFile(t, path, `
apiVersion: omca.dev/v1alpha1
kind: Activation
metadata:
  worktree: worktree:sha256:abc
spec:
  hosts:
    not-a-real-host:
      enable:
        skills: [x]
`)
	_, _, err := LoadActivation(path)
	if err == nil {
		t.Fatal("expected an error for an unknown host id")
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("error %q does not name the file path %q", err.Error(), path)
	}
}
