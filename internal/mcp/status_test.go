package mcp

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	hostcontext "github.com/wangzitian0/oh-my-code-agent/internal/context"
	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
	"github.com/wangzitian0/oh-my-code-agent/internal/observe"
	"github.com/wangzitian0/oh-my-code-agent/internal/runtime"
)

// mustWriteFile is this package's own tiny fixture helper, mirroring
// internal/runtime/helpers_test.go's identical function (not exported
// across the package boundary, so duplicated here exactly like
// internal/shim's isExecutableFile precedent documents for small,
// state-free helpers).
func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mustWriteFile: mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("mustWriteFile: write %s: %v", path, err)
	}
}

// buildManagedCodexWorktree compiles a real codex bootstrap generation
// (internal/runtime.Bootstrap, via EnsureGeneration) from a synthetic
// CODEX_HOME carrying, if withMCPConfig, one native MCP configuration file
// (config.toml, itself registering several servers -- but see
// HostStatus.ExcludedMCPServers' doc comment: internal/observe's mcp_server
// concept is file-level, so this always contributes at most 1 to the
// exclusion count no matter how many [mcp_servers.*] tables the file
// contains) and skillCount native Skills (one SKILL.md file each, which
// DOES map 1:1 to the exclusion count). Records the result as "current"
// (SetCurrentGeneration) — exactly the on-disk shape `omca env` produces,
// which is what ComputeStatus reads. Returns the worktree state directory
// ComputeStatusRequest.WorktreeStateDir expects.
func buildManagedCodexWorktree(t *testing.T, withMCPConfig bool, skillCount int) string {
	t.Helper()
	root := t.TempDir()
	codexHome := filepath.Join(root, "codex-home")
	worktreeRoot := filepath.Join(root, "project")

	if withMCPConfig {
		toml := "[mcp_servers.one]\ncommand = \"npx\"\n\n[mcp_servers.two]\ncommand = \"npx\"\n\n[mcp_servers.three]\ncommand = \"npx\"\n"
		mustWriteFile(t, filepath.Join(codexHome, "config.toml"), toml)
	}
	for i := 0; i < skillCount; i++ {
		name := "skill" + string(rune('a'+i))
		mustWriteFile(t, filepath.Join(codexHome, "skills", name, "SKILL.md"), "---\nname: "+name+"\n---\nbody\n")
	}
	mustWriteFile(t, filepath.Join(worktreeRoot, "AGENTS.md"), "# instructions\n")

	det := hostcontext.HostDetection{
		Host:    "codex",
		Surface: "cli",
		Version: "0.144.5",
		NativeHomes: []hostcontext.NativeHome{
			{Name: "CODEX_HOME", Path: codexHome, FromEnvVar: "CODEX_HOME"},
		},
	}
	obs, err := observe.Observe(observe.Request{Detection: det, WorktreeRoot: worktreeRoot})
	if err != nil {
		t.Fatalf("observe.Observe: %v", err)
	}
	digest, err := domain.CanonicalDigest(worktreeRoot)
	if err != nil {
		t.Fatalf("CanonicalDigest: %v", err)
	}
	wt := hostcontext.Worktree{ID: "worktree:" + digest, Root: worktreeRoot}

	req := runtime.BootstrapRequest{
		Detection:    det,
		Worktree:     wt,
		Observations: obs,
		Now:          time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC),
	}
	worktreeStateDir := t.TempDir()
	gen, outputDir, err := runtime.EnsureGeneration(req, filepath.Join(worktreeStateDir, "generations"))
	if err != nil {
		t.Fatalf("EnsureGeneration: %v", err)
	}
	t.Cleanup(func() { restoreWritableTree(outputDir) })
	if err := runtime.SetCurrentGeneration(worktreeStateDir, "codex", outputDir, gen, det, time.Now()); err != nil {
		t.Fatalf("SetCurrentGeneration: %v", err)
	}
	return worktreeStateDir
}

// restoreWritableTree undoes internal/runtime/readonly.go's read-only
// generation tree so t.TempDir()'s own cleanup can remove it — the same
// need internal/runtime/helpers_test.go's restoreWritable documents,
// duplicated here for the same "small, package-local helper" reason.
// Symlinks are skipped defensively, matching internal/perf/perf_test.go's
// newMeasurementBaseDir: this function's only call site walks outputDir
// (the generation directory itself), a sibling of, never a parent of, the
// "current" symlink runtime.SetCurrentGeneration plants elsewhere under
// worktreeStateDir, so no symlink is actually encountered today — but
// os.Chmod on Unix follows a symlink to its target, and a future caller
// that reuses this helper against worktreeStateDir directly would silently
// reintroduce the exact "chmod clobbers the already-fixed target
// directory's mode" hazard that package's own doc comment describes in
// detail after hitting it once during that package's development.
func restoreWritableTree(root string) {
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // best-effort cleanup only
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil // never chmod through a symlink -- see doc comment above
		}
		if d.IsDir() {
			_ = os.Chmod(path, 0o755)
		} else {
			_ = os.Chmod(path, 0o644)
		}
		return nil
	})
}

