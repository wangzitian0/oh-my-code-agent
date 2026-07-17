package profiles

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/wangzitian0/oh-my-code-agent/internal/resolve"
)

// composeFixtureLayout builds a full, realistic on-disk layout under a
// fresh t.TempDir(): a fake ~/.config/omca (config) tree per docs/
// architecture/README.md §7's first layout block, a fake <repository>/.omca
// tree per its second block, and a fake worktree state tree per §8. It
// returns the directories/paths a CompositionInput needs.
type composeFixtureLayout struct {
	profileDirs      []string
	bindingDirs      []string
	exceptionDirs    []string
	activationPath   string
	worktreeStateDir string
}

func newComposeFixtureLayout(t *testing.T) composeFixtureLayout {
	t.Helper()
	root := t.TempDir()
	config := filepath.Join(root, "config", "omca")
	repo := filepath.Join(root, "repo", ".omca")
	state := filepath.Join(root, "state", "worktrees", "wt1")

	return composeFixtureLayout{
		profileDirs:      []string{filepath.Join(config, "profiles"), filepath.Join(repo, "profiles")},
		bindingDirs:      []string{filepath.Join(config, "bindings"), filepath.Join(repo, "bindings")},
		exceptionDirs:    []string{filepath.Join(config, "exceptions"), filepath.Join(repo, "exceptions")},
		activationPath:   filepath.Join(state, "desired", "activation.yaml"),
		worktreeStateDir: state,
	}
}

// TestCompose_RequirementsSection4GoldenScenario_EndToEnd is issue #16's
// composition entry point (Deliverable #5), driven all the way through to
// resolve.Resolve: a real on-disk config layout (personal + company
// Profiles selected by a real Binding document, plus a real Activation
// document) composes into exactly the []domain.Profile + domain.Activation
// set docs/product/requirements.md §4's golden scenario expects, and
// resolve.Resolve on that composed output reproduces the same golden
// outcome internal/resolve/golden_test.go already proves for hand-built
// inputs — proving Compose's output is actually consumable by the frozen
// resolver, not just structurally plausible.
func TestCompose_RequirementsSection4GoldenScenario_EndToEnd(t *testing.T) {
	fx := newComposeFixtureLayout(t)

	mustWriteFile(t, filepath.Join(fx.profileDirs[0], "personal", "alice.yaml"), `
apiVersion: omca.dev/v1alpha1
kind: Profile
metadata:
  id: personal:alice
spec:
  assets:
    skills:
      - id: personal-notes
        intent: AVAILABLE
`)
	mustWriteFile(t, filepath.Join(fx.profileDirs[0], "company", "example.yaml"), `
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
      - id: internal-docs
        intent: AVAILABLE
      - id: codegraph
        intent: DEFAULT
        hosts: [codex]
    instructions:
      - id: engineering-baseline
        intent: DEFAULT
  policy:
    permissions:
      sandbox:
        intent: DEFAULT
        value: workspace-write
`)
	mustWriteFile(t, filepath.Join(fx.bindingDirs[0], "order-service.yaml"), `
apiVersion: omca.dev/v1alpha1
kind: Binding
metadata:
  id: binding:order-service
spec:
  match:
    repository: github.com/example/order-service
    paths: ["**"]
  profiles:
    - personal:alice
    - company:example
`)
	mustWriteFile(t, fx.activationPath, activationYAML)

	input := CompositionInput{
		Repository:       "github.com/example/order-service",
		RelPath:          "internal/service",
		ProfileDirs:      fx.profileDirs,
		BindingDirs:      fx.bindingDirs,
		ExceptionDirs:    fx.exceptionDirs,
		ActivationPath:   fx.activationPath,
		WorktreeStateDir: fx.worktreeStateDir,
		Now:              time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}

	result, err := Compose(input)
	if err != nil {
		t.Fatalf("Compose: %v", err)
	}
	if len(result.Profiles) != 2 {
		t.Fatalf("Profiles = %+v, want 2 (personal:alice, company:example)", result.Profiles)
	}
	if result.Profiles[0].Metadata.ID != "personal:alice" || result.Profiles[1].Metadata.ID != "company:example" {
		t.Fatalf("Profiles order = [%s, %s], want [personal:alice, company:example] (broad-to-narrow)", result.Profiles[0].Metadata.ID, result.Profiles[1].Metadata.ID)
	}
	if result.Activation.Metadata.Worktree != "worktree:sha256:0123456789abcdef" {
		t.Fatalf("Activation not loaded correctly: %+v", result.Activation)
	}
	if len(result.Exceptions) != 0 || len(result.ExpiredExceptions) != 0 {
		t.Fatalf("expected no exceptions, got live=%v expired=%v", result.Exceptions, result.ExpiredExceptions)
	}

	// Feed straight into the real, frozen resolver -- this is the whole
	// point of Compose's output shape.
	claudeState, err := resolve.Resolve(result.Profiles, result.Activation, result.Exceptions, "claude-code", input.Now)
	if err != nil {
		t.Fatalf("resolve.Resolve(claude-code): %v", err)
	}
	if len(claudeState.Conflicts) != 0 {
		t.Fatalf("claude-code conflicts = %+v, want none", claudeState.Conflicts)
	}
	if !claudeState.IsActive(resolve.KindSkill, "deep-refactor") {
		t.Error("claude-code: deep-refactor should be active")
	}
	if !claudeState.IsActive(resolve.KindSkill, "ui-review") {
		t.Error("claude-code: ui-review should be active (host-scoped Activation entry)")
	}
	if claudeState.IsActive(resolve.KindMCPServer, "codegraph") {
		t.Error("claude-code: codegraph is codex-scoped and must NOT be active")
	}

	codexState, err := resolve.Resolve(result.Profiles, result.Activation, result.Exceptions, "codex", input.Now)
	if err != nil {
		t.Fatalf("resolve.Resolve(codex): %v", err)
	}
	if !codexState.IsActive(resolve.KindMCPServer, "codegraph") {
		t.Error("codex: codegraph should be active")
	}
	if codexState.IsActive(resolve.KindMCPServer, "internal-docs") {
		t.Error("codex: internal-docs is disabled by the host-scoped Activation entry and must NOT be active")
	}
}

