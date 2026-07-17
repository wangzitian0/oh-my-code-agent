package effective

import (
	"testing"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
)

func TestLookupProgram_Found(t *testing.T) {
	hk := domain.HostKnowledge{
		PrecedencePrograms: []domain.PrecedenceProgram{
			{ID: "instruction.concat-ordered", Operator: "CONCAT_ORDERED"},
			{ID: "mcp_server.union-by-id", Operator: "UNION_BY_ID"},
		},
	}
	p, ok := LookupProgram(hk, "instruction")
	if !ok {
		t.Fatal("expected a program for instruction")
	}
	if p.Operator != "CONCAT_ORDERED" {
		t.Errorf("Operator = %q, want CONCAT_ORDERED", p.Operator)
	}
}

func TestLookupProgram_NotDeclared(t *testing.T) {
	hk := domain.HostKnowledge{}
	if _, ok := LookupProgram(hk, "skill"); ok {
		t.Error("expected no program for an empty Pack")
	}
}

func TestLookupProgram_AmbiguousDuplicateIsNotUsable(t *testing.T) {
	hk := domain.HostKnowledge{
		PrecedencePrograms: []domain.PrecedenceProgram{
			{ID: "skill.replace-by-scope", Operator: "REPLACE"},
			{ID: "skill.namespace-by-source", Operator: "NAMESPACE"},
		},
	}
	if _, ok := LookupProgram(hk, "skill"); ok {
		t.Error("expected two programs matching one concept to be unusable, not silently picked between")
	}
}

func TestLookupProgram_PrefixDoesNotFalseMatch(t *testing.T) {
	// "mcp_server." must not match a program declared for "mcp_server_extra"
	// or similar prefix collisions.
	hk := domain.HostKnowledge{
		PrecedencePrograms: []domain.PrecedenceProgram{
			{ID: "mcp_server_extra.something", Operator: "REPLACE"},
		},
	}
	if _, ok := LookupProgram(hk, "mcp_server"); ok {
		t.Error("expected no false-positive prefix match")
	}
}