// TestComputeStatus_ReportsExclusionCountsAndContextCost is issue #15's
// literal AC exercised end to end: a generation compiled from a fixture
// with one native MCP configuration source (registering three servers --
// see buildManagedCodexWorktree's doc comment on why the exclusion count is
// 1, not 3) and 2 native Skills reports exactly those counts, a non-nil
// context-cost estimate honestly labeled as an estimate, and the current
// generation ID.
func TestComputeStatus_ReportsExclusionCountsAndContextCost(t *testing.T) {
	worktreeStateDir := buildManagedCodexWorktree(t, true, 2)

	result, err := ComputeStatus(ComputeStatusRequest{
		WorktreeID:       "worktree:sha256:deadbeef",
		ContextID:        "context:sha256:cafef00d",
		WorktreeStateDir: worktreeStateDir,
		Hosts:            []string{"codex", "claude-code"},
	})
	if err != nil {
		t.Fatalf("ComputeStatus: %v", err)
	}
	if result.WorktreeID != "worktree:sha256:deadbeef" {
		t.Errorf("WorktreeID = %q, want passthrough of the input", result.WorktreeID)
	}
	if len(result.Hosts) != 2 {
		t.Fatalf("Hosts has %d entries, want 2 (one per requested host)", len(result.Hosts))
	}

	codex := result.Hosts[0]
	if codex.Host != "codex" {
		t.Fatalf("Hosts[0].Host = %q, want %q", codex.Host, "codex")
	}
	if !codex.Managed {
		t.Fatalf("codex HostStatus.Managed = false, want true (a generation was compiled and recorded); detail: %s", codex.Detail)
	}
	if codex.GenerationID == "" {
		t.Error("codex HostStatus.GenerationID is empty")
	}
	if codex.ExcludedMCPServers != 1 {
		t.Errorf("codex ExcludedMCPServers = %d, want 1 (one excluded native MCP configuration source, regardless of how many servers it registers)", codex.ExcludedMCPServers)
	}
	if codex.ExcludedSkills != 2 {
		t.Errorf("codex ExcludedSkills = %d, want 2", codex.ExcludedSkills)
	}
	if codex.ContextCost == nil {
		t.Fatal("codex ContextCost is nil, want a populated estimate")
	}
	wantTokens := 1*estimatedTokensPerExcludedMCPServer + 2*estimatedTokensPerExcludedSkill
	if codex.ContextCost.EstimatedTokensExcluded != wantTokens {
		t.Errorf("EstimatedTokensExcluded = %d, want %d", codex.ContextCost.EstimatedTokensExcluded, wantTokens)
	}
	if codex.ContextCost.Confidence != ConfidenceEstimateNotMeasured {
		t.Errorf("Confidence = %q, want the fixed estimate-not-measured label", codex.ContextCost.Confidence)
	}

	claude := result.Hosts[1]
	if claude.Host != "claude-code" {
		t.Fatalf("Hosts[1].Host = %q, want %q", claude.Host, "claude-code")
	}
	if claude.Managed {
		t.Error("claude-code HostStatus.Managed = true, want false (no generation was ever compiled for this host in this fixture)")
	}
	if claude.Detail == "" {
		t.Error("claude-code HostStatus.Detail is empty even though Managed is false")
	}
	if claude.ContextCost != nil {
		t.Error("claude-code ContextCost is non-nil, want nil when Managed is false")
	}
}

// TestComputeStatus_ZeroExclusions_StillReportsManagedWithZeroCounts is the
// negative control: a generation with nothing native to exclude still
// reports Managed:true with zero counts and a zero-token (but still present
// and honestly labeled) estimate, rather than omitting ContextCost or
// misreporting Managed.
func TestComputeStatus_ZeroExclusions_StillReportsManagedWithZeroCounts(t *testing.T) {
	worktreeStateDir := buildManagedCodexWorktree(t, false, 0)

	result, err := ComputeStatus(ComputeStatusRequest{
		WorktreeStateDir: worktreeStateDir,
		Hosts:            []string{"codex"},
	})
	if err != nil {
		t.Fatalf("ComputeStatus: %v", err)
	}
	codex := result.Hosts[0]
	if !codex.Managed {
		t.Fatalf("Managed = false, want true; detail: %s", codex.Detail)
	}
	if codex.ExcludedMCPServers != 0 || codex.ExcludedSkills != 0 {
		t.Errorf("exclusion counts = (%d, %d), want (0, 0)", codex.ExcludedMCPServers, codex.ExcludedSkills)
	}
	if codex.ContextCost == nil {
		t.Fatal("ContextCost is nil, want a present, zero-valued estimate")
	}
	if codex.ContextCost.EstimatedTokensExcluded != 0 {
		t.Errorf("EstimatedTokensExcluded = %d, want 0", codex.ContextCost.EstimatedTokensExcluded)
	}
}

