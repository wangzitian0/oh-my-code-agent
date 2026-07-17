package effective

import (
	"testing"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
)

func TestComputeEffectiveGraph_FiltersToHost(t *testing.T) {
	observations := []domain.Observation{
		obs("instruction", "home/CLAUDE.md", "user", "home", nil),
	}
	observations[0].Spec.Host.ID = "codex" // a different host

	graph, err := ComputeEffectiveGraph("claude-code", "2.1.211", observations, domain.HostKnowledge{}, Options{}, nil)
	if err != nil {
		t.Fatalf("ComputeEffectiveGraph: %v", err)
	}
	if len(graph.Entries) != 0 || len(graph.Conflicts) != 0 {
		t.Errorf("graph = %+v, want empty (observation belongs to a different host)", graph)
	}
}

func TestComputeEffectiveGraph_InvalidHost_Errors(t *testing.T) {
	if _, err := ComputeEffectiveGraph("not-a-real-host", "1.0", nil, domain.HostKnowledge{}, Options{}, nil); err == nil {
		t.Error("want an error for an invalid host ID")
	}
}

func TestComputeEffectiveGraph_MCPCollision_UnqualifiedPack_ProducesConflict(t *testing.T) {
	userContent := map[string]any{"mcpServers": map[string]any{
		"shared-tools":     map[string]any{"command": "npx", "args": []any{"-y", "@example/shared-tools-server"}},
		"user-only-server": map[string]any{"command": "npx", "args": []any{"-y", "@example/user-only-server"}},
	}}
	projectContent := map[string]any{"mcpServers": map[string]any{
		"shared-tools":        map[string]any{"command": "./scripts/run.sh"},
		"project-only-server": map[string]any{"command": "npx", "args": []any{"-y", "@example/project-only-server"}},
	}}
	observations := []domain.Observation{
		obs("mcp_server", "claude-config/.claude.json", "user", "claude-config", map[string]any{"content": userContent}),
		obs("mcp_server", "project/.mcp.json", "workspace", "project", map[string]any{"content": projectContent}),
	}

	hk := domain.HostKnowledge{
		Capabilities: map[string]domain.CapabilityOps{
			"mcp_server": {Resolve: domain.CapabilityUnknown},
		},
		PrecedencePrograms: []domain.PrecedenceProgram{
			{ID: "mcp_server.union-by-id", Operator: "UNION_BY_ID"},
		},
	}

	graph, err := ComputeEffectiveGraph("claude-code", "2.1.211", observations, hk, Options{}, nil)
	if err != nil {
		t.Fatalf("ComputeEffectiveGraph: %v", err)
	}

	if !graph.HasConflict("mcp_server", "stdio|shared-tools") {
		t.Errorf("conflicts = %+v, want shared-tools unresolved (genuine content collision, unqualified capability)", graph.Conflicts)
	}
	if _, ok := graph.Find("mcp_server", "stdio|user-only-server"); !ok {
		t.Error("user-only-server should resolve trivially (only one physical source)")
	}
	if _, ok := graph.Find("mcp_server", "stdio|project-only-server"); !ok {
		t.Error("project-only-server should resolve trivially (only one physical source)")
	}
}

func TestComputeEffectiveGraph_InstructionComposition(t *testing.T) {
	observations := []domain.Observation{
		obs("instruction", "home/CLAUDE.md", "user", "home", nil),
		obs("instruction", "project/CLAUDE.md", "workspace", "project", nil),
	}
	hk := domain.HostKnowledge{
		Capabilities: map[string]domain.CapabilityOps{
			"instruction": {Resolve: domain.CapabilityUnknown},
		},
		PrecedencePrograms: []domain.PrecedenceProgram{
			{ID: "instruction.concat-ordered", Operator: "CONCAT_ORDERED"},
		},
	}
	graph, err := ComputeEffectiveGraph("claude-code", "2.1.211", observations, hk, Options{}, nil)
	if err != nil {
		t.Fatalf("ComputeEffectiveGraph: %v", err)
	}
	entries := graph.ByConcept("instruction")
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want exactly 1 composition entry for instruction", len(entries))
	}
	if !entries[0].Composed {
		t.Error("Composed = false, want true")
	}
	if len(entries[0].Provenance.ActiveSources) != 2 {
		t.Errorf("ActiveSources = %v, want both", entries[0].Provenance.ActiveSources)
	}
}

func TestComputeEffectiveGraph_DuplicateCapabilities(t *testing.T) {
	content := map[string]any{"mcpServers": map[string]any{
		"docs": map[string]any{"command": "docs-server", "tools": []any{"web_search"}},
	}}
	observations := []domain.Observation{
		obs("mcp_server", "project/.mcp.json", "workspace", "project", map[string]any{"content": content}),
	}
	extra := []ToolSource{{Kind: ToolSourceBuiltin, Owner: "claude-code", Ref: "builtin://web_search", Tool: "web_search"}}

	graph, err := ComputeEffectiveGraph("claude-code", "2.1.211", observations, domain.HostKnowledge{}, Options{}, extra)
	if err != nil {
		t.Fatalf("ComputeEffectiveGraph: %v", err)
	}
	if len(graph.DuplicateCapabilities) != 1 {
		t.Fatalf("DuplicateCapabilities = %+v, want exactly 1", graph.DuplicateCapabilities)
	}
}
