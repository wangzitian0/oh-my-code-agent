package runtime

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
	"github.com/wangzitian0/oh-my-code-agent/internal/observe"
)

// TestBootstrap_ClaudeCode_ExcludesNativeMCPAndSkills is the Claude Code
// half of issue #13 AC #1's spirit (the AC's literal fixture numbers are
// Codex-specific, but the exclusion property is not): a native
// CLAUDE_CONFIG_DIR populated with an MCP registration and a Skill must not
// leak into the generated tree.
func TestBootstrap_ClaudeCode_ExcludesNativeMCPAndSkills(t *testing.T) {
	tr := newClaudeFixtureTree(t)
	mustWriteFile(t, filepath.Join(tr.ClaudeConfigDir, ".claude.json"), `{"mcpServers":{"native-leaky-server":{"command":"npx"}}}`)
	mustWriteFile(t, filepath.Join(tr.ClaudeConfigDir, "skills", "native-leaky-skill", "SKILL.md"), "---\nname: native-leaky-skill\n---\nmust not leak\n")
	mustWriteFile(t, filepath.Join(tr.WorktreeRoot, "CLAUDE.md"), "# project instructions must survive\n")

	obs, err := observe.Observe(tr.request("2.1.211"))
	if err != nil {
		t.Fatalf("observe.Observe: %v", err)
	}

	req := BootstrapRequest{
		Detection:    tr.detection("2.1.211"),
		Worktree:     tr.worktree(t),
		Observations: obs,
		Now:          time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC),
	}
	outputDir := filepath.Join(t.TempDir(), "generation")
	gen, err := Bootstrap(req, outputDir)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	restoreWritable(t, outputDir)

	tree := walkGeneratedTree(t, outputDir)
	// As in bootstrap_codex_test.go: the "must not leak" check applies to
	// the host-facing artifact tree (hosts/claude-code/cli/**), not to
	// manifest.json, which legitimately names excluded source paths as
	// part of explaining every exclusion (issue #13 AC #3).
	hostTreePrefix := filepath.Join("hosts", "claude-code", "cli") + string(filepath.Separator)
	var hostBlob strings.Builder
	var manifestBlob string
	hostTreeFileCount := 0
	for path, content := range tree {
		if path == "manifest.json" {
			manifestBlob = string(content)
			continue
		}
		if !strings.HasPrefix(path, hostTreePrefix) {
			t.Errorf("unexpected generated file outside both manifest.json and hosts/claude-code/cli/**: %s", path)
			continue
		}
		hostTreeFileCount++
		hostBlob.WriteString(path)
		hostBlob.WriteByte('\n')
		hostBlob.Write(content)
		hostBlob.WriteByte('\n')
	}
	if hostTreeFileCount == 0 {
		t.Fatal("no files were generated under hosts/claude-code/cli/**; this proof would be vacuous")
	}
	if strings.Contains(hostBlob.String(), "native-leaky-server") {
		t.Error("native user-global MCP server leaked into the generated host-facing artifact tree")
	}
	if strings.Contains(hostBlob.String(), "native-leaky-skill") {
		t.Error("native user-global Skill leaked into the generated host-facing artifact tree")
	}
	if !strings.Contains(hostBlob.String(), "project instructions must survive") {
		t.Error("repository Instructions content did not survive in the host-facing tree (positive control failed)")
	}

	// "Explained, not hidden": manifest.json does name the excluded native
	// sources' file paths. Note this is the *file* path, not necessarily
	// every identifier nested inside it: Claude Code's MCP registrations
	// all live in one file (.claude.json), so the manifest names that file
	// (excluded, concept mcp_server), not each individual server ID
	// registered inside it -- internal/observe's own granularity for this
	// concept is file-level, not per-registered-server, and this
	// compiler's manifest reflects exactly that granularity, no more, no
	// less. Skills are one file per package, so the skill's own name IS
	// part of its source path and does appear.
	if !strings.Contains(manifestBlob, ".claude.json") {
		t.Error("manifest.json does not reference the excluded native .claude.json MCP source by path")
	}
	if !strings.Contains(manifestBlob, "native-leaky-skill") {
		t.Error("manifest.json does not reference the excluded native Skill by path")
	}

	claudeConfigDir := filepath.Join("hosts", "claude-code", "cli", "claude-config")
	found := false
	for path := range tree {
		if strings.HasPrefix(path, claudeConfigDir+string(filepath.Separator)) {
			found = true
		}
	}
	if !found {
		t.Fatalf("no file was generated under %s", claudeConfigDir)
	}

	assertClaudeCapabilityGap(t, gen)
}

