package effective

import (
	"testing"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
)

func mcpCandidate(ref, logicalID, scopeKind string, fields map[string]any) Candidate {
	digest, err := domain.CanonicalDigest(fields)
	if err != nil {
		panic(err)
	}
	return Candidate{
		Concept:       "mcp_server",
		LogicalID:     logicalID,
		Ref:           ref,
		Scope:         domain.ObservationScope{Kind: scopeKind, Root: scopeKind},
		Source:        domain.ObservationSource{Kind: "file", Path: ref},
		EvidenceLevel: domain.EvidenceLevelParsed,
		Fields:        fields,
		ContentDigest: digest,
	}
}

// TestMatchIdentities_SameIDDifferentScope_OneLogicalEntity is the issue #21
// example: an MCP server with the same transport+id observed at both user
// and workspace scope is one logical entity observed twice, not two.
func TestMatchIdentities_SameIDDifferentScope_OneLogicalEntity(t *testing.T) {
	a := mcpCandidate("user/.claude.json#mcpServers.docs", "stdio|docs", "user", map[string]any{"command": "docs-server"})
	b := mcpCandidate("project/.mcp.json#mcpServers.docs", "stdio|docs", "workspace", map[string]any{"command": "docs-server"})

	groups, ambiguous := MatchIdentities([]Candidate{a, b})
	if len(groups) != 1 {
		t.Fatalf("groups = %d, want 1 (same transport+id must be one logical entity)", len(groups))
	}
	if len(groups[0].Candidates) != 2 {
		t.Fatalf("group candidates = %d, want 2", len(groups[0].Candidates))
	}
	if len(ambiguous) != 0 {
		t.Errorf("ambiguous = %+v, want none (this is a confident identity match, not an ambiguity)", ambiguous)
	}
}

// TestMatchIdentities_AmbiguousPair_GoldenCase is the required round-2-audit
// golden: two physical MCP server registrations that plausibly could or
// could not be the same logical entity — different registered IDs
// ("docs-server" vs "documentation"), but byte-identical connection
// definitions (same command). The strict transport+id identity rule says
// "different"; the content coincidence says "maybe the same physical
// server, registered twice under different names." Neither signal
// dominates, so the Identity Matcher must preserve the ambiguity: it must
// NOT silently merge them into one logical entity, and it must NOT silently
// treat them as confidently unrelated with no record at all.
func TestMatchIdentities_AmbiguousPair_GoldenCase(t *testing.T) {
	fields := map[string]any{"command": "/usr/local/bin/mcp-docs", "args": []any{"--stdio"}}
	a := mcpCandidate("claude-config/.claude.json#mcpServers.docs-server", "stdio|docs-server", "user", fields)
	b := mcpCandidate("project/.mcp.json#mcpServers.documentation", "stdio|documentation", "workspace", fields)

	groups, ambiguous := MatchIdentities([]Candidate{a, b})

	// Not silently merged: two distinct logical IDs, two separate groups.
	if len(groups) != 2 {
		t.Fatalf("groups = %d, want 2 (must not silently merge an ambiguous pair into one logical entity)", len(groups))
	}
	for _, g := range groups {
		if len(g.Candidates) != 1 {
			t.Errorf("group %s has %d candidates, want 1 each (no silent merge)", g.LogicalID, len(g.Candidates))
		}
	}

	// Not silently treated as unrelated: the ambiguity is recorded.
	if len(ambiguous) != 1 {
		t.Fatalf("ambiguous = %d, want exactly 1 preserved ambiguous pair", len(ambiguous))
	}
	pair := ambiguous[0]
	if pair.Concept != "mcp_server" {
		t.Errorf("Concept = %q, want mcp_server", pair.Concept)
	}
	if pair.Reason == "" {
		t.Error("Reason must explain why this pair is ambiguous, not just flag it silently")
	}
	gotRefs := map[string]bool{pair.A.Ref: true, pair.B.Ref: true}
	if !gotRefs[a.Ref] || !gotRefs[b.Ref] {
		t.Errorf("ambiguous pair refs = %v, want both %q and %q", gotRefs, a.Ref, b.Ref)
	}
}

// TestMatchIdentities_DifferentContent_NotAmbiguous proves the heuristic is
// not trigger-happy: two genuinely different MCP servers under different
// IDs with different connection definitions must not be flagged.
func TestMatchIdentities_DifferentContent_NotAmbiguous(t *testing.T) {
	a := mcpCandidate("a.json#mcpServers.one", "stdio|one", "user", map[string]any{"command": "tool-one"})
	b := mcpCandidate("b.json#mcpServers.two", "stdio|two", "workspace", map[string]any{"command": "tool-two"})

	_, ambiguous := MatchIdentities([]Candidate{a, b})
	if len(ambiguous) != 0 {
		t.Errorf("ambiguous = %+v, want none for genuinely different servers", ambiguous)
	}
}

// TestMatchIdentities_SkillAndInstruction_NoAmbiguityHeuristic proves the
// ambiguity heuristic is scoped to mcp_server only (see identity.go's doc
// comment): identical skill content under two different names is a
// duplicated skill, not an identity ambiguity.
func TestMatchIdentities_SkillAndInstruction_NoAmbiguityHeuristic(t *testing.T) {
	a := Candidate{Concept: "skill", LogicalID: "deploy|file", Ref: "a/SKILL.md", ContentDigest: "sha256:same"}
	b := Candidate{Concept: "skill", LogicalID: "release|file", Ref: "b/SKILL.md", ContentDigest: "sha256:same"}
	_, ambiguous := MatchIdentities([]Candidate{a, b})
	if len(ambiguous) != 0 {
		t.Errorf("ambiguous = %+v, want none for non-mcp_server concepts", ambiguous)
	}
}
