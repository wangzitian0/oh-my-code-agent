package main

import (
	"bytes"
	"path/filepath"
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
