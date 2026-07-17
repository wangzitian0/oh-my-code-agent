package effective

import (
	"strings"
	"testing"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
)

func cand(ref, logicalID, scopeKind string, content string) Candidate {
	return Candidate{
		Concept:       "mcp_server",
		LogicalID:     logicalID,
		Ref:           ref,
		Scope:         domain.ObservationScope{Kind: scopeKind},
		Source:        domain.ObservationSource{Kind: "file", Path: ref},
		EvidenceLevel: domain.EvidenceLevelParsed,
		ContentDigest: content,
	}
}

func qualifiedPack(concept, operator string) (domain.HostKnowledge, domain.CapabilityOps) {
	hk := domain.HostKnowledge{
		PrecedencePrograms: []domain.PrecedenceProgram{
			{ID: concept + ".test-program", Operator: operator},
		},
	}
	return hk, domain.CapabilityOps{Resolve: domain.CapabilityExact}
}

// --- Trivial resolution (no program needed at all) ---

func TestResolveGroup_SingleCandidate_TriviallyResolved(t *testing.T) {
	group := LogicalGroup{Concept: "mcp_server", LogicalID: "stdio|solo", Candidates: []Candidate{cand("a.json#mcpServers.solo", "stdio|solo", "user", "digest-1")}}
	entry, conflict := ResolveGroup(group, domain.HostKnowledge{}, domain.CapabilityOps{}, Options{})
	if conflict != nil {
		t.Fatalf("unexpected conflict: %+v", conflict)
	}
	if entry.Provenance.SelectedSource != "a.json#mcpServers.solo" {
		t.Errorf("SelectedSource = %q", entry.Provenance.SelectedSource)
	}
}

func TestResolveGroup_IdenticalContentAcrossSources_TriviallyResolved(t *testing.T) {
	group := LogicalGroup{Concept: "mcp_server", LogicalID: "stdio|dup", Candidates: []Candidate{
		cand("a.json#mcpServers.dup", "stdio|dup", "user", "same-digest"),
		cand("b.json#mcpServers.dup", "stdio|dup", "workspace", "same-digest"),
	}}
	entry, conflict := ResolveGroup(group, domain.HostKnowledge{}, domain.CapabilityOps{}, Options{})
	if conflict != nil {
		t.Fatalf("unexpected conflict for byte-identical content: %+v", conflict)
	}
	if len(entry.Provenance.ActiveSources) != 2 {
		t.Errorf("ActiveSources = %v, want both sources active", entry.Provenance.ActiveSources)
	}
}

// --- The required "UNKNOWN operator never guessed" acceptance test ---

func TestResolveGroup_NoProgram_UnresolvedConflict_NeverGuessed(t *testing.T) {
	group := LogicalGroup{Concept: "mcp_server", LogicalID: "stdio|shared", Candidates: []Candidate{
		cand("user/.claude.json#mcpServers.shared", "stdio|shared", "user", "digest-a"),
		cand("project/.mcp.json#mcpServers.shared", "stdio|shared", "workspace", "digest-b"),
	}}
	// No PrecedenceProgram declared at all.
	entry, conflict := ResolveGroup(group, domain.HostKnowledge{}, domain.CapabilityOps{Resolve: domain.CapabilityExact}, Options{})
	if conflict == nil {
		t.Fatalf("entry = %+v, want a Conflict when no precedence program is declared", entry)
	}
	if conflict.Reason == "" {
		t.Error("Conflict.Reason must explain why, not just flag it silently")
	}
}

func TestResolveGroup_UnknownOperatorLiteral_UnresolvedConflict_NeverGuessed(t *testing.T) {
	group := LogicalGroup{Concept: "mcp_server", LogicalID: "stdio|shared", Candidates: []Candidate{
		cand("user/.claude.json#mcpServers.shared", "stdio|shared", "user", "digest-a"),
		cand("project/.mcp.json#mcpServers.shared", "stdio|shared", "workspace", "digest-b"),
	}}
	hk, capOps := qualifiedPack("mcp_server", "UNKNOWN")
	entry, conflict := ResolveGroup(group, hk, capOps, Options{})
	if conflict == nil {
		t.Fatalf("entry = %+v, want a Conflict for a literal UNKNOWN operator", entry)
	}
	if !strings.Contains(conflict.Reason, "no usable precedence program") {
		t.Errorf("Reason = %q, want it to explain the operator is not usable", conflict.Reason)
	}
}