// TestCompose_NoMatchingBinding_YieldsEmptyProfiles proves a repository
// with no matching Binding at all composes cleanly to zero Profiles rather
// than erroring -- an unmanaged repository is a normal state, not a
// failure.
func TestCompose_NoMatchingBinding_YieldsEmptyProfiles(t *testing.T) {
	fx := newComposeFixtureLayout(t)
	input := CompositionInput{
		Repository:       "github.com/example/unmanaged",
		ProfileDirs:      fx.profileDirs,
		BindingDirs:      fx.bindingDirs,
		ExceptionDirs:    fx.exceptionDirs,
		ActivationPath:   fx.activationPath,
		WorktreeStateDir: fx.worktreeStateDir,
		Now:              time.Now(),
	}
	result, err := Compose(input)
	if err != nil {
		t.Fatalf("Compose: %v", err)
	}
	if len(result.Profiles) != 0 {
		t.Errorf("Profiles = %+v, want none", result.Profiles)
	}
}

// TestCompose_AmbiguousIdentity_ThenPersistedSelectionResolves is issue
// #16's full round trip: two distinct company:* Profiles both match via two
// different Bindings scoped to the same repository/path, so Compose refuses
// to guess and returns *AmbiguousIdentityError; persisting an explicit
// selection and calling Compose again then succeeds with the chosen
// Profile.
func TestCompose_AmbiguousIdentity_ThenPersistedSelectionResolves(t *testing.T) {
	fx := newComposeFixtureLayout(t)

	mustWriteFile(t, filepath.Join(fx.profileDirs[0], "company", "example.yaml"), `
apiVersion: omca.dev/v1alpha1
kind: Profile
metadata:
  id: company:example
spec:
  assets:
    skills:
      - id: code-review
        intent: AVAILABLE
`)
	mustWriteFile(t, filepath.Join(fx.profileDirs[0], "company", "other-corp.yaml"), `
apiVersion: omca.dev/v1alpha1
kind: Profile
metadata:
  id: company:other-corp
spec:
  assets:
    skills:
      - id: code-review
        intent: DENIED
`)
	mustWriteFile(t, filepath.Join(fx.bindingDirs[0], "binding-a.yaml"), `
apiVersion: omca.dev/v1alpha1
kind: Binding
metadata:
  id: binding:a
spec:
  match:
    repository: github.com/example/shared
  profiles:
    - company:example
`)
	mustWriteFile(t, filepath.Join(fx.bindingDirs[0], "binding-b.yaml"), `
apiVersion: omca.dev/v1alpha1
kind: Binding
metadata:
  id: binding:b
spec:
  match:
    repository: github.com/example/shared
  profiles:
    - company:other-corp
`)

	input := CompositionInput{
		Repository:       "github.com/example/shared",
		ProfileDirs:      fx.profileDirs,
		BindingDirs:      fx.bindingDirs,
		ExceptionDirs:    fx.exceptionDirs,
		ActivationPath:   fx.activationPath,
		WorktreeStateDir: fx.worktreeStateDir,
		Now:              time.Now(),
	}

	_, err := Compose(input)
	if err == nil {
		t.Fatal("expected an error: two distinct company:* Profiles both match")
	}
	ambErr, ok := err.(*AmbiguousIdentityError)
	if !ok {
		t.Fatalf("expected *AmbiguousIdentityError, got %T: %v", err, err)
	}
	if len(ambErr.Ambiguous) != 1 || ambErr.Ambiguous[0].Category != "company" {
		t.Fatalf("Ambiguous = %+v, want exactly one ambiguous 'company' category", ambErr.Ambiguous)
	}

	if err := PersistSelection(fx.worktreeStateDir, "worktree:sha256:wt1", map[string]string{"company": "company:other-corp"}, time.Now()); err != nil {
		t.Fatalf("PersistSelection: %v", err)
	}

	result, err := Compose(input)
	if err != nil {
		t.Fatalf("Compose after PersistSelection: %v", err)
	}
	if len(result.Profiles) != 1 || result.Profiles[0].Metadata.ID != "company:other-corp" {
		t.Fatalf("Profiles = %+v, want exactly [company:other-corp]", result.Profiles)
	}
}

