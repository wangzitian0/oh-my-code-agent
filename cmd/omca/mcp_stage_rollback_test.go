package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	hostcontext "github.com/wangzitian0/oh-my-code-agent/internal/context"
	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
	"github.com/wangzitian0/oh-my-code-agent/internal/runtime"
)

// stageViaCompileFuncForMCP stages a pending generation for exactly the
// hosts named in hostIDs through the REAL omca_stage production code path
// (compileFuncForMCP, mcp.go) -- unlike buildPendingFixtureForActivate
// (activate_test.go), which hand-composes/compiles/stages via
// composeFreshCompileRequest directly and lets its caller thread
// Metadata.Parent by hand, this helper drives compileFuncForMCP itself, the
// exact function issue #68 fixed, so Parent comes out of that function's own
// currentByHost-agreement logic (mcp.go's compileFuncForMCP doc comment)
// exactly the way a real `omca_stage` tools/call would produce it -- never
// injected by the test. profileYAML varies the desired state, mirroring
// buildPendingFixtureForActivate's own doc comment: two sequential calls
// with different content compile two distinct, content-addressed
// generations.
func stageViaCompileFuncForMCP(t *testing.T, env managedTestEnv, profileYAML string, hostIDs ...string) (worktreeStateDir string, gen domain.Generation, outputDir string) {
	t.Helper()
	profileDir := filepath.Join(env.HomeDir, ".config", "omca", "profiles", "company")
	mustWriteFileForActivateTest(t, filepath.Join(profileDir, "example.yaml"), profileYAML)

	wt, err := hostcontext.DetectWorktree(env.WorktreeRoot)
	if err != nil {
		t.Fatalf("DetectWorktree: %v", err)
	}
	// See buildPendingFixtureForActivate's identical Binding fixture for why
	// this is required: without it, Compose resolves zero Profiles
	// regardless of what exists under profiles/.
	bindingDir := filepath.Join(env.HomeDir, ".config", "omca", "bindings")
	mustWriteFileForActivateTest(t, filepath.Join(bindingDir, "example.yaml"), "apiVersion: omca.dev/v1alpha1\nkind: Binding\nmetadata:\n  id: binding:example\nspec:\n  match:\n    repository: "+wt.Root+"\n    paths: [\"**\"]\n  profiles:\n    - company:example\n")

	activations := make(map[string]domain.HostActivation, len(hostIDs))
	for _, h := range hostIDs {
		activations[h] = domain.HostActivation{}
	}
	compileFn := compileFuncForMCP(&bytes.Buffer{})
	gen, _, err = compileFn(activations)
	if err != nil {
		t.Fatalf("compileFuncForMCP: %v", err)
	}

	stateRoot, err := realStateRoot()
	if err != nil {
		t.Fatalf("realStateRoot: %v", err)
	}
	worktreeStateDir = worktreeStateDirPath(stateRoot, wt.ID)
	outputDir = filepath.Join(worktreeStateDir, "generations", runtime.DirSafeID(gen.Metadata.ID))
	return worktreeStateDir, gen, outputDir
}