func TestResolveGroup_UnspecifiedOperator_UnresolvedConflict(t *testing.T) {
	group := LogicalGroup{Concept: "mcp_server", LogicalID: "stdio|shared", Candidates: []Candidate{
		cand("a", "stdio|shared", "user", "digest-a"),
		cand("b", "stdio|shared", "workspace", "digest-b"),
	}}
	hk, capOps := qualifiedPack("mcp_server", "UNSPECIFIED")
	_, conflict := ResolveGroup(group, hk, capOps, Options{})
	if conflict == nil {
		t.Fatal("want a Conflict for UNSPECIFIED (vendor does not define conflict resolution)")
	}
}

func TestResolveGroup_ValidOperator_UnqualifiedCapability_UnresolvedConflict(t *testing.T) {
	// A real, documented operator (REPLACE) but the Pack has not qualified
	// resolve for this concept -- must still refuse to guess, matching
	// today's real Knowledge Packs (every concept's resolve capability is
	// UNKNOWN).
	group := LogicalGroup{Concept: "skill", LogicalID: "deploy|file", Candidates: []Candidate{
		cand("user/skills/deploy/SKILL.md", "deploy|file", "user", "digest-a"),
		cand("project/.claude/skills/deploy/SKILL.md", "deploy|file", "workspace", "digest-b"),
	}}
	hk := domain.HostKnowledge{PrecedencePrograms: []domain.PrecedenceProgram{{ID: "skill.replace-by-scope", Operator: "REPLACE"}}}
	capOps := domain.CapabilityOps{Resolve: domain.CapabilityUnknown}
	_, conflict := ResolveGroup(group, hk, capOps, Options{})
	if conflict == nil {
		t.Fatal("want a Conflict when resolve capability is not EXACT/COMPATIBLE, even with a named operator")
	}
	if !strings.Contains(conflict.Reason, "resolve capability") {
		t.Errorf("Reason = %q, want it to name the capability gate", conflict.Reason)
	}
}

// --- Real operator semantics, once qualified ---

func TestResolveGroup_Replace_WithScopeOrder(t *testing.T) {
	group := LogicalGroup{Concept: "skill", LogicalID: "deploy|file", Candidates: []Candidate{
		cand("user/skills/deploy/SKILL.md", "deploy|file", "user", "digest-a"),
		cand("project/.claude/skills/deploy/SKILL.md", "deploy|file", "workspace", "digest-b"),
	}}
	hk, capOps := qualifiedPack("skill", "REPLACE")
	entry, conflict := ResolveGroup(group, hk, capOps, Options{ScopeRank: map[string]int{"user": 1, "workspace": 2}})
	if conflict != nil {
		t.Fatalf("unexpected conflict: %+v", conflict)
	}
	if entry.Provenance.SelectedSource != "project/.claude/skills/deploy/SKILL.md" {
		t.Errorf("SelectedSource = %q, want the higher-ranked workspace source to win", entry.Provenance.SelectedSource)
	}
	if len(entry.Provenance.IgnoredSources) != 1 {
		t.Errorf("IgnoredSources = %v, want the user source ignored", entry.Provenance.IgnoredSources)
	}
}

func TestResolveGroup_Replace_TiedRank_Unresolved(t *testing.T) {
	group := LogicalGroup{Concept: "skill", LogicalID: "deploy|file", Candidates: []Candidate{
		cand("a", "deploy|file", "user", "digest-a"),
		cand("b", "deploy|file", "profile", "digest-b"),
	}}
	hk, capOps := qualifiedPack("skill", "REPLACE")
	_, conflict := ResolveGroup(group, hk, capOps, Options{ScopeRank: map[string]int{"user": 1, "profile": 1}})
	if conflict == nil {
		t.Fatal("want a Conflict for a tied scope rank")
	}
}

func TestResolveGroup_UnionByID_ContentCollision_WithScopeOrder(t *testing.T) {
	group := LogicalGroup{Concept: "mcp_server", LogicalID: "stdio|shared", Candidates: []Candidate{
		cand("user/.claude.json#mcpServers.shared", "stdio|shared", "user", "digest-a"),
		cand("project/.mcp.json#mcpServers.shared", "stdio|shared", "workspace", "digest-b"),
	}}
	hk, capOps := qualifiedPack("mcp_server", "UNION_BY_ID")
	entry, conflict := ResolveGroup(group, hk, capOps, Options{ScopeRank: map[string]int{"user": 2, "workspace": 1}})
	if conflict != nil {
		t.Fatalf("unexpected conflict: %+v", conflict)
	}
	if entry.Provenance.SelectedSource != "user/.claude.json#mcpServers.shared" {
		t.Errorf("SelectedSource = %q, want the higher-ranked user source", entry.Provenance.SelectedSource)
	}
}

