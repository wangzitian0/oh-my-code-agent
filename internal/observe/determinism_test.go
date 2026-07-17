package observe

import (
	"path/filepath"
	"testing"
)

// TestObserve_Deterministic_CodexRepeatCalls is the acceptance criterion
// "Observing fixture homes twice yields byte-identical inventories
// (determinism)" exercised directly: Observe is called twice against the
// same, unchanged fixture tree, and the two results must marshal to
// byte-identical JSON — same order, same digests, no field that varies
// between calls (e.g. no wall-clock timestamp).
func TestObserve_Deterministic_CodexRepeatCalls(t *testing.T) {
	tr := newCodexTree(t)
	mustWriteFile(t, filepath.Join(tr.CodexHome, "AGENTS.override.md"), "# override\n")
	mustWriteFile(t, filepath.Join(tr.CodexHome, "AGENTS.md"), "# base\n")
	mustWriteFile(t, filepath.Join(tr.CodexHome, "config.toml"), "[mcp_servers.demo]\ncommand = \"npx\"\n")
	mustWriteFile(t, filepath.Join(tr.CodexHome, "skills", "a", "SKILL.md"), "---\nname: a\n---\n")
	mustWriteFile(t, filepath.Join(tr.CodexHome, "skills", "z", "SKILL.md"), "---\nname: z\n---\n")
	mustWriteFile(t, filepath.Join(tr.HomeAgentsDir, "shared", "SKILL.md"), "---\nname: shared\n---\n")
	mustWriteFile(t, filepath.Join(tr.WorktreeRoot, "AGENTS.md"), "# project\n")
	mustWriteFile(t, filepath.Join(tr.WorktreeRoot, ".codex", "config.toml"), "[mcp_servers.proj]\ncommand = \"./run.sh\"\n")
	mustWriteFile(t, filepath.Join(tr.WorktreeRoot, ".agents", "skills", "proj", "SKILL.md"), "---\nname: proj\n---\n")

	req := tr.request("0.144.5")

	first, err := Observe(req)
	if err != nil {
		t.Fatalf("Observe (first): %v", err)
	}
	second, err := Observe(req)
	if err != nil {
		t.Fatalf("Observe (second): %v", err)
	}

	if len(first) == 0 {
		t.Fatal("Observe returned zero observations; this determinism test would be vacuous")
	}
	if len(first) != len(second) {
		t.Fatalf("first call returned %d observations, second returned %d", len(first), len(second))
	}

	firstJSON := jsonRoundTrip(t, first)
	secondJSON := jsonRoundTrip(t, second)
	if firstJSON != secondJSON {
		t.Fatalf("Observe is not deterministic across repeat calls:\nfirst:  %s\nsecond: %s", firstJSON, secondJSON)
	}

	// Order must be stable and explicit (sorted by Metadata.ID), not an
	// accident of this particular filesystem's directory-entry order.
	for i := 1; i < len(first); i++ {
		if first[i-1].Metadata.ID >= first[i].Metadata.ID {
			t.Fatalf("observations are not sorted by Metadata.ID: %q >= %q at index %d", first[i-1].Metadata.ID, first[i].Metadata.ID, i)
		}
	}
}

// TestObserve_Deterministic_ClaudeCodeRepeatCalls mirrors the Codex
// determinism test for Claude Code, additionally covering the JSON-parsed
// MCP content branch (parseContent's other code path) so determinism is
// proven for both content-handling branches, not just the opaque-text one.
func TestObserve_Deterministic_ClaudeCodeRepeatCalls(t *testing.T) {
	tr := newClaudeTree(t)
	mustWriteFile(t, filepath.Join(tr.ClaudeConfigDir, "CLAUDE.md"), "# user\n")
	mustWriteFile(t, filepath.Join(tr.ClaudeConfigDir, "rules", "a.md"), "# rule a\n")
	mustWriteFile(t, filepath.Join(tr.ClaudeConfigDir, ".claude.json"), `{"mcpServers":{"demo":{"command":"npx","args":["-y","x"]}}}`)
	mustWriteFile(t, filepath.Join(tr.ClaudeConfigDir, "skills", "a", "SKILL.md"), "---\nname: a\n---\n")
	mustWriteFile(t, filepath.Join(tr.WorktreeRoot, ".claude", "CLAUDE.md"), "# project\n")
	mustWriteFile(t, filepath.Join(tr.WorktreeRoot, ".mcp.json"), `{"mcpServers":{"proj":{"command":"./run.sh"}}}`)

	req := tr.request("2.1.211")

	first, err := Observe(req)
	if err != nil {
		t.Fatalf("Observe (first): %v", err)
	}
	second, err := Observe(req)
	if err != nil {
		t.Fatalf("Observe (second): %v", err)
	}
	if len(first) == 0 {
		t.Fatal("Observe returned zero observations; this determinism test would be vacuous")
	}

	firstJSON := jsonRoundTrip(t, first)
	secondJSON := jsonRoundTrip(t, second)
	if firstJSON != secondJSON {
		t.Fatalf("Observe is not deterministic across repeat calls:\nfirst:  %s\nsecond: %s", firstJSON, secondJSON)
	}
}
