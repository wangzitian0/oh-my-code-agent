package observe

import (
	"reflect"
	"testing"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
)

// TestCoverage_CompleteForBothHosts is issue #20's round-2 acceptance
// criterion, exercised directly: "Concept coverage is explicit and complete
// for both hosts: Instructions, Skills, MCP, Hooks, permissions/trust, and
// Plugins/Extensions." This asserts the full 2-host x 6-concept cross
// product (12 cells) is present, with no duplicates and no extra/unexpected
// cell, so a future edit that silently drops a cell (or forgets to add one
// for a new concept) fails a test rather than only being caught by manual
// review.
func TestCoverage_CompleteForBothHosts(t *testing.T) {
	wantHosts := []string{"codex", "claude-code"}
	wantConcepts := []string{conceptInstruction, conceptSkill, conceptMCPServer, conceptHook, conceptPolicy, conceptPlugin}

	cov := Coverage()
	if len(cov) != len(wantHosts)*len(wantConcepts) {
		t.Fatalf("Coverage() returned %d entries, want %d (%d hosts x %d concepts)", len(cov), len(wantHosts)*len(wantConcepts), len(wantHosts), len(wantConcepts))
	}

	seen := make(map[string]bool, len(cov))
	for _, c := range cov {
		key := c.Host + "/" + c.Concept
		if seen[key] {
			t.Errorf("duplicate coverage cell %s", key)
		}
		seen[key] = true
	}

	for _, h := range wantHosts {
		for _, concept := range wantConcepts {
			key := h + "/" + concept
			if !seen[key] {
				t.Errorf("Coverage() is missing the required cell %s", key)
			}
		}
	}
}

// TestCoverage_EveryDimensionIsAValidCapabilityLevel proves every
// per-dimension value Coverage declares is one of the closed
// domain.CapabilityLevel enum values — a typo (e.g. "EXACTT") would
// silently degrade a report's trustworthiness rather than fail loudly.
func TestCoverage_EveryDimensionIsAValidCapabilityLevel(t *testing.T) {
	for _, c := range Coverage() {
		for dim, level := range map[string]domain.CapabilityLevel{
			"discover":  c.Ops.Discover,
			"parse":     c.Ops.Parse,
			"normalize": c.Ops.Normalize,
			"resolve":   c.Ops.Resolve,
		} {
			if level == "" {
				t.Errorf("%s/%s: %s is empty, want an explicit CapabilityLevel", c.Host, c.Concept, dim)
				continue
			}
			if err := domain.ValidateCapabilityLevel(level); err != nil {
				t.Errorf("%s/%s: %s: %v", c.Host, c.Concept, dim, err)
			}
		}
	}
}

// TestCoverage_NormalizeAndResolveAreNeverClaimed proves this package never
// overstates its own capability: Normalize/Resolve require precedence
// resolution this package explicitly does not perform (doc.go's safety
// property 5 — every Observation is E0 or E1, never E2+), so every cell
// must report CapabilityUnsupported for both, never anything stronger.
func TestCoverage_NormalizeAndResolveAreNeverClaimed(t *testing.T) {
	for _, c := range Coverage() {
		if c.Ops.Normalize != domain.CapabilityUnsupported {
			t.Errorf("%s/%s: Normalize = %s, want %s (this package never resolves precedence)", c.Host, c.Concept, c.Ops.Normalize, domain.CapabilityUnsupported)
		}
		if c.Ops.Resolve != domain.CapabilityUnsupported {
			t.Errorf("%s/%s: Resolve = %s, want %s (this package never resolves precedence)", c.Host, c.Concept, c.Ops.Resolve, domain.CapabilityUnsupported)
		}
	}
}

// TestCoverage_Deterministic proves Coverage()'s output order is stable
// (sorted by Host then Concept), the same determinism bar every other
// exported function in this package holds.
func TestCoverage_Deterministic(t *testing.T) {
	first := Coverage()
	second := Coverage()
	if len(first) != len(second) {
		t.Fatalf("Coverage() returned different lengths across calls: %d vs %d", len(first), len(second))
	}
	for i := range first {
		if !reflect.DeepEqual(first[i], second[i]) {
			t.Fatalf("Coverage() is not deterministic at index %d: %+v vs %+v", i, first[i], second[i])
		}
		if i > 0 {
			prev, cur := first[i-1], first[i]
			if prev.Host > cur.Host || (prev.Host == cur.Host && prev.Concept > cur.Concept) {
				t.Fatalf("Coverage() is not sorted: %+v appears before %+v", prev, cur)
			}
		}
	}
}

// TestCoverage_MutatingReturnedSliceDoesNotAffectFutureCalls proves Coverage
// returns a fresh copy each call, not a shared slice/backing array a caller
// could accidentally corrupt for every subsequent caller.
func TestCoverage_MutatingReturnedSliceDoesNotAffectFutureCalls(t *testing.T) {
	first := Coverage()
	first[0].Host = "mutated"

	second := Coverage()
	if second[0].Host == "mutated" {
		t.Fatal("Coverage() returned a slice sharing backing storage with a previous call's result")
	}
}
