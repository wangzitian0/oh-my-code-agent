package effective

import (
	"testing"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
)

func obs(concept, path, scopeKind, scopeRoot string, opaque map[string]any) domain.Observation {
	return domain.Observation{
		APIVersion: domain.SupportedAPIVersion,
		Kind:       "Observation",
		Metadata:   domain.Metadata{ID: "claude-code:" + concept + ":" + path},
		Spec: domain.ObservationSpec{
			Host:               domain.ObservationHost{ID: "claude-code", Version: "2.1.211"},
			Surface:            "cli",
			Concept:            concept,
			Source:             domain.ObservationSource{Kind: "file", Path: path, Digest: "sha256:" + path},
			Scope:              domain.ObservationScope{Kind: scopeKind, Root: scopeRoot},
			Disposition:        domain.DispositionDiscovered,
			EvidenceLevel:      domain.EvidenceLevelParsed,
			RawDigest:          "sha256:raw-" + path,
			ParsedDigest:       "sha256:parsed-" + path,
			OpaqueVendorFields: opaque,
		},
	}
}

func TestExtractCandidates_Instruction_OnePerFile(t *testing.T) {
	observations := []domain.Observation{
		obs("instruction", "home/CLAUDE.md", "user", "home", nil),
		obs("instruction", "project/CLAUDE.md", "workspace", "project", nil),
	}
	cands, err := ExtractCandidates(observations)
	if err != nil {
		t.Fatalf("ExtractCandidates: %v", err)
	}
	if len(cands) != 2 {
		t.Fatalf("candidates = %d, want 2", len(cands))
	}
}

func TestExtractCandidates_Skill_NameFromParentDirectory(t *testing.T) {
	observations := []domain.Observation{
		obs("skill", "project/.claude/skills/deploy/SKILL.md", "workspace", "project", nil),
	}
	cands, err := ExtractCandidates(observations)
	if err != nil {
		t.Fatalf("ExtractCandidates: %v", err)
	}
	if len(cands) != 1 {
		t.Fatalf("candidates = %d, want 1", len(cands))
	}
	if cands[0].LogicalID != "deploy|file" {
		t.Errorf("LogicalID = %q, want %q", cands[0].LogicalID, "deploy|file")
	}
}

func TestExtractCandidates_MCPServer_JSON_SplitsPerServer(t *testing.T) {
	content := map[string]any{
		"mcpServers": map[string]any{
			"shared-tools": map[string]any{"command": "npx", "args": []any{"-y", "@example/shared"}},
			"solo-server":  map[string]any{"command": "npx", "args": []any{"-y", "@example/solo"}},
		},
	}
	observations := []domain.Observation{
		obs("mcp_server", "claude-config/.claude.json", "user", "claude-config", map[string]any{"content": content}),
	}
	cands, err := ExtractCandidates(observations)
	if err != nil {
		t.Fatalf("ExtractCandidates: %v", err)
	}
	if len(cands) != 2 {
		t.Fatalf("candidates = %d, want 2 (one per server entry)", len(cands))
	}
	byLogicalID := map[string]Candidate{}
	for _, c := range cands {
		byLogicalID[c.LogicalID] = c
	}
	if _, ok := byLogicalID["stdio|shared-tools"]; !ok {
		t.Errorf("missing shared-tools candidate, got %+v", byLogicalID)
	}
	if _, ok := byLogicalID["stdio|solo-server"]; !ok {
		t.Errorf("missing solo-server candidate, got %+v", byLogicalID)
	}
	want := "claude-config/.claude.json#mcpServers.shared-tools"
	if byLogicalID["stdio|shared-tools"].Ref != want {
		t.Errorf("Ref = %q, want %q", byLogicalID["stdio|shared-tools"].Ref, want)
	}
}

func TestExtractCandidates_MCPServer_TOML_ScrapesTables(t *testing.T) {
	toml := `# comment
[mcp_servers.shared-tools]
command = "npx"
args = ["-y", "@example/shared-tools-server"]

[mcp_servers.user-only-server]
command = "npx"
args = ["-y", "@example/user-only-server"]
`
	observations := []domain.Observation{
		obs("mcp_server", "codex-home/config.toml", "user", "codex-home", map[string]any{"content": toml}),
	}
	cands, err := ExtractCandidates(observations)
	if err != nil {
		t.Fatalf("ExtractCandidates: %v", err)
	}
	if len(cands) != 2 {
		t.Fatalf("candidates = %d, want 2", len(cands))
	}
	byLogicalID := map[string]Candidate{}
	for _, c := range cands {
		byLogicalID[c.LogicalID] = c
	}
	if _, ok := byLogicalID["stdio|shared-tools"]; !ok {
		t.Errorf("missing shared-tools candidate, got %+v", byLogicalID)
	}
	if _, ok := byLogicalID["stdio|user-only-server"]; !ok {
		t.Errorf("missing user-only-server candidate, got %+v", byLogicalID)
	}
}

func TestExtractCandidates_MCPServer_NoTableFound_WholeFileFallback(t *testing.T) {
	observations := []domain.Observation{
		obs("mcp_server", "project/.mcp.json", "workspace", "project", map[string]any{"content": map[string]any{"unrelated": "value"}}),
	}
	cands, err := ExtractCandidates(observations)
	if err != nil {
		t.Fatalf("ExtractCandidates: %v", err)
	}
	if len(cands) != 1 {
		t.Fatalf("candidates = %d, want 1 (whole-file fallback, never silently dropped)", len(cands))
	}
	if cands[0].Ref != "project/.mcp.json" {
		t.Errorf("Ref = %q", cands[0].Ref)
	}
}

func TestExtractCandidates_MCPServer_DiscoveredOnly_WholeFileFallback(t *testing.T) {
	// An E0 (discovered-only) Observation has no OpaqueVendorFields content
	// this package recognizes as parseable -- must still produce exactly
	// one Candidate, never silently vanish.
	observations := []domain.Observation{
		obs("mcp_server", "codex-home/config.toml", "user", "codex-home", nil),
	}
	cands, err := ExtractCandidates(observations)
	if err != nil {
		t.Fatalf("ExtractCandidates: %v", err)
	}
	if len(cands) != 1 {
		t.Fatalf("candidates = %d, want 1", len(cands))
	}
}

func TestExtractCandidates_UnknownConcept_Skipped(t *testing.T) {
	observations := []domain.Observation{
		obs("hook", "project/.claude/settings.json", "workspace", "project", nil),
	}
	cands, err := ExtractCandidates(observations)
	if err != nil {
		t.Fatalf("ExtractCandidates: %v", err)
	}
	if len(cands) != 0 {
		t.Errorf("candidates = %+v, want none for a concept this package does not extract", cands)
	}
}
