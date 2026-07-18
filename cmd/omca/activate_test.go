package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	hostcontext "github.com/wangzitian0/oh-my-code-agent/internal/context"
	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
	"github.com/wangzitian0/oh-my-code-agent/internal/runtime"
)

// mustWriteFileForActivateTest writes content to path, creating parent
// directories as needed -- this file's own tiny fixture helper (this
// package has no shared mustWriteFile already; testenv_test.go's helpers
// are named differently).
func mustWriteFileForActivateTest(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mustWriteFileForActivateTest: mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("mustWriteFileForActivateTest: write %s: %v", path, err)
	}
}

// mcpServerProfileYAML is a minimal Profile document (docs/product/
// requirements.md §4.1's own worked shape) REQUIRING one mcpServer for
// codex -- enough to make DiffProposedChanges/RequireConfirmation actually
// gate on something real when compiled.
const mcpServerProfileYAML = `
apiVersion: omca.dev/v1alpha1
kind: Profile
metadata:
  id: company:example
spec:
  assets:
    mcpServers:
      - id: internal-docs
        intent: REQUIRED
`

// buildPendingFixtureForActivate writes profileYAML into the real (test-
// redirected) $HOME/.config/omca/profiles tree, then compiles and stages it
// as codex's pending generation via EXACTLY the same
// composeFreshCompileRequest helper production activation uses -- so this
// fixture's pending generation and a real `omca activate` call's
// independently-recomputed Fresh request are guaranteed to agree, the same
// way a real caller's compile-then-activate sequence would. Every call
// (re)writes the same file path (profiles/company/example.yaml), so a
// caller that wants two genuinely different fixtures in sequence must pass
// different profileYAML content each time -- passing the SAME content twice
// would compile to the identical content-addressed generation directory,
// which the second Compile call would then fail to write into (already
// read-only from the first).
func buildPendingFixtureForActivate(t *testing.T, env managedTestEnv, profileYAML string, parent *string, now time.Time) (worktreeStateDir string, gen domain.Generation, outputDir string) {
	t.Helper()
	profileDir := filepath.Join(env.HomeDir, ".config", "omca", "profiles", "company")
	mustWriteFileForActivateTest(t, filepath.Join(profileDir, "example.yaml"), profileYAML)

	wt, err := hostcontext.DetectWorktree(env.WorktreeRoot)
	if err != nil {
		t.Fatalf("DetectWorktree: %v", err)
	}

	// A Binding is what actually selects a Profile for this repository
	// (internal/profiles.MatchBindings) -- without one, Compose resolves
	// zero Profiles regardless of what exists under profiles/, and this
	// fixture's Profile above would never reach the compiled generation at
	// all. composeFreshCompileRequest passes wt.Root as
	// CompositionInput.Repository, so this Binding's own match.repository
	// must equal wt.Root exactly (a real absolute filesystem path in this
	// test fixture, standing in for docs/product/requirements.md §4.2's
	// logical "github.com/example/order-service"-style repository name a
	// production Binding would actually use).
	bindingDir := filepath.Join(env.HomeDir, ".config", "omca", "bindings")
	mustWriteFileForActivateTest(t, filepath.Join(bindingDir, "example.yaml"), "apiVersion: omca.dev/v1alpha1\nkind: Binding\nmetadata:\n  id: binding:example\nspec:\n  match:\n    repository: "+wt.Root+"\n    paths: [\"**\"]\n  profiles:\n    - company:example\n")

	stateRoot, err := realStateRoot()
	if err != nil {
		t.Fatalf("realStateRoot: %v", err)
	}
	worktreeStateDir = worktreeStateDirPath(stateRoot, wt.ID)
	shimDir := shimDirPath(worktreeStateDir)
	if err := installShims(shimDir); err != nil {
		t.Fatalf("installShims: %v", err)
	}

	placeholder := domain.Generation{Spec: domain.GenerationSpec{Hosts: map[string]domain.GenerationHostEntry{"codex": {}}}}
	fresh, err := composeFreshCompileRequest(wt, placeholder, worktreeStateDir, shimDir, now)
	if err != nil {
		t.Fatalf("composeFreshCompileRequest: %v", err)
	}
	fresh.Parent = parent

	genID, err := runtime.CompileGenerationID(fresh)
	if err != nil {
		t.Fatalf("CompileGenerationID: %v", err)
	}
	outputDir = filepath.Join(worktreeStateDir, "generations", runtime.DirSafeID(genID))
	gen, err = runtime.Compile(fresh, outputDir)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	restoreWritableTree(t, outputDir)

	if err := runtime.SetPendingGeneration(worktreeStateDir, "codex", outputDir, gen, fresh.Hosts[0].Detection, now); err != nil {
		t.Fatalf("SetPendingGeneration: %v", err)
	}
	return worktreeStateDir, gen, outputDir
}