// TestBootstrap_ClaudeCode_CapabilityGap_PresentEvenWithNoNativeSources
// proves the capability-gap entries are unconditional M1 policy for
// claude-code -- not merely emitted "when something suspicious was
// observed." Issue #13's round-2 anti-drift rule requires the gap to be
// tracked as long as the mechanism itself is unproven, regardless of
// whether this particular run happened to discover any native content.
func TestBootstrap_ClaudeCode_CapabilityGap_PresentEvenWithNoNativeSources(t *testing.T) {
	tr := newClaudeFixtureTree(t)
	mustWriteFile(t, filepath.Join(tr.WorktreeRoot, "CLAUDE.md"), "# instructions\n")

	obs, err := observe.Observe(tr.request("2.1.211"))
	if err != nil {
		t.Fatalf("observe.Observe: %v", err)
	}
	req := BootstrapRequest{
		Detection:    tr.detection("2.1.211"),
		Worktree:     tr.worktree(t),
		Observations: obs,
		Now:          time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC),
	}
	outputDir := filepath.Join(t.TempDir(), "generation")
	gen, err := Bootstrap(req, outputDir)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	restoreWritable(t, outputDir)

	assertClaudeCapabilityGap(t, gen)
}

// TestBootstrap_Codex_NoCapabilityGapEntries proves the capability-gap
// entries are Claude-Code-specific (see compile.go's
// claudeConfigDirExclusionGapSources doc comment for why): a Codex
// generation's sources list must never contain one, since Codex's
// CODEX_HOME/HOME redirection is a boundary this compiler fully controls by
// construction.
func TestBootstrap_Codex_NoCapabilityGapEntries(t *testing.T) {
	tr := newCodexFixtureTree(t)
	mustWriteFile(t, filepath.Join(tr.WorktreeRoot, "AGENTS.md"), "# instructions\n")
	obs, err := observe.Observe(tr.request("0.144.5"))
	if err != nil {
		t.Fatalf("observe.Observe: %v", err)
	}
	req := BootstrapRequest{
		Detection:    tr.detection("0.144.5"),
		Worktree:     tr.worktree(t),
		Observations: obs,
		Now:          time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC),
	}
	outputDir := filepath.Join(t.TempDir(), "generation")
	gen, err := Bootstrap(req, outputDir)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	restoreWritable(t, outputDir)

	for _, s := range gen.Spec.Sources {
		if s.CapabilityGap {
			t.Errorf("unexpected capability gap entry on a Codex generation: %+v", s)
		}
	}
}

// assertClaudeCapabilityGap asserts gen's sources list carries at least one
// capability-gap entry per concept (mcp_server, skill), each with a
// non-empty TrackingIssue pointing at the real follow-up issue this PR
// filed -- issue #13's round-2 rule: "any residual class is listed in the
// manifest as an explicit capability gap, never silently ignored" and
// "the generation manifest links that issue."
func assertClaudeCapabilityGap(t *testing.T, gen domain.Generation) {
	t.Helper()
	seenConcepts := map[string]bool{}
	for _, s := range gen.Spec.Sources {
		if !s.CapabilityGap {
			continue
		}
		if s.TrackingIssue == "" {
			t.Errorf("capability gap entry %+v has no trackingIssue", s)
		}
		if s.TrackingIssue != ClaudeConfigDirExclusionGapIssueURL {
			t.Errorf("capability gap entry trackingIssue = %q, want %q", s.TrackingIssue, ClaudeConfigDirExclusionGapIssueURL)
		}
		if s.Reason == "" {
			t.Errorf("capability gap entry %+v has no reason", s)
		}
		seenConcepts[s.Concept] = true
	}
	for _, concept := range []string{"mcp_server", "skill"} {
		if !seenConcepts[concept] {
			t.Errorf("no capability gap entry for concept %q; want one for both mcp_server and skill", concept)
		}
	}
}