// TestComputeStatus_RequiresWorktreeStateDir proves the "explicit inputs"
// discipline is enforced, not just documented: an empty WorktreeStateDir is
// a caller error, not a silent no-op or a panic reading an empty path.
func TestComputeStatus_RequiresWorktreeStateDir(t *testing.T) {
	if _, err := ComputeStatus(ComputeStatusRequest{Hosts: []string{"codex"}}); err == nil {
		t.Fatal("ComputeStatus with empty WorktreeStateDir: want error, got nil")
	}
}

// TestComputeStatus_NoHostsManaged_NeverErrors proves a completely fresh
// worktree (no `omca env` ever run) still returns a valid StatusResult
// naming every host as unmanaged, rather than an error — this is the
// expected first-ever omca_status call inside a brand-new bootstrap
// session.
func TestComputeStatus_NoHostsManaged_NeverErrors(t *testing.T) {
	result, err := ComputeStatus(ComputeStatusRequest{
		WorktreeStateDir: t.TempDir(),
		Hosts:            []string{"codex", "claude-code"},
	})
	if err != nil {
		t.Fatalf("ComputeStatus: %v", err)
	}
	for _, h := range result.Hosts {
		if h.Managed {
			t.Errorf("host %s reported Managed:true in a worktree state dir with no generations at all", h.Host)
		}
	}
}

// TestCountUserExclusions_ExcludesCapabilityGapPlaceholders is a
// regression test for a real Copilot review finding on this PR:
// CountUserExclusions's own doc comment claimed a capability-gap entry
// "carries no Scope at all," but internal/runtime/compile.go's actual
// claudeConfigDirExclusionGapSources sets Scope: "user" on both of its
// placeholder entries (mcp_server and skill) — so, before this fix, they
// were silently counted as confirmed exclusions alongside real observed-
// and-excluded sources, inflating N/M by one per gap class. This builds a
// domain.Generation with one real excluded mcp_server source, one real
// excluded skill source, and two CapabilityGap:true placeholders (one per
// concept, mirroring claudeConfigDirExclusionGapSources exactly), and
// proves the count reflects only the two real sources.
func TestCountUserExclusions_ExcludesCapabilityGapPlaceholders(t *testing.T) {
	gen := domain.Generation{
		Spec: domain.GenerationSpec{
			Sources: []domain.GenerationSourceEntry{
				{Concept: "mcp_server", Scope: "user", Source: "/native/.claude.json", Included: false, Reason: "excluded: native user-global source"},
				{Concept: "skill", Scope: "user", Source: "/native/skills/deploy/SKILL.md", Included: false, Reason: "excluded: native user-global source"},
				{Concept: "mcp_server", Scope: "user", Included: false, Reason: "capability gap: ...", CapabilityGap: true, TrackingIssue: "https://github.com/wangzitian0/oh-my-code-agent/issues/47"},
				{Concept: "skill", Scope: "user", Included: false, Reason: "capability gap: ...", CapabilityGap: true, TrackingIssue: "https://github.com/wangzitian0/oh-my-code-agent/issues/47"},
				// A workspace-scope excluded source and an included source
				// must not be counted either — already covered by the
				// existing Scope/Included filters, included here so this
				// test is a complete proof of the function's whole filter,
				// not just the new CapabilityGap line.
				{Concept: "mcp_server", Scope: "workspace", Included: false, Reason: "excluded: repository-scope Skill/MCP source, not yet activated"},
				{Concept: "instruction", Scope: "workspace", Included: true, Reason: "included: repository-scope Instructions chain"},
			},
		},
	}

	mcpServers, skills := CountUserExclusions(gen)
	if mcpServers != 1 {
		t.Errorf("mcpServers = %d, want 1 (one real exclusion; the capability-gap placeholder must not count)", mcpServers)
	}
	if skills != 1 {
		t.Errorf("skills = %d, want 1 (one real exclusion; the capability-gap placeholder must not count)", skills)
	}
}