func TestResolveGroup_DeepMerge_NonConflictingLeavesMergeCleanly(t *testing.T) {
	a := Candidate{Concept: "mcp_server", LogicalID: "stdio|x", Ref: "a", Scope: domain.ObservationScope{Kind: "user"}, ContentDigest: "digest-a",
		Fields: map[string]any{"command": "tool", "env": map[string]any{"A": "1"}}}
	b := Candidate{Concept: "mcp_server", LogicalID: "stdio|x", Ref: "b", Scope: domain.ObservationScope{Kind: "workspace"}, ContentDigest: "digest-b",
		Fields: map[string]any{"args": []any{"--flag"}, "env": map[string]any{"B": "2"}}}
	group := LogicalGroup{Concept: "mcp_server", LogicalID: "stdio|x", Candidates: []Candidate{a, b}}
	hk, capOps := qualifiedPack("mcp_server", "DEEP_MERGE")
	entry, conflict := ResolveGroup(group, hk, capOps, Options{})
	if conflict != nil {
		t.Fatalf("unexpected conflict merging non-overlapping fields: %+v", conflict)
	}
	if entry.Provenance.Operator != "DEEP_MERGE" {
		t.Errorf("Operator = %q", entry.Provenance.Operator)
	}
}

func TestResolveGroup_DeepMerge_LeafConflict_NoScopeOrder_Unresolved(t *testing.T) {
	a := Candidate{Concept: "mcp_server", LogicalID: "stdio|x", Ref: "a", Scope: domain.ObservationScope{Kind: "user"}, ContentDigest: "digest-a",
		Fields: map[string]any{"command": "tool-a"}}
	b := Candidate{Concept: "mcp_server", LogicalID: "stdio|x", Ref: "b", Scope: domain.ObservationScope{Kind: "workspace"}, ContentDigest: "digest-b",
		Fields: map[string]any{"command": "tool-b"}}
	group := LogicalGroup{Concept: "mcp_server", LogicalID: "stdio|x", Candidates: []Candidate{a, b}}
	hk, capOps := qualifiedPack("mcp_server", "DEEP_MERGE")
	_, conflict := ResolveGroup(group, hk, capOps, Options{})
	if conflict == nil {
		t.Fatal("want a Conflict for a genuine leaf conflict with no scope order")
	}
}

func TestResolveGroup_DeepMerge_LeafConflict_WithScopeOrder_Resolved(t *testing.T) {
	a := Candidate{Concept: "mcp_server", LogicalID: "stdio|x", Ref: "a", Scope: domain.ObservationScope{Kind: "user"}, ContentDigest: "digest-a",
		Fields: map[string]any{"command": "tool-a"}}
	b := Candidate{Concept: "mcp_server", LogicalID: "stdio|x", Ref: "b", Scope: domain.ObservationScope{Kind: "workspace"}, ContentDigest: "digest-b",
		Fields: map[string]any{"command": "tool-b"}}
	group := LogicalGroup{Concept: "mcp_server", LogicalID: "stdio|x", Candidates: []Candidate{a, b}}
	hk, capOps := qualifiedPack("mcp_server", "DEEP_MERGE")
	entry, conflict := ResolveGroup(group, hk, capOps, Options{ScopeRank: map[string]int{"user": 1, "workspace": 2}})
	if conflict != nil {
		t.Fatalf("unexpected conflict: %+v", conflict)
	}
	if entry.Provenance.Operator != "DEEP_MERGE" {
		t.Errorf("Operator = %q", entry.Provenance.Operator)
	}
}

func TestResolveGroup_FirstMatch_WithPriority(t *testing.T) {
	group := LogicalGroup{Concept: "instruction", LogicalID: "x", Candidates: []Candidate{
		cand("home/AGENTS.md", "x", "user", "digest-a"),
		cand("project/AGENTS.md", "x", "workspace", "digest-b"),
	}}
	hk, capOps := qualifiedPack("instruction", "FIRST_MATCH")
	entry, conflict := ResolveGroup(group, hk, capOps, Options{ScopePriority: []string{"workspace", "user"}})
	if conflict != nil {
		t.Fatalf("unexpected conflict: %+v", conflict)
	}
	if entry.Provenance.SelectedSource != "project/AGENTS.md" {
		t.Errorf("SelectedSource = %q, want workspace (first in priority) to win", entry.Provenance.SelectedSource)
	}
}

func TestResolveGroup_FirstMatch_NoPriority_Unresolved(t *testing.T) {
	group := LogicalGroup{Concept: "instruction", LogicalID: "x", Candidates: []Candidate{
		cand("a", "x", "user", "digest-a"),
		cand("b", "x", "workspace", "digest-b"),
	}}
	hk, capOps := qualifiedPack("instruction", "FIRST_MATCH")
	_, conflict := ResolveGroup(group, hk, capOps, Options{})
	if conflict == nil {
		t.Fatal("want a Conflict when no priority order is supplied")
	}
}

