package effective

import "testing"

func TestFingerprint_NormalizesAcrossNamingConventions(t *testing.T) {
	cases := [][2]string{
		{"web_search", "web-search"},
		{"WebSearch", "web search"},
	}
	for _, c := range cases {
		if Fingerprint(c[0]) != Fingerprint(c[1]) {
			t.Errorf("Fingerprint(%q)=%q != Fingerprint(%q)=%q, want equal", c[0], Fingerprint(c[0]), c[1], Fingerprint(c[1]))
		}
	}
	if Fingerprint("web_search") == Fingerprint("websearch_v2") {
		t.Error("genuinely different tool names must not fingerprint identically")
	}
}

// TestDetectDuplicateCapabilities_OneToolTwoTransports_Flagged is the
// required round-2-audit golden: a fixture exposing one logical tool
// ("web_search") through two transports -- a native built-in and an
// MCP-registered server -- must be flagged before launch
// (docs/ontology/README.md §8: "Same-brand connector, MCP server, plugin
// tool, and built-in tool are separate sources. Duplicate logical
// capabilities must be shown before launch.").
func TestDetectDuplicateCapabilities_OneToolTwoTransports_Flagged(t *testing.T) {
	sources := []ToolSource{
		{Kind: ToolSourceBuiltin, Owner: "claude-code", Ref: "builtin://web_search", Tool: "web_search"},
		{Kind: ToolSourceMCP, Owner: "stdio|docs-server", Ref: "project/.mcp.json#mcpServers.docs-server", Tool: "web-search"},
		{Kind: ToolSourceMCP, Owner: "stdio|docs-server", Ref: "project/.mcp.json#mcpServers.docs-server", Tool: "fetch_page"},
	}
	dups := DetectDuplicateCapabilities(sources)
	if len(dups) != 1 {
		t.Fatalf("DetectDuplicateCapabilities = %d duplicates, want exactly 1", len(dups))
	}
	dup := dups[0]
	if len(dup.Sources) != 2 {
		t.Fatalf("duplicate sources = %d, want 2 (the builtin and the mcp registration)", len(dup.Sources))
	}
	kinds := map[ToolSourceKind]bool{}
	for _, s := range dup.Sources {
		kinds[s.Kind] = true
	}
	if !kinds[ToolSourceBuiltin] || !kinds[ToolSourceMCP] {
		t.Errorf("duplicate kinds = %v, want both builtin and mcp represented", kinds)
	}
}

func TestDetectDuplicateCapabilities_SameKindCollision_NotFlagged(t *testing.T) {
	// Two different MCP servers both naming a tool "search": an ordinary
	// same-transport name collision the concept's own merge operator
	// governs, not a cross-transport duplicate capability.
	sources := []ToolSource{
		{Kind: ToolSourceMCP, Owner: "stdio|server-a", Ref: "a", Tool: "search"},
		{Kind: ToolSourceMCP, Owner: "stdio|server-b", Ref: "b", Tool: "search"},
	}
	if dups := DetectDuplicateCapabilities(sources); len(dups) != 0 {
		t.Errorf("DetectDuplicateCapabilities = %+v, want none for a same-kind collision", dups)
	}
}

func TestDetectDuplicateCapabilities_NoOverlap_NotFlagged(t *testing.T) {
	sources := []ToolSource{
		{Kind: ToolSourceBuiltin, Owner: "claude-code", Ref: "builtin://a", Tool: "alpha"},
		{Kind: ToolSourceMCP, Owner: "stdio|x", Ref: "x", Tool: "beta"},
		{Kind: ToolSourcePlugin, Owner: "plugin:y", Ref: "y", Tool: "gamma"},
	}
	if dups := DetectDuplicateCapabilities(sources); len(dups) != 0 {
		t.Errorf("DetectDuplicateCapabilities = %+v, want none", dups)
	}
}

func TestMCPToolSources_ProjectsCandidateTools(t *testing.T) {
	c := Candidate{Concept: "mcp_server", LogicalID: "stdio|docs", Ref: "a.json#mcpServers.docs", Tools: []string{"search", "fetch"}}
	sources := MCPToolSources([]Candidate{c})
	if len(sources) != 2 {
		t.Fatalf("sources = %d, want 2", len(sources))
	}
	for _, s := range sources {
		if s.Kind != ToolSourceMCP {
			t.Errorf("Kind = %q, want mcp", s.Kind)
		}
	}
}