// TestCompose_ExceptionExpiry_LiveOnlyReachesResolve proves the composed
// Exceptions slice (fed to resolve.Resolve) excludes an expired Exception,
// while CompositionResult.ExpiredExceptions still reports it -- issue #16's
// round-2 AC end to end, including the real resolver: without the live
// exception, a DENIED asset a caller tries to enable via Activation stays
// denied; with it (a second, otherwise-identical case), it is excepted.
func TestCompose_ExceptionExpiry_LiveOnlyReachesResolve(t *testing.T) {
	fx := newComposeFixtureLayout(t)

	mustWriteFile(t, filepath.Join(fx.profileDirs[0], "company", "policy.yaml"), `
apiVersion: omca.dev/v1alpha1
kind: Profile
metadata:
  id: company:policy
spec:
  assets:
    mcpServers:
      - id: banned-tool
        intent: DENIED
`)
	mustWriteFile(t, filepath.Join(fx.bindingDirs[0], "binding.yaml"), `
apiVersion: omca.dev/v1alpha1
kind: Binding
metadata:
  id: binding:x
spec:
  match:
    repository: github.com/example/x
  profiles:
    - company:policy
`)
	mustWriteFile(t, fx.activationPath, `
apiVersion: omca.dev/v1alpha1
kind: Activation
metadata:
  worktree: worktree:sha256:x
spec:
  enable:
    mcpServers: [banned-tool]
`)
	mustWriteFile(t, filepath.Join(fx.exceptionDirs[0], "expired.yaml"), fmtException(
		"exception:expired", "banned-tool", "company:policy",
		"allowance that has since lapsed", "2025-01-01T00:00:00Z",
	))

	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	input := CompositionInput{
		Repository:       "github.com/example/x",
		ProfileDirs:      fx.profileDirs,
		BindingDirs:      fx.bindingDirs,
		ExceptionDirs:    fx.exceptionDirs,
		ActivationPath:   fx.activationPath,
		WorktreeStateDir: fx.worktreeStateDir,
		Now:              now,
	}

	result, err := Compose(input)
	if err != nil {
		t.Fatalf("Compose: %v", err)
	}
	if len(result.Exceptions) != 0 {
		t.Fatalf("Exceptions (live) = %+v, want none: the only exception on disk is expired", result.Exceptions)
	}
	if len(result.ExpiredExceptions) != 1 || result.ExpiredExceptions[0].Metadata.ID != "exception:expired" {
		t.Fatalf("ExpiredExceptions = %+v, want exactly [exception:expired]", result.ExpiredExceptions)
	}

	state, err := resolve.Resolve(result.Profiles, result.Activation, result.Exceptions, "claude-code", now)
	if err != nil {
		t.Fatalf("resolve.Resolve: %v", err)
	}
	if state.IsActive(resolve.KindMCPServer, "banned-tool") {
		t.Error("banned-tool must stay DENIED: the only exception on disk is expired and must not reach the resolver")
	}

	// Now add a live exception (same asset/scope, unexpired) alongside the
	// expired one, and prove it DOES except the DENIED asset.
	mustWriteFile(t, filepath.Join(fx.exceptionDirs[0], "live.yaml"), fmtException(
		"exception:live", "banned-tool", "company:policy",
		"temporary security-reviewed allowance", "2026-06-01T00:00:00Z",
	))
	result2, err := Compose(input)
	if err != nil {
		t.Fatalf("Compose (with live exception): %v", err)
	}
	if len(result2.Exceptions) != 1 || len(result2.ExpiredExceptions) != 1 {
		t.Fatalf("expected one live and one expired exception, got live=%v expired=%v", result2.Exceptions, result2.ExpiredExceptions)
	}
	state2, err := resolve.Resolve(result2.Profiles, result2.Activation, result2.Exceptions, "claude-code", now)
	if err != nil {
		t.Fatalf("resolve.Resolve: %v", err)
	}
	if !state2.IsActive(resolve.KindMCPServer, "banned-tool") {
		t.Error("banned-tool should be active: a live, scope-matching exception now excepts the DENIED profile intent")
	}
}