func TestResolveGroup_Namespace_AllRemainActive(t *testing.T) {
	group := LogicalGroup{Concept: "skill", LogicalID: "deploy|file", Candidates: []Candidate{
		cand("home/.agents/skills/deploy/SKILL.md", "deploy|file", "user", "digest-a"),
		cand("project/.agents/skills/deploy/SKILL.md", "deploy|file", "workspace", "digest-b"),
		cand("codex-home/skills/deploy/SKILL.md", "deploy|file", "user", "digest-c"),
	}}
	hk, capOps := qualifiedPack("skill", "NAMESPACE")
	entry, conflict := ResolveGroup(group, hk, capOps, Options{})
	if conflict != nil {
		t.Fatalf("unexpected conflict: %+v", conflict)
	}
	if len(entry.Provenance.ActiveSources) != 3 {
		t.Errorf("ActiveSources = %v, want all 3 to remain active under NAMESPACE", entry.Provenance.ActiveSources)
	}
	if len(entry.Provenance.IgnoredSources) != 0 {
		t.Errorf("IgnoredSources = %v, want none (NAMESPACE never shadows)", entry.Provenance.IgnoredSources)
	}
}

func TestResolveGroup_DenyWins_ExcludesDenied(t *testing.T) {
	a := cand("a", "x", "user", "digest-a")
	b := cand("b", "x", "workspace", "digest-b")
	group := LogicalGroup{Concept: "mcp_server", LogicalID: "x", Candidates: []Candidate{a, b}}
	hk, capOps := qualifiedPack("mcp_server", "DENY_WINS")
	entry, conflict := ResolveGroup(group, hk, capOps, Options{DeniedRefs: map[string]bool{"a": true}})
	if conflict != nil {
		t.Fatalf("unexpected conflict: %+v", conflict)
	}
	if entry.Provenance.SelectedSource != "b" {
		t.Errorf("SelectedSource = %q, want the non-denied survivor", entry.Provenance.SelectedSource)
	}
}

func TestResolveGroup_DenyWins_AllDenied(t *testing.T) {
	// A single-candidate group is trivially resolved before the operator
	// even runs (nothing to adjudicate), so this exercises DENY_WINS's own
	// all-denied path directly against a genuine multi-candidate collision.
	a := cand("a", "x", "user", "digest-a")
	b := cand("b", "x", "workspace", "digest-b")
	hk, capOps := qualifiedPack("mcp_server", "DENY_WINS")
	group := LogicalGroup{Concept: "mcp_server", LogicalID: "x", Candidates: []Candidate{a, b}}
	entry, conflict := ResolveGroup(group, hk, capOps, Options{DeniedRefs: map[string]bool{"a": true, "b": true}})
	if conflict != nil {
		t.Fatalf("unexpected conflict: %+v", conflict)
	}
	if entry.Provenance.SelectedSource != "" {
		t.Errorf("SelectedSource = %q, want none selected when everything is denied", entry.Provenance.SelectedSource)
	}
}

func TestResolveGroup_ManagedGuardrail_ManagedSourceWins(t *testing.T) {
	managed := cand("managed/policy.json", "x", "managed", "digest-managed")
	user := cand("user/config.json", "x", "user", "digest-user")
	group := LogicalGroup{Concept: "mcp_server", LogicalID: "x", Candidates: []Candidate{managed, user}}
	hk, capOps := qualifiedPack("mcp_server", "MANAGED_GUARDRAIL")
	entry, conflict := ResolveGroup(group, hk, capOps, Options{})
	if conflict != nil {
		t.Fatalf("unexpected conflict: %+v", conflict)
	}
	if entry.Provenance.SelectedSource != "managed/policy.json" {
		t.Errorf("SelectedSource = %q, want the managed source to constrain the result", entry.Provenance.SelectedSource)
	}
}

func TestResolveGroup_ManagedGuardrail_NoManagedSource_Unresolved(t *testing.T) {
	user := cand("user/config.json", "x", "user", "digest-user")
	workspace := cand("project/config.json", "x", "workspace", "digest-workspace")
	group := LogicalGroup{Concept: "mcp_server", LogicalID: "x", Candidates: []Candidate{user, workspace}}
	hk, capOps := qualifiedPack("mcp_server", "MANAGED_GUARDRAIL")
	_, conflict := ResolveGroup(group, hk, capOps, Options{})
	if conflict == nil {
		t.Fatal("want a Conflict when no managed-scope source is present to apply the guardrail")
	}
}
