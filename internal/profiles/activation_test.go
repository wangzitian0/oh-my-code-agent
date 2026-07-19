package profiles

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
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

// TestPersistActivation_RoundTripsThroughLoadActivation is PersistActivation's
// core contract (issue #35): what it writes to
// <worktree state dir>/desired/activation.yaml, LoadActivation reads back
// byte-for-byte equivalent, including a host-scoped Enable/Disable
// selection -- the exact round trip internal/tui's stageAssetActivation
// depends on for a later, independent profiles.Compose call to see the
// same merged Activation a pending generation was compiled from.
func TestPersistActivation_RoundTripsThroughLoadActivation(t *testing.T) {
	stateDir := t.TempDir()
	a := domain.Activation{
		APIVersion: domain.SupportedAPIVersion,
		Kind:       "Activation",
		Metadata:   domain.ActivationMetadata{Worktree: "worktree:sha256:persist-test"},
		Spec: domain.ActivationSpec{
			Hosts: map[string]domain.HostActivation{
				"codex": {Enable: domain.ActivationSelection{Skills: []string{"code-review"}, MCPServers: []string{"internal-docs"}}},
			},
		},
	}

	if err := PersistActivation(stateDir, a); err != nil {
		t.Fatalf("PersistActivation: %v", err)
	}

	got, ok, err := LoadActivation(filepath.Join(stateDir, "desired", "activation.yaml"))
	if err != nil {
		t.Fatalf("LoadActivation after PersistActivation: %v", err)
	}
	if !ok {
		t.Fatal("ok = false after PersistActivation, want true")
	}
	if got.Metadata.Worktree != a.Metadata.Worktree {
		t.Errorf("Metadata.Worktree = %q, want %q", got.Metadata.Worktree, a.Metadata.Worktree)
	}
	codex, present := got.Spec.Hosts["codex"]
	if !present {
		t.Fatal("spec.hosts.codex missing after round trip")
	}
	if len(codex.Enable.Skills) != 1 || codex.Enable.Skills[0] != "code-review" {
		t.Errorf("hosts.codex.enable.skills = %+v, want [code-review]", codex.Enable.Skills)
	}
	if len(codex.Enable.MCPServers) != 1 || codex.Enable.MCPServers[0] != "internal-docs" {
		t.Errorf("hosts.codex.enable.mcpServers = %+v, want [internal-docs]", codex.Enable.MCPServers)
	}
}

// TestPersistActivation_Overwrites proves a second PersistActivation call
// replaces the first document entirely (atomic rename, matching
// PersistSelection's identical discipline) rather than merging with it --
// the caller (stageAssetActivation) is responsible for merging onto a
// freshly-loaded Activation before calling this function.
func TestPersistActivation_Overwrites(t *testing.T) {
	stateDir := t.TempDir()
	first := domain.Activation{
		APIVersion: domain.SupportedAPIVersion,
		Kind:       "Activation",
		Metadata:   domain.ActivationMetadata{Worktree: "worktree:sha256:overwrite-test"},
		Spec:       domain.ActivationSpec{Hosts: map[string]domain.HostActivation{"codex": {Enable: domain.ActivationSelection{Skills: []string{"a"}}}}},
	}
	second := first
	second.Spec.Hosts = map[string]domain.HostActivation{"codex": {Enable: domain.ActivationSelection{Skills: []string{"b"}}}}

	if err := PersistActivation(stateDir, first); err != nil {
		t.Fatalf("PersistActivation(first): %v", err)
	}
	if err := PersistActivation(stateDir, second); err != nil {
		t.Fatalf("PersistActivation(second): %v", err)
	}

	got, _, err := LoadActivation(filepath.Join(stateDir, "desired", "activation.yaml"))
	if err != nil {
		t.Fatalf("LoadActivation: %v", err)
	}
	if skills := got.Spec.Hosts["codex"].Enable.Skills; len(skills) != 1 || skills[0] != "b" {
		t.Errorf("hosts.codex.enable.skills = %+v, want [b] (second write must fully replace the first)", skills)
	}
}