// TestRunActivate_RequiresConfirmation_BlocksWithoutFlag proves the round-2
// addendum's core CLI claim: activating a pending generation that newly
// enables an MCP server is BLOCKED without an explicit --confirm, and the
// blocked message names the confirmation class.
func TestRunActivate_RequiresConfirmation_BlocksWithoutFlag(t *testing.T) {
	env := setupManagedTestEnv(t, true, false)
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	buildPendingFixtureForActivate(t, env, mcpServerProfileYAML, nil, now)

	var stdout, stderr bytes.Buffer
	code := runActivate(&stdout, &stderr, []string{"codex"})
	if code != 1 {
		t.Fatalf("runActivate without --confirm = %d, want 1; stderr:\n%s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "confirm-with-detail") || !strings.Contains(stderr.String(), "internal-docs") {
		t.Errorf("stderr does not name the confirm-with-detail class and the affected MCP server:\n%s", stderr.String())
	}

	if _, err := runtime.CurrentGenerationDir(worktreeStateDirForTest(t, env), "codex"); err == nil {
		t.Error("current generation was set despite a blocked activation")
	}
}

// TestRunActivate_WithConfirmation_Succeeds is the positive control: the
// same pending generation, now with --confirm enable-mcp-server, actually
// activates.
func TestRunActivate_WithConfirmation_Succeeds(t *testing.T) {
	env := setupManagedTestEnv(t, true, false)
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	worktreeStateDir, gen, outputDir := buildPendingFixtureForActivate(t, env, mcpServerProfileYAML, nil, now)

	var stdout, stderr bytes.Buffer
	code := runActivate(&stdout, &stderr, []string{"codex", "--confirm", "enable-mcp-server:internal-docs"})
	if code != 0 {
		t.Fatalf("runActivate with --confirm = %d, want 0; stderr:\n%s", code, stderr.String())
	}

	gotCurrent, err := runtime.CurrentGenerationDir(worktreeStateDir, "codex")
	if err != nil {
		t.Fatalf("CurrentGenerationDir: %v", err)
	}
	if gotCurrent != outputDir {
		t.Errorf("CurrentGenerationDir = %q, want the activated pending generation %q", gotCurrent, outputDir)
	}

	entries, err := runtime.ReadLedger(worktreeStateDir, "codex")
	if err != nil {
		t.Fatalf("ReadLedger: %v", err)
	}
	found := false
	for _, e := range entries {
		if e.Kind == "activated" && e.GenerationID == gen.Metadata.ID {
			found = true
		}
	}
	if !found {
		t.Errorf("ledger has no 'activated' entry: %+v", entries)
	}
}

// TestRunRollback_RestoresParent activates two generations in sequence via
// `omca activate`, then rolls back via `omca rollback` and proves the FIRST
// generation is restored as current.
func TestRunRollback_RestoresParent(t *testing.T) {
	env := setupManagedTestEnv(t, true, false)
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)

	worktreeStateDir, firstGen, firstDir := buildPendingFixtureForActivate(t, env, mcpServerProfileYAML, nil, now)
	var stdout, stderr bytes.Buffer
	if code := runActivate(&stdout, &stderr, []string{"codex", "--confirm", "enable-mcp-server:internal-docs"}); code != 0 {
		t.Fatalf("runActivate (first): %d; stderr:\n%s", code, stderr.String())
	}

	// A second Profile edit (removing the mcpServer, adding a REQUIRED
	// skill instead) simulates real desired-state evolution between
	// activations.
	const skillProfileYAML = `
apiVersion: omca.dev/v1alpha1
kind: Profile
metadata:
  id: company:example
spec:
  assets:
    skills:
      - id: code-review
        intent: REQUIRED
`
	parent := firstGen.Metadata.ID
	_, secondGen, secondDir := buildPendingFixtureForActivate(t, env, skillProfileYAML, &parent, now.Add(time.Minute))
	if secondGen.Metadata.ID == firstGen.Metadata.ID {
		t.Fatal("fixture setup did not actually vary content between the two generations")
	}

	stdout.Reset()
	stderr.Reset()
	if code := runActivate(&stdout, &stderr, []string{"codex", "--confirm", "select-reviewed-skill:code-review"}); code != 0 {
		t.Fatalf("runActivate (second): %d; stderr:\n%s", code, stderr.String())
	}
	gotCurrent, err := runtime.CurrentGenerationDir(worktreeStateDir, "codex")
	if err != nil {
		t.Fatalf("CurrentGenerationDir before rollback: %v", err)
	}
	if gotCurrent != secondDir {
		t.Fatalf("CurrentGenerationDir before rollback = %q, want the second generation %q", gotCurrent, secondDir)
	}

	stdout.Reset()
	stderr.Reset()
	if code := runRollback(&stdout, &stderr, []string{"codex"}); code != 0 {
		t.Fatalf("runRollback = %d, want 0; stderr:\n%s", code, stderr.String())
	}

	gotCurrentAfter, err := runtime.CurrentGenerationDir(worktreeStateDir, "codex")
	if err != nil {
		t.Fatalf("CurrentGenerationDir after rollback: %v", err)
	}
	if gotCurrentAfter != firstDir {
		t.Errorf("CurrentGenerationDir after rollback = %q, want the first (parent) generation %q", gotCurrentAfter, firstDir)
	}
}