// TestOmcaStage_Activate_Rollback_EndToEnd_ThroughRealStagingPath is issue
// #68's own regression test: a real `omca_stage` -> `omca activate` ->
// `omca rollback` sequence, staged through compileFuncForMCP itself --
// never hand-threading Metadata.Parent the way rollback_test.go/
// verify_test.go/this package's own buildPendingFixtureForActivate helper
// all do (issue #68's own root-cause finding: those tests pass only because
// they thread Parent themselves, which is exactly why they never caught
// this gap) -- succeeds end-to-end.
//
// Before issue #68's fix, compileFuncForMCP never set Parent at all, so the
// second `runRollback` call below failed with "... has no parent generation
// recorded; nothing to roll back to" against a generation staged the normal
// way -- exactly the failure the issue's real-environment walkthrough
// reproduced on a real machine (`omca rollback codex`/`omca rollback
// claude` against a generation staged through the identical
// runtime.Compile/runtime.SetPendingGeneration calls omca_stage makes).
func TestOmcaStage_Activate_Rollback_EndToEnd_ThroughRealStagingPath(t *testing.T) {
	env := setupManagedTestEnv(t, true, false)

	worktreeStateDir, firstGen, firstDir := stageViaCompileFuncForMCP(t, env, mcpServerProfileYAML, "codex")
	if firstGen.Metadata.Parent != nil {
		t.Fatalf("first staged generation's Metadata.Parent = %v, want nil (codex has no current generation yet -- first-ever activation)", *firstGen.Metadata.Parent)
	}

	var stdout, stderr bytes.Buffer
	if code := runActivate(&stdout, &stderr, []string{"codex", "--confirm", "enable-mcp-server:internal-docs"}); code != 0 {
		t.Fatalf("runActivate (first): %d; stderr:\n%s", code, stderr.String())
	}

	// A second Profile edit (removing the mcpServer, adding a REQUIRED
	// skill instead) simulates real desired-state evolution between
	// activations, exactly like TestRunRollback_RestoresParent's identical
	// fixture (activate_test.go).
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
	_, secondGen, secondDir := stageViaCompileFuncForMCP(t, env, skillProfileYAML, "codex")
	if secondGen.Metadata.ID == firstGen.Metadata.ID {
		t.Fatal("fixture setup did not actually vary content between the two staged generations")
	}
	// This is issue #68's core assertion: compileFuncForMCP itself must have
	// read codex's own current generation (the first, now-activated
	// generation) and threaded its ID into the second staged generation's
	// Metadata.Parent -- never hand-set by this test.
	if secondGen.Metadata.Parent == nil || *secondGen.Metadata.Parent != firstGen.Metadata.ID {
		got := "nil"
		if secondGen.Metadata.Parent != nil {
			got = *secondGen.Metadata.Parent
		}
		t.Fatalf("second staged generation's Metadata.Parent = %s, want the first generation's ID %q -- compileFuncForMCP did not thread Parent from codex's own current generation", got, firstGen.Metadata.ID)
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
	// This is the exact command (`omca rollback codex`) that failed on the
	// maintainer's real machine before issue #68's fix.
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

// TestCompileFuncForMCP_MultiHostDivergedParents_LeavesParentNil proves the
// other half of issue #68's reviewed decision (compileFuncForMCP's own doc
// comment, mcp.go): when two named hosts' own current generations
// genuinely differ, compileFuncForMCP leaves Metadata.Parent nil rather
// than guessing which one is "the" parent -- the same honest "nothing to
// roll back to" state internal/runtime/rollback.go's Rollback already
// refuses on, not a regression.
func TestCompileFuncForMCP_MultiHostDivergedParents_LeavesParentNil(t *testing.T) {
	env := setupManagedTestEnv(t, true, true)

	// Stage and activate codex alone against one desired state.
	_, codexGen, _ := stageViaCompileFuncForMCP(t, env, mcpServerProfileYAML, "codex")
	var stdout, stderr bytes.Buffer
	if code := runActivate(&stdout, &stderr, []string{"codex", "--confirm", "enable-mcp-server:internal-docs"}); code != 0 {
		t.Fatalf("runActivate (codex): %d; stderr:\n%s", code, stderr.String())
	}

	// Stage and activate claude-code alone against a DIFFERENT desired
	// state, so its current generation ends up a genuinely different ID
	// than codex's.
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
	_, claudeGen, _ := stageViaCompileFuncForMCP(t, env, skillProfileYAML, "claude-code")
	if claudeGen.Metadata.ID == codexGen.Metadata.ID {
		t.Fatal("fixture setup did not actually vary content between codex's and claude-code's generations")
	}
	stdout.Reset()
	stderr.Reset()
	if code := runActivate(&stdout, &stderr, []string{"claude", "--confirm", "select-reviewed-skill:code-review"}); code != 0 {
		t.Fatalf("runActivate (claude-code): %d; stderr:\n%s", code, stderr.String())
	}

	// Now stage a THIRD generation naming BOTH hosts at once: codex's and
	// claude-code's current generations genuinely disagree (asserted
	// above), so there is no single correct Parent -- it must stay nil.
	const thirdProfileYAML = `
apiVersion: omca.dev/v1alpha1
kind: Profile
metadata:
  id: company:example
spec:
  assets:
    skills:
      - id: another-skill
        intent: REQUIRED
`
	_, thirdGen, _ := stageViaCompileFuncForMCP(t, env, thirdProfileYAML, "codex", "claude-code")
	if thirdGen.Metadata.Parent != nil {
		t.Errorf("Metadata.Parent = %q, want nil (codex and claude-code have genuinely different current generations, so there is no single correct parent)", *thirdGen.Metadata.Parent)
	}
}

// TestCompileFuncForMCP_CorruptCurrentManifest_FailsLoudly is a regression
// test (Copilot review finding on this PR): before this fix, currentByHost
// silently ignored ANY error from CurrentGenerationDir/ReadGenerationManifest,
// including a real "the pointer exists but its target manifest is
// unreadable" failure -- indistinguishable, from compileFuncForMCP's own
// point of view, from "this host has never been activated" (the genuinely
// benign os.IsNotExist case). Treating the two the same silently derives
// Parent as if codex had no current generation at all, exactly the kind of
// "recompile/skip on any error, not just ENOENT" bug class this project has
// already fixed at three other call sites this session.
func TestCompileFuncForMCP_CorruptCurrentManifest_FailsLoudly(t *testing.T) {
	env := setupManagedTestEnv(t, true, false)

	worktreeStateDir, _, firstDir := stageViaCompileFuncForMCP(t, env, mcpServerProfileYAML, "codex")
	var stdout, stderr bytes.Buffer
	if code := runActivate(&stdout, &stderr, []string{"codex", "--confirm", "enable-mcp-server:internal-docs"}); code != 0 {
		t.Fatalf("runActivate: %d; stderr:\n%s", code, stderr.String())
	}

	// codex's "current" pointer now resolves to firstDir, but the whole
	// generation tree activation just wrote (readonly.go's makeTreeReadOnly)
	// is read-only -- corrupt manifest.json in place, exactly like this
	// package's own existing corrupt-manifest regression tests elsewhere in
	// this codebase (e.g. activate_test.go's tampering pattern).
	manifestPath := filepath.Join(firstDir, "manifest.json")
	if err := os.Chmod(manifestPath, 0o644); err != nil {
		t.Fatalf("chmod manifest writable: %v", err)
	}
	if err := os.WriteFile(manifestPath, []byte("not valid json"), 0o644); err != nil {
		t.Fatalf("corrupting manifest.json: %v", err)
	}
	_ = worktreeStateDir

	// Write a DIFFERENT desired state before calling compileFn directly
	// (not via stageViaCompileFuncForMCP's own profile-write, to keep this
	// test's raw compileFn call in full control) -- this call's own
	// content-addressed outputDir must differ from the corrupted firstDir,
	// so the only way its own corruption can surface is through
	// currentByHost's lookup of codex's current generation, never through
	// the unrelated, pre-existing "cache-hit manifest is corrupt" check a
	// few lines later in compileFuncForMCP (which this test would otherwise
	// accidentally also trip, since re-staging IDENTICAL content would reuse
	// firstDir as its own outputDir too, confounding which check actually
	// produced the error).
	const differentProfileYAML = `
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
	profileDir := filepath.Join(env.HomeDir, ".config", "omca", "profiles", "company")
	mustWriteFileForActivateTest(t, filepath.Join(profileDir, "example.yaml"), differentProfileYAML)

	compileFn := compileFuncForMCP(&bytes.Buffer{})
	_, _, err := compileFn(map[string]domain.HostActivation{"codex": {}})
	if err == nil {
		t.Fatal("compileFuncForMCP against a host whose current-generation manifest is corrupt: want an error, got nil")
	}
	if !strings.Contains(err.Error(), "codex") || !strings.Contains(err.Error(), "unreadable") {
		t.Errorf("compileFuncForMCP error does not clearly name the host and explain the unreadable manifest: %v", err)
	}
}

// TestCompileFuncForMCP_CacheHit_ReconcilesStaleParent is a regression test
// (Copilot review finding on this PR): a generation's content-addressed ID
// deliberately excludes Metadata.Parent (generationid.go's own doc
// comment), so re-staging IDENTICAL desired state after codex's "current"
// generation has moved on hits the SAME on-disk generation directory
// (outputDir) a second time -- a cache hit. Before this fix, the cache-hit
// branch returned that directory's manifest completely unchanged, so its
// Metadata.Parent still named whichever generation was current the FIRST
// time this exact content was compiled, not codex's actual, freshly
// current generation. A later Rollback trusts manifest.Parent as its only
// source of truth, so an uncorrected stale Parent would make it restore an
// unexpected, older generation.
func TestCompileFuncForMCP_CacheHit_ReconcilesStaleParent(t *testing.T) {
	env := setupManagedTestEnv(t, true, false)

	// 1. Stage and activate content X (mcpServerProfileYAML) -- codex's
	// first-ever generation, Parent nil.
	_, genX1, _ := stageViaCompileFuncForMCP(t, env, mcpServerProfileYAML, "codex")
	var stdout, stderr bytes.Buffer
	if code := runActivate(&stdout, &stderr, []string{"codex", "--confirm", "enable-mcp-server:internal-docs"}); code != 0 {
		t.Fatalf("runActivate (X): %d; stderr:\n%s", code, stderr.String())
	}

	// 2. Stage and activate different content Y -- codex's current moves on
	// to genY, whose own Parent correctly names genX1.
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
	_, genY, _ := stageViaCompileFuncForMCP(t, env, skillProfileYAML, "codex")
	stdout.Reset()
	stderr.Reset()
	if code := runActivate(&stdout, &stderr, []string{"codex", "--confirm", "select-reviewed-skill:code-review"}); code != 0 {
		t.Fatalf("runActivate (Y): %d; stderr:\n%s", code, stderr.String())
	}

	// 3. Re-stage the ORIGINAL content X again -- identical bytes, so this
	// hits the exact same outputDir genX1 already compiled into (a cache
	// hit, not a fresh compile). codex's current generation is now genY, so
	// this call's freshly-computed Parent is genY.Metadata.ID -- the fix
	// under test is that the returned (and on-disk) manifest reflects that,
	// not genX1's original (nil) Parent.
	_, genX2, outputDirX := stageViaCompileFuncForMCP(t, env, mcpServerProfileYAML, "codex")
	if genX2.Metadata.ID != genX1.Metadata.ID {
		t.Fatalf("fixture setup did not actually produce a cache hit: genX1.ID=%s genX2.ID=%s", genX1.Metadata.ID, genX2.Metadata.ID)
	}
	if genX2.Metadata.Parent == nil || *genX2.Metadata.Parent != genY.Metadata.ID {
		got := "nil"
		if genX2.Metadata.Parent != nil {
			got = *genX2.Metadata.Parent
		}
		t.Errorf("cache-hit re-stage's returned Metadata.Parent = %s, want the freshly-current generation %q (not the stale value from when this content was first compiled)", got, genY.Metadata.ID)
	}

	// The on-disk manifest must agree with the returned value -- a later,
	// independent ReadGenerationManifest (e.g. Rollback's own read) must
	// see the same reconciled Parent, not just this call's in-memory return.
	onDisk, err := runtime.ReadGenerationManifest(outputDirX)
	if err != nil {
		t.Fatalf("ReadGenerationManifest after cache-hit reconciliation: %v", err)
	}
	if onDisk.Metadata.Parent == nil || *onDisk.Metadata.Parent != genY.Metadata.ID {
		got := "nil"
		if onDisk.Metadata.Parent != nil {
			got = *onDisk.Metadata.Parent
		}
		t.Errorf("on-disk manifest.json Metadata.Parent after cache-hit reconciliation = %s, want %q", got, genY.Metadata.ID)
	}
}

// availableMCPServerProfileYAML declares "internal-docs" as AVAILABLE, not
// REQUIRED: it is not included by default, exactly the "select an AVAILABLE
// asset" shape issue #35's own TUI action layer (and this test's real
// omca_stage caller below) needs something real to activate.
const availableMCPServerProfileYAML = `
apiVersion: omca.dev/v1alpha1
kind: Profile
metadata:
  id: company:example
spec:
  assets:
    mcpServers:
      - id: internal-docs
        intent: AVAILABLE
`

// TestOmcaStage_EnableSelection_PersistsSoLaterActivateSeesIt is a
// regression test for a real, pre-existing production gap surfaced (and
// fixed at its root, profiles.PersistActivation) while building issue #35's
// TUI action layer: compileFuncForMCP (the real omca_stage production path)
// merged a caller-supplied Enable/Disable selection into mergedActivation
// only in memory, never durably writing it to activation.yaml. A later,
// independent `omca activate` call recomposes desired state fresh from that
// exact file (composeFreshCompileRequest) for its own CAS check -- with the
// selection never persisted, that fresh recomposition disagreed with what
// omca_stage actually staged, and Activate rejected the pending generation.
//
// This test drives the real omca_stage -> omca activate sequence: stage
// codex's pending generation through compileFuncForMCP with a genuine
// Enable.MCPServers selection (never hand-injected into activation.yaml by
// the test), then activate it. Before the persistence fix, this failed;
// after it, the freshly recomposed desired state agrees and activation
// succeeds.
func TestOmcaStage_EnableSelection_PersistsSoLaterActivateSeesIt(t *testing.T) {
	env := setupManagedTestEnv(t, true, false)
	profileDir := filepath.Join(env.HomeDir, ".config", "omca", "profiles", "company")
	mustWriteFileForActivateTest(t, filepath.Join(profileDir, "example.yaml"), availableMCPServerProfileYAML)

	wt, err := hostcontext.DetectWorktree(env.WorktreeRoot)
	if err != nil {
		t.Fatalf("DetectWorktree: %v", err)
	}
	bindingDir := filepath.Join(env.HomeDir, ".config", "omca", "bindings")
	mustWriteFileForActivateTest(t, filepath.Join(bindingDir, "example.yaml"), "apiVersion: omca.dev/v1alpha1\nkind: Binding\nmetadata:\n  id: binding:example\nspec:\n  match:\n    repository: "+wt.Root+"\n    paths: [\"**\"]\n  profiles:\n    - company:example\n")

	compileFn := compileFuncForMCP(&bytes.Buffer{})
	_, _, err = compileFn(map[string]domain.HostActivation{
		"codex": {Enable: domain.ActivationSelection{MCPServers: []string{"internal-docs"}}},
	})
	if err != nil {
		t.Fatalf("compileFuncForMCP (real omca_stage path): %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := runActivate(&stdout, &stderr, []string{"codex", "--confirm", "enable-mcp-server:internal-docs"})
	if code != 0 {
		t.Fatalf("runActivate after omca_stage staged an Enable.MCPServers selection: %d, want 0 (the selection should have been persisted so this activation's own fresh CAS recomposition sees it); stderr:\n%s", code, stderr.String())
	}
}

// TestCompileFuncForMCP_HostNotInstalled_DoesNotPersistActivation is a
// regression test (Copilot review finding on this PR, also applied to
// internal/tui/actions.go's identical stageAssetActivation): compileFuncForMCP
// must persist the merged Enable/Disable selection only once every named
// host's detect/observe/Parent-resolution has already succeeded -- not
// immediately after merging -- so a common, easy-to-hit failure (a named
// host not installed) never durably mutates desired/activation.yaml on a
// call that goes on to fail anyway.
func TestCompileFuncForMCP_HostNotInstalled_DoesNotPersistActivation(t *testing.T) {
	env := setupManagedTestEnv(t, false, false) // codex NOT installed
	profileDir := filepath.Join(env.HomeDir, ".config", "omca", "profiles", "company")
	mustWriteFileForActivateTest(t, filepath.Join(profileDir, "example.yaml"), availableMCPServerProfileYAML)

	wt, err := hostcontext.DetectWorktree(env.WorktreeRoot)
	if err != nil {
		t.Fatalf("DetectWorktree: %v", err)
	}
	bindingDir := filepath.Join(env.HomeDir, ".config", "omca", "bindings")
	mustWriteFileForActivateTest(t, filepath.Join(bindingDir, "example.yaml"), "apiVersion: omca.dev/v1alpha1\nkind: Binding\nmetadata:\n  id: binding:example\nspec:\n  match:\n    repository: "+wt.Root+"\n    paths: [\"**\"]\n  profiles:\n    - company:example\n")

	compileFn := compileFuncForMCP(&bytes.Buffer{})
	_, _, err = compileFn(map[string]domain.HostActivation{
		"codex": {Enable: domain.ActivationSelection{MCPServers: []string{"internal-docs"}}},
	})
	if err == nil {
		t.Fatal("compileFuncForMCP against an uninstalled host: want an error, got nil")
	}

	stateRoot, err := realStateRoot()
	if err != nil {
		t.Fatalf("realStateRoot: %v", err)
	}
	activationYAML := filepath.Join(worktreeStateDirPath(stateRoot, wt.ID), "desired", "activation.yaml")
	if _, statErr := os.Stat(activationYAML); statErr == nil {
		t.Errorf("%s exists after a failed compileFuncForMCP call (codex was never installed) -- the activation selection was persisted despite staging failing", activationYAML)
	} else if !os.IsNotExist(statErr) {
		t.Fatalf("stat %s: %v", activationYAML, statErr)
	}
}