// TestPersistActivation_RejectsInvalidActivation proves PersistActivation
// validates before writing (domain.ValidateActivation), refusing to
// durably persist a document its own reader (LoadActivation) would then
// reject -- e.g. an unknown host id.
func TestPersistActivation_RejectsInvalidActivation(t *testing.T) {
	stateDir := t.TempDir()
	invalid := domain.Activation{
		APIVersion: domain.SupportedAPIVersion,
		Kind:       "Activation",
		Metadata:   domain.ActivationMetadata{Worktree: "worktree:sha256:invalid-test"},
		Spec:       domain.ActivationSpec{Hosts: map[string]domain.HostActivation{"not-a-real-host": {}}},
	}
	if err := PersistActivation(stateDir, invalid); err == nil {
		t.Fatal("PersistActivation with an unknown host id: want an error, got nil")
	}
	if _, err := os.Stat(filepath.Join(stateDir, "desired", "activation.yaml")); err == nil {
		t.Error("activation.yaml was written despite a validation failure")
	}
}

// TestPersistActivation_RequiresWorktreeStateDir proves the same
// caller-must-supply-a-real-directory discipline PersistSelection already
// enforces.
func TestPersistActivation_RequiresWorktreeStateDir(t *testing.T) {
	err := PersistActivation("", domain.Activation{APIVersion: domain.SupportedAPIVersion, Kind: "Activation", Metadata: domain.ActivationMetadata{Worktree: "x"}})
	if err == nil {
		t.Fatal("PersistActivation with an empty worktreeStateDir: want an error, got nil")
	}
}

// TestPersistActivation_ConcurrentCalls_NoTempFileCollision is a regression
// test (Copilot review finding on this PR): PersistActivation previously
// built its temp file's name from the process PID alone
// (".tmp-<pid>"), which is identical across every call within this same
// process -- two goroutines racing PersistActivation could open/write/
// rename the SAME temp path, risking a lost update or a write error,
// exactly the case os.CreateTemp's own random-suffixed uniqueness (this
// fix) exists to rule out. Many goroutines call PersistActivation
// concurrently with distinct, individually valid Activation documents; none
// may error, and the final activation.yaml must be a complete, valid
// document matching exactly one caller's content (never truncated/
// interleaved bytes from two writers), proving no collision occurred.
func TestPersistActivation_ConcurrentCalls_NoTempFileCollision(t *testing.T) {
	stateDir := t.TempDir()
	const n = 20

	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			a := domain.Activation{
				APIVersion: domain.SupportedAPIVersion,
				Kind:       "Activation",
				Metadata:   domain.ActivationMetadata{Worktree: fmt.Sprintf("worktree:sha256:concurrent-%d", i)},
			}
			errs[i] = PersistActivation(stateDir, a)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("PersistActivation call %d: %v", i, err)
		}
	}

	final, ok, err := LoadActivation(activationPath(stateDir))
	if err != nil {
		t.Fatalf("LoadActivation after concurrent PersistActivation calls: %v (a temp-file collision would corrupt/truncate the final file, exactly what this failure would indicate)", err)
	}
	if !ok {
		t.Fatal("LoadActivation reports no file present after concurrent PersistActivation calls")
	}
	if !strings.HasPrefix(final.Metadata.Worktree, "worktree:sha256:concurrent-") {
		t.Errorf("final activation.yaml's Metadata.Worktree = %q, want one of the concurrent callers' own values", final.Metadata.Worktree)
	}

	// No leftover temp files: every os.CreateTemp-created file must have
	// been successfully renamed away or removed on error, never abandoned.
	entries, err := os.ReadDir(filepath.Join(stateDir, "desired"))
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if e.Name() != "activation.yaml" {
			t.Errorf("leftover file %q in desired/ after concurrent PersistActivation calls", e.Name())
		}
	}
}