// TestRunActivate_FailedVerification_AutomaticallyRollsBackAndLedgersBoth is
// this PR's own CLI-level proof of issue #28's headline AC through the real
// `omca activate` command: activate a first (parent) generation
// successfully, then tamper with a second (child) generation's compiled
// artifact tree before activating it -- simulating a generation whose
// on-disk output diverged from its own manifest -- and prove `omca
// activate` (a) exits non-zero (the requested activation did not stick),
// (b) leaves "current" restored to the parent, not the broken child, and
// (c) the Ledger records both the verification failure and the automated
// restoration.
func TestRunActivate_FailedVerification_AutomaticallyRollsBackAndLedgersBoth(t *testing.T) {
	env := setupManagedTestEnv(t, true, false)
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)

	worktreeStateDir, firstGen, firstDir := buildPendingFixtureForActivate(t, env, mcpServerProfileYAML, nil, now)
	var stdout, stderr bytes.Buffer
	if code := runActivate(&stdout, &stderr, []string{"codex", "--confirm", "enable-mcp-server:internal-docs"}); code != 0 {
		t.Fatalf("runActivate (first): %d; stderr:\n%s", code, stderr.String())
	}

	const skillProfileYAML = `
apiVersion: omca.dev/v1alpha1
kind: Profile
metadata:
  id: company:example
spec:
  assets:
    skills:
      - id: code-review
        intent: REQUIRED
`
	parent := firstGen.Metadata.ID
	_, secondGen, secondDir := buildPendingFixtureForActivate(t, env, skillProfileYAML, &parent, now.Add(time.Minute))
	if secondGen.Metadata.ID == firstGen.Metadata.ID {
		t.Fatal("fixture setup did not actually vary content between the two generations")
	}

	// Tamper the second (about-to-be-activated) generation's compiled
	// config.toml -- the same always-present artifact
	// internal/runtime/verify_test.go's tamperArtifact targets -- BEFORE
	// activating it, simulating a generation whose on-disk output no
	// longer matches its own manifest at the moment it becomes current.
	tamperedPath := filepath.Join(secondDir, "hosts", "codex", "cli", "codex-home", "config.toml")
	if err := os.Chmod(tamperedPath, 0o644); err != nil {
		t.Fatalf("chmod tampered artifact: %v", err)
	}
	if err := os.WriteFile(tamperedPath, []byte("tampered, does not match the manifest\n"), 0o644); err != nil {
		t.Fatalf("writing tampered artifact: %v", err)
	}

	stdout.Reset()
	stderr.Reset()
	code := runActivate(&stdout, &stderr, []string{"codex", "--confirm", "select-reviewed-skill:code-review"})
	if code != 1 {
		t.Fatalf("runActivate (second, tampered) = %d, want 1; stdout:\n%s\nstderr:\n%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "verification FAILED") || !strings.Contains(stderr.String(), "rolled back") {
		t.Errorf("stderr does not explain the failed verification / automated rollback:\n%s", stderr.String())
	}

	gotCurrent, err := runtime.CurrentGenerationDir(worktreeStateDir, "codex")
	if err != nil {
		t.Fatalf("CurrentGenerationDir: %v", err)
	}
	if gotCurrent != firstDir {
		t.Errorf("CurrentGenerationDir after a failed-verification activation = %q, want the parent %q (never the broken child %q)", gotCurrent, firstDir, secondDir)
	}

	entries, err := runtime.ReadLedger(worktreeStateDir, "codex")
	if err != nil {
		t.Fatalf("ReadLedger: %v", err)
	}
	var verifyFailedIdx, rolledBackIdx = -1, -1
	for i, e := range entries {
		if e.Kind == "verification-failed" && e.GenerationID == secondGen.Metadata.ID {
			verifyFailedIdx = i
		}
		if e.Kind == "rolledback" && e.GenerationID == firstGen.Metadata.ID {
			rolledBackIdx = i
		}
	}
	if verifyFailedIdx == -1 {
		t.Errorf("ledger has no 'verification-failed' entry for the tampered generation %s: %+v", secondGen.Metadata.ID, entries)
	}
	if rolledBackIdx == -1 {
		t.Errorf("ledger has no 'rolledback' entry restoring the parent %s: %+v", firstGen.Metadata.ID, entries)
	}
	if verifyFailedIdx != -1 && rolledBackIdx != -1 && verifyFailedIdx > rolledBackIdx {
		t.Errorf("'verification-failed' (index %d) must be ledgered before 'rolledback' (index %d)", verifyFailedIdx, rolledBackIdx)
	}
}

// worktreeStateDirForTest re-derives worktreeStateDir the same way
// buildPendingFixtureForActivate did, for a test that needs it after
// runActivate has already been called (rather than plumbing it back out of
// buildPendingFixtureForActivate's own multiple return values every time).
func worktreeStateDirForTest(t *testing.T, env managedTestEnv) string {
	t.Helper()
	wt, err := hostcontext.DetectWorktree(env.WorktreeRoot)
	if err != nil {
		t.Fatalf("DetectWorktree: %v", err)
	}
	stateRoot, err := realStateRoot()
	if err != nil {
		t.Fatalf("realStateRoot: %v", err)
	}
	return worktreeStateDirPath(stateRoot, wt.ID)
}
