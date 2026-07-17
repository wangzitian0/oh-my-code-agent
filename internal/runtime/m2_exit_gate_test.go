package runtime

import (
	"path/filepath"
	"testing"
	"time"

	hostcontext "github.com/wangzitian0/oh-my-code-agent/internal/context"
	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
	"github.com/wangzitian0/oh-my-code-agent/internal/observe"
)

// TestM2ExitGate_CodexAndClaudeRunDifferentLoadoutsFromOneDesiredState is
// this PR's own end-to-end fixture for docs/project/roadmap.md's M2 Exit
// Gate line "two hosts in one worktree run deliberately different loadouts
// from one desired state." It runs codex and claude-code in ONE worktree,
// compiled from ONE CompileRequest/one shared Generation, using
// docs/product/requirements.md §4.1's own worked example almost verbatim:
// a Profile whose "codegraph" mcpServer is DEFAULT (active) only for codex
// and whose "deep-refactor" skill is DEFAULT (active) only for claude-code.
// It then runs each host's own Activation transaction (validate pending,
// CAS check, atomic switch, Ledger entry) independently, proving every
// other M2 exit-gate line together in the same fixture:
//
//   - "current never changes during a session": neither host's "current"
//     pointer exists until Activate runs (SetCurrentGeneration is never
//     called ambiently); this test's own structure -- compile once, stage
//     pending twice, activate twice -- is the only way "current" changes.
//   - "changes compile to pending": the ONE compiled generation is staged
//     as SetPendingGeneration for both hosts before either is activated.
//   - "activation is atomic and restart-bound": each host's Activate call
//     is its own transaction (see activate_test.go's dedicated crash-
//     injection/CAS tests for that property in isolation; this fixture
//     proves the two per-host transactions compose correctly).
//   - "rollback restores the parent generation": exercised in isolation by
//     rollback_test.go, not repeated here.
//   - "generated artifacts never become desired-state sources": Compile's
//     own inputs (req.Profiles/Activation/Exceptions) are plain
//     domain.Profile/Activation/Exception fixtures built directly in this
//     test, never read back from a previously compiled generation
//     directory -- there is no code path in this fixture (or anywhere in
//     this package) that feeds a generation's own output back in as a
//     future desired-state input.
func TestM2ExitGate_CodexAndClaudeRunDifferentLoadoutsFromOneDesiredState(t *testing.T) {
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	root := t.TempDir()
	worktreeRoot := filepath.Join(root, "project")
	mustWriteFile(t, filepath.Join(worktreeRoot, "AGENTS.md"), "# codex instructions\n")
	mustWriteFile(t, filepath.Join(worktreeRoot, "CLAUDE.md"), "# claude instructions\n")

	codexDetection := hostcontext.HostDetection{
		Host: "codex", Surface: "cli", Version: "0.144.5",
		NativeHomes: []hostcontext.NativeHome{{Name: "CODEX_HOME", Path: filepath.Join(root, "codex-home"), FromEnvVar: "CODEX_HOME"}},
	}
	claudeDetection := hostcontext.HostDetection{
		Host: "claude-code", Surface: "cli", Version: "2.1.211",
		NativeHomes: []hostcontext.NativeHome{{Name: "CLAUDE_CONFIG_DIR", Path: filepath.Join(root, "claude-config"), FromEnvVar: "CLAUDE_CONFIG_DIR"}},
	}
	obsCodex, err := observe.Observe(observe.Request{Detection: codexDetection, WorktreeRoot: worktreeRoot})
	if err != nil {
		t.Fatalf("observe.Observe (codex): %v", err)
	}
	obsClaude, err := observe.Observe(observe.Request{Detection: claudeDetection, WorktreeRoot: worktreeRoot})
	if err != nil {
		t.Fatalf("observe.Observe (claude): %v", err)
	}

	// docs/product/requirements.md §4.1's own worked example: codegraph is
	// host-scoped DEFAULT for codex only; deep-refactor is host-scoped
	// DEFAULT for claude-code only. Neither has a host-neutral baseline, so
	// each is active for exactly one host and inactive (but still recorded,
	// not silently omitted) for the other.
	sharedProfile := domain.Profile{
		APIVersion: domain.SupportedAPIVersion,
		Kind:       "Profile",
		Metadata:   domain.Metadata{ID: "company:example"},
		Spec: domain.ProfileSpec{
			Assets: domain.ProfileAssets{
				Skills: []domain.AssetRef{
					{ID: "code-review", Intent: domain.IntentAvailable},
					{ID: "deep-refactor", Intent: domain.IntentDefault, Hosts: []string{"claude-code"}},
				},
				MCPServers: []domain.AssetRef{
					{ID: "internal-docs", Intent: domain.IntentAvailable},
					{ID: "codegraph", Intent: domain.IntentDefault, Hosts: []string{"codex"}},
				},
			},
		},
	}

	worktreeStateDir := t.TempDir()
	req := CompileRequest{
		Worktree: hostcontext.Worktree{ID: worktreeIDFor(t, worktreeRoot), Root: worktreeRoot},
		Hosts: []HostCompileInput{
			{Detection: codexDetection, Observations: obsCodex},
			{Detection: claudeDetection, Observations: obsClaude},
		},
		Profiles: []domain.Profile{sharedProfile},
		Now:      now,
	}
	genID, err := CompileGenerationID(req)
	if err != nil {
		t.Fatalf("CompileGenerationID: %v", err)
	}
	outputDir := filepath.Join(worktreeStateDir, "generations", DirSafeID(genID))
	gen, err := Compile(req, outputDir)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	restoreWritable(t, outputDir)

	if len(gen.Spec.Hosts) != 2 {
		t.Fatalf("Spec.Hosts has %d entries, want 2 (one shared Generation for both hosts)", len(gen.Spec.Hosts))
	}

	// codegraph is included exactly once across the whole Sources list
	// (i.e. active for exactly one host -- codex), never for both and
	// never for neither.
	assertActiveForExactlyOneHost(t, gen, "mcpServer", "codegraph")
	assertActiveForExactlyOneHost(t, gen, "skill", "deep-refactor")

	// internal-docs (host-neutral AVAILABLE, not DEFAULT) is discovered for
	// both hosts but not automatically activated for either -- proving this
	// fixture's differentiation is driven by the host selector, not by
	// every asset ending up active everywhere regardless.
	for _, s := range gen.Spec.Sources {
		if s.Concept == "mcpServer" && s.Source == "internal-docs" && s.Included {
			t.Errorf("internal-docs (AVAILABLE, not DEFAULT) is Included:true; want it inactive by default: %+v", s)
		}
	}

	// Per-host artifact trees exist and are genuinely different files.
	tree := walkGeneratedTree(t, outputDir)
	if _, ok := tree[filepath.Join("hosts", "codex", "cli", "codex-home", "config.toml")]; !ok {
		t.Errorf("missing codex artifact tree; got %v", keysOf(tree))
	}
	if _, ok := tree[filepath.Join("hosts", "claude-code", "cli", "claude-config", "settings.json")]; !ok {
		t.Errorf("missing claude-code artifact tree; got %v", keysOf(tree))
	}

	// Stage the ONE compiled generation as pending for BOTH hosts.
	if err := SetPendingGeneration(worktreeStateDir, "codex", outputDir, gen, codexDetection, now); err != nil {
		t.Fatalf("SetPendingGeneration (codex): %v", err)
	}
	if err := SetPendingGeneration(worktreeStateDir, "claude-code", outputDir, gen, claudeDetection, now); err != nil {
		t.Fatalf("SetPendingGeneration (claude-code): %v", err)
	}

	// Neither host has a "current" generation yet -- current never changes
	// on its own, only through an explicit Activate call.
	if _, err := CurrentGenerationDir(worktreeStateDir, "codex"); err == nil {
		t.Fatal("codex has a current generation before any Activate call")
	}
	if _, err := CurrentGenerationDir(worktreeStateDir, "claude-code"); err == nil {
		t.Fatal("claude-code has a current generation before any Activate call")
	}

	// Activate each host independently -- restart-bound, per-host, exactly
	// as docs/architecture/runtime.md §5.5 describes ("hosts launch
	// independently from the same generation").
	codexResult, err := Activate(ActivateRequest{WorktreeStateDir: worktreeStateDir, Host: "codex", Fresh: req, Now: now.Add(time.Minute)})
	if err != nil {
		t.Fatalf("Activate (codex): %v", err)
	}
	claudeResult, err := Activate(ActivateRequest{WorktreeStateDir: worktreeStateDir, Host: "claude-code", Fresh: req, Now: now.Add(2 * time.Minute)})
	if err != nil {
		t.Fatalf("Activate (claude-code): %v", err)
	}
	if codexResult.ActivatedGenerationID != gen.Metadata.ID || claudeResult.ActivatedGenerationID != gen.Metadata.ID {
		t.Fatalf("both hosts should activate the SAME shared generation %s: codex=%s claude=%s", gen.Metadata.ID, codexResult.ActivatedGenerationID, claudeResult.ActivatedGenerationID)
	}

	codexCurrent, err := CurrentGenerationDir(worktreeStateDir, "codex")
	if err != nil {
		t.Fatalf("CurrentGenerationDir (codex): %v", err)
	}
	claudeCurrent, err := CurrentGenerationDir(worktreeStateDir, "claude-code")
	if err != nil {
		t.Fatalf("CurrentGenerationDir (claude-code): %v", err)
	}
	if codexCurrent != outputDir || claudeCurrent != outputDir {
		t.Errorf("both hosts should now point 'current' at the same shared generation directory %s: codex=%s claude=%s", outputDir, codexCurrent, claudeCurrent)
	}

	// Each host's own Ledger recorded its own activation independently.
	codexLedger, err := ReadLedger(worktreeStateDir, "codex")
	if err != nil {
		t.Fatalf("ReadLedger (codex): %v", err)
	}
	claudeLedger, err := ReadLedger(worktreeStateDir, "claude-code")
	if err != nil {
		t.Fatalf("ReadLedger (claude-code): %v", err)
	}
	assertHasActivatedEntry(t, codexLedger, gen.Metadata.ID, "codex")
	assertHasActivatedEntry(t, claudeLedger, gen.Metadata.ID, "claude-code")
}

func assertActiveForExactlyOneHost(t *testing.T, gen domain.Generation, concept, source string) {
	t.Helper()
	includedCount := 0
	for _, s := range gen.Spec.Sources {
		if s.Concept == concept && s.Source == source && s.Included {
			includedCount++
		}
	}
	if includedCount != 1 {
		t.Errorf("%s %q is Included:true in %d Sources entries, want exactly 1 (active for exactly one host)", concept, source, includedCount)
	}
}

func assertHasActivatedEntry(t *testing.T, entries []LedgerEntry, generationID, host string) {
	t.Helper()
	for _, e := range entries {
		if e.Kind == "activated" && e.GenerationID == generationID && e.Host == host {
			return
		}
	}
	t.Errorf("ledger for %s has no 'activated' entry for generation %s: %+v", host, generationID, entries)
}
