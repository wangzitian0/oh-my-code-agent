package effective

import (
	"math/rand"
	"reflect"
	"testing"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
)

// shuffledObservations mirrors internal/resolve/determinism_test.go and
// internal/drift/determinism_test.go's technique: reorder the input and
// prove the output is unaffected.
func shuffledObservations(observations []domain.Observation, r *rand.Rand) []domain.Observation {
	out := append([]domain.Observation(nil), observations...)
	r.Shuffle(len(out), func(i, j int) { out[i], out[j] = out[j], out[i] })
	return out
}

// TestComputeEffectiveGraph_Deterministic is this package's property test,
// matching internal/resolve.TestResolve_Deterministic and
// internal/drift.TestGroup_Deterministic's rigor: ComputeEffectiveGraph must
// be a pure function of the *set* of input Observations, not their order,
// and Options' map-typed fields must not leak Go's randomized map iteration
// order into the result.
func TestComputeEffectiveGraph_Deterministic(t *testing.T) {
	userContent := map[string]any{"mcpServers": map[string]any{
		"shared-tools":     map[string]any{"command": "npx", "args": []any{"-y", "@example/shared-tools-server"}},
		"user-only-server": map[string]any{"command": "npx", "args": []any{"-y", "@example/user-only-server"}},
	}}
	projectContent := map[string]any{"mcpServers": map[string]any{
		"shared-tools":        map[string]any{"command": "./scripts/run.sh"},
		"project-only-server": map[string]any{"command": "npx", "args": []any{"-y", "@example/project-only-server"}},
	}}
	base := []domain.Observation{
		obs("mcp_server", "claude-config/.claude.json", "user", "claude-config", map[string]any{"content": userContent}),
		obs("mcp_server", "project/.mcp.json", "workspace", "project", map[string]any{"content": projectContent}),
		obs("instruction", "home/CLAUDE.md", "user", "home", nil),
		obs("instruction", "project/CLAUDE.md", "workspace", "project", nil),
		obs("skill", "home/skills/deploy/SKILL.md", "user", "home", nil),
		obs("skill", "project/.claude/skills/deploy/SKILL.md", "workspace", "project", nil),
	}

	buildScopeRankAsc := func() map[string]int {
		m := map[string]int{}
		m["user"] = 1
		m["workspace"] = 2
		return m
	}
	buildScopeRankDesc := func() map[string]int {
		m := map[string]int{}
		m["workspace"] = 2
		m["user"] = 1
		return m
	}

	hk := domain.HostKnowledge{
		Capabilities: map[string]domain.CapabilityOps{
			"mcp_server":  {Resolve: domain.CapabilityExact},
			"instruction": {Resolve: domain.CapabilityUnknown},
			"skill":       {Resolve: domain.CapabilityExact},
		},
		PrecedencePrograms: []domain.PrecedenceProgram{
			{ID: "mcp_server.union-by-id", Operator: "UNION_BY_ID"},
			{ID: "instruction.concat-ordered", Operator: "CONCAT_ORDERED"},
			{ID: "skill.replace-by-scope", Operator: "REPLACE"},
		},
	}

	r := rand.New(rand.NewSource(11))
	const iterations = 25
	var graphs []EffectiveGraph
	var digests []string
	for i := 0; i < iterations; i++ {
		observations := shuffledObservations(base, r)
		scopeRank := buildScopeRankAsc()
		if i%2 == 1 {
			scopeRank = buildScopeRankDesc()
		}
		graph, err := ComputeEffectiveGraph("claude-code", "2.1.211", observations, hk, Options{ScopeRank: scopeRank}, nil)
		if err != nil {
			t.Fatalf("iteration %d: ComputeEffectiveGraph: %v", i, err)
		}
		graphs = append(graphs, graph)

		digest, err := domain.CanonicalDigest(graph)
		if err != nil {
			t.Fatalf("iteration %d: CanonicalDigest: %v", i, err)
		}
		digests = append(digests, digest)
	}

	for i := 1; i < len(graphs); i++ {
		if !reflect.DeepEqual(graphs[0], graphs[i]) {
			t.Fatalf("run 0 and run %d produced different EffectiveGraph for the same logical input:\n%+v\nvs\n%+v", i, graphs[0], graphs[i])
		}
		if digests[0] != digests[i] {
			t.Fatalf("run 0 and run %d produced different digests: %s vs %s", i, digests[0], digests[i])
		}
	}
}
