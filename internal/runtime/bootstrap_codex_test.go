package runtime

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wangzitian0/oh-my-code-agent/internal/observe"
)

// TestBootstrap_Codex_30MCPServersAnd20Skills_NoneLeak is issue #13 AC #1,
// implemented literally: "Fixture with 30 fake user-global MCP servers and
// 20 skills: the generated CODEX_HOME contains none of them." It builds a
// synthetic CODEX_HOME with 30 distinct [mcp_servers.*] entries in
// config.toml and 20 distinct skills/*/SKILL.md packages, runs
// internal/observe.Observe over it (the compiler's one source of "what
// exists"), then Bootstrap, then walks every file Bootstrap wrote and
// asserts none of the 30 server IDs or 20 skill names appear anywhere in
// path or content. A repository-scope AGENTS.md is also planted as a
// positive control: if this test only ever proved "the generated tree is
// empty," it would pass vacuously even with a broken compiler that excludes
// everything unconditionally -- the positive control instead proves real,
// selective exclusion (native excluded, repository included).
func TestBootstrap_Codex_30MCPServersAnd20Skills_NoneLeak(t *testing.T) {
	tr := newCodexFixtureTree(t)

	const (
		mcpCount   = 30
		skillCount = 20
	)
	var toml strings.Builder
	mcpIDs := make([]string, 0, mcpCount)
	for i := 0; i < mcpCount; i++ {
		id := fmt.Sprintf("leaky-native-mcp-%02d", i)
		mcpIDs = append(mcpIDs, id)
		fmt.Fprintf(&toml, "[mcp_servers.%s]\ncommand = \"npx\"\nargs = [\"%s-package\"]\n\n", id, id)
	}
	mustWriteFile(t, filepath.Join(tr.CodexHome, "config.toml"), toml.String())

	skillNames := make([]string, 0, skillCount)
	for i := 0; i < skillCount; i++ {
		name := fmt.Sprintf("leaky-native-skill-%02d", i)
		skillNames = append(skillNames, name)
		mustWriteFile(t, filepath.Join(tr.CodexHome, "skills", name, "SKILL.md"),
			fmt.Sprintf("---\nname: %s\n---\nDo not let this leak into any bootstrap generation.\n", name))
	}

	// Positive control: a real repository-scope Instructions file the
	// bootstrap policy DOES include.
	const repoInstructions = "# project instructions\nThis line must survive into the generation.\n"
	mustWriteFile(t, filepath.Join(tr.WorktreeRoot, "AGENTS.md"), repoInstructions)

	obs, err := observe.Observe(tr.request("0.144.5"))
	if err != nil {
		t.Fatalf("observe.Observe: %v", err)
	}
	// config.toml (3: mcp_server + hook + policy, see
	// internal/observe/rules.go's codexUserRules doc comment for why PR-16
	// multiplexes all three concepts onto this one physical file) +
	// AGENTS.md (1) + skillCount skills.
	if len(obs) != 3+1+skillCount {
		t.Fatalf("Observe returned %d observations, want %d (sanity check on fixture construction)", len(obs), 3+1+skillCount)
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

	tree := walkGeneratedTree(t, outputDir)
	if len(tree) == 0 {
		t.Fatal("Bootstrap wrote no files at all; this proof would be vacuous")
	}

	// issue #13 AC #1's literal subject is "the generated CODEX_HOME" --
	// the host-facing artifact tree at hosts/codex/<surface>/codex-home,
	// the directory a launch shim would actually point CODEX_HOME at. This
	// checks the WHOLE per-host artifact tree (hosts/codex/cli/**), not
	// just codex-home specifically, since the instructions/ directory and
	// the permission-defaults file sit alongside it under the same
	// per-host prefix and must be equally clean. manifest.json is
	// deliberately excluded from this check: it lives at the generation
	// root (never inside hosts/**, never pointed at by any host env var)
	// and its whole job -- issue #13 AC #3, docs/architecture/runtime.md
	// §12's "native exclusions are explained rather than hidden" -- is to
	// name exactly what was excluded and why, which necessarily means its
	// sources[].source paths contain these same fake IDs/names. That is
	// the intended "explained, not hidden" behavior, verified separately
	// below, not a leak.
	hostTreePrefix := filepath.Join("hosts", "codex", "cli") + string(filepath.Separator)
	var hostTreeBlob strings.Builder
	var manifestBlob string
	hostTreeFileCount := 0
	for path, content := range tree {
		if path == "manifest.json" {
			manifestBlob = string(content)
			continue
		}
		if !strings.HasPrefix(path, hostTreePrefix) {
			t.Errorf("unexpected generated file outside both manifest.json and hosts/codex/cli/**: %s", path)
			continue
		}
		hostTreeFileCount++
		hostTreeBlob.WriteString(path)
		hostTreeBlob.WriteByte('\n')
		hostTreeBlob.Write(content)
		hostTreeBlob.WriteByte('\n')
	}
	if hostTreeFileCount == 0 {
		t.Fatal("no files were generated under hosts/codex/cli/**; this proof would be vacuous")
	}
	if manifestBlob == "" {
		t.Fatal("manifest.json was not generated")
	}
	hostBlob := hostTreeBlob.String()

	for _, id := range mcpIDs {
		if strings.Contains(hostBlob, id) {
			t.Errorf("leaked native user-global MCP server id %q into the generated host-facing artifact tree", id)
		}
	}
	for _, name := range skillNames {
		if strings.Contains(hostBlob, name) {
			t.Errorf("leaked native user-global skill name %q into the generated host-facing artifact tree", name)
		}
	}

	// The generated CODEX_HOME itself (hosts/codex/cli/codex-home) must
	// exist -- it is the literal thing AC #1 names.
	codexHomeDir := filepath.Join("hosts", "codex", "cli", "codex-home")
	foundCodexHome := false
	for path := range tree {
		if strings.HasPrefix(path, codexHomeDir+string(filepath.Separator)) || path == codexHomeDir {
			foundCodexHome = true
		}
	}
	if !foundCodexHome {
		t.Fatalf("no file was generated under %s; expected the conservative permission defaults file there", codexHomeDir)
	}

	// Positive control: repository Instructions content DID survive, in the
	// host-facing tree specifically (not just somewhere in the generation).
	if !strings.Contains(hostBlob, "This line must survive into the generation.") {
		t.Error("repository-scope AGENTS.md content did not survive into the generated host-facing tree (positive control failed)")
	}

	// "Explained, not hidden": every excluded native source's path is named
	// in manifest.json, each with its own reason -- proving this is a
	// disclosed exclusion, not silent data loss. All 30 fake MCP servers
	// live in the one shared config.toml source path, so checking that
	// path once covers all 30; skills are one file per package, so a
	// sample of one is checked below (the manifest_test.go suite already
	// proves every single observation gets its own sources entry).
	if !strings.Contains(manifestBlob, "config.toml") {
		t.Error("manifest.json does not reference the excluded native config.toml source at all")
	}
	sampleSkill := skillNames[0]
	if !strings.Contains(manifestBlob, sampleSkill) {
		t.Errorf("manifest.json does not reference excluded native skill %q by path -- exclusions must be explained, not silently hidden", sampleSkill)
	}
	if !strings.Contains(manifestBlob, "excluded: native user-global source") {
		t.Error("manifest.json does not carry the expected exclusion reason text")
	}

	if gen.Metadata.ID == "" {
		t.Error("Bootstrap returned a Generation with an empty metadata.id")
	}
}
