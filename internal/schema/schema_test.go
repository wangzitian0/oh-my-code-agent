package schema_test

import (
	"testing"

	"github.com/wangzitian0/oh-my-code-agent/internal/schema"
)

func TestSchema_PackageLevelLookup(t *testing.T) {
	for _, id := range []string{"skill", "instruction", "mcp_server"} {
		c, ok := schema.Concept(id)
		if !ok {
			t.Fatalf("expected schema.Concept(%q) to be known in default registry", id)
		}
		if c.ID != id {
			t.Fatalf("expected concept ID %q, got %q", id, c.ID)
		}
		if len(c.CanonicalFields) == 0 {
			t.Fatalf("expected concept %q to have canonical fields", id)
		}
	}
}

func TestSchema_ValidateMergeOperator(t *testing.T) {
	if err := schema.ValidateMergeOperator(schema.OpReplace); err != nil {
		t.Fatalf("unexpected error for OpReplace: %v", err)
	}
	if err := schema.ValidateMergeOperator(schema.MergeOperator("INVALID_OP")); err == nil {
		t.Fatal("expected error for invalid merge operator, got nil")
	}
}
