package mcp

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
	"github.com/wangzitian0/oh-my-code-agent/internal/knowledge"
)

// This file proves, end-to-end and with a real (fixture-backed)
// knowledge.Repository -- not a hand-stubbed CapabilityFunc like
// staticCapability -- that issue #32's second acceptance criterion actually
// holds: "An installed unqualified host version is marked as Knowledge
// Drift and blocks write expansion (test)."
//
// cmd/omca/mcp.go's capabilityFuncForMCP is the real production wiring:
//
//	resolution := repo.Resolve(host, hd.Surface, hd.Version)
//	if !resolution.Qualified {
//		return domain.CapabilityOps{}, false
//	}
//	return resolution.CapabilityFor(concept), true
//
// That function itself is not unit-testable in isolation here: it also
// shells out to real host detection (hostcontext.DetectHost) and the real
// on-disk Knowledge repository (knowledge.Default()), neither of which this
// package can control deterministically. What CAN be, and previously
// was NOT, proven end-to-end is the chain this closure sits in the middle
// of: a real knowledge.Repository.Resolve outcome, unqualified because no
// committed Pack's versionRange covers the installed version, flowing
// through this exact two-line gate into internal/mcp's real capability gate
// (capabilityAndPolicyGates, called by both ComputePropose and
// ComputeStage) and actually rejecting the proposal -- not merely a claim
// that Resolution.Qualified exists.
//
// realCapabilityFuncFromResolve builds a CapabilityFunc with the identical
// logic capabilityFuncForMCP uses, closed over a real knowledge.Repository
// and a fixed (surface, version) pair instead of a live host detection --
// the one substitution needed to make the real Resolve/CapabilityFor chain
// deterministic in a test.
func realCapabilityFuncFromResolve(repo knowledge.Repository, surface, version string) CapabilityFunc {
	return func(host, concept string) (domain.CapabilityOps, bool) {
		resolution := repo.Resolve(host, surface, version)
		if !resolution.Qualified {
			return domain.CapabilityOps{}, false
		}
		return resolution.CapabilityFor(concept), true
	}
}

// knowledgeGateFixturePackJSON publishes exactly one Pack for host "codex",
// surface "cli", versionRange ">=1.0.0 <2.0.0" -- a real Pack a real
// installed version can either fall inside or, as this file's rejection
// test proves, fall entirely outside of.
const knowledgeGateFixturePackJSON = `{
  "apiVersion": "omca.dev/v1alpha1",
  "kind": "HostKnowledge",
  "metadata": {
    "id": "codex:cli:1.0",
    "host": "codex",
    "surface": "cli",
    "versionRange": ">=1.0.0 <2.0.0",
    "status": "FRESH"
  },
  "evidence": [ { "id": "codex-doc", "kind": "official-doc" } ],
  "capabilities": { "skill": { "discover": "EXACT", "resolve": "EXACT", "compile": "EXACT" } }
}`

func loadKnowledgeGateFixtureRepo(t *testing.T) knowledge.Repository {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "codex", "cli", "1.0")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, knowledge.PackFileName), []byte(knowledgeGateFixturePackJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	repo, err := knowledge.LoadRepository(root)
	if err != nil {
		t.Fatalf("loadKnowledgeGateFixtureRepo: %v", err)
	}
	return repo
}

// TestComputePropose_RealKnowledgeRepository_UnqualifiedHostVersion_RejectsWriteExpansion
// is this issue's own explicit "(test)" requirement: an installed host
// version ("9.9.9") that matches NO committed Knowledge Pack's versionRange
// must be rejected at the capability gate before any write-expanding
// Activation change (enabling a skill) can be staged.
func TestComputePropose_RealKnowledgeRepository_UnqualifiedHostVersion_RejectsWriteExpansion(t *testing.T) {
	repo := loadKnowledgeGateFixtureRepo(t)

	// Sanity-check the premise with the real Resolve call this test relies
	// on, so a future change to the fixture or to Resolve's own matching
	// rules fails loudly here rather than silently making this test vacuous.
	resolution := repo.Resolve("codex", "cli", "9.9.9")
	if resolution.Qualified {
		t.Fatalf("test premise broken: repo.Resolve(codex, cli, 9.9.9) unexpectedly Qualified against versionRange %q", "codex:cli:1.0")
	}

	capFn := realCapabilityFuncFromResolve(repo, "cli", "9.9.9")
	pc := ProposeContext{Artifact: proposeFixtureArtifact(), CapabilityFor: capFn}

	_, err := ComputePropose(pc, validProposal(t))
	if err == nil {
		t.Fatal("ComputePropose: want an error for an unqualified installed host version, got nil -- write expansion was NOT blocked")
	}
	if gate := rejectedGate(t, err); gate != "capability" {
		t.Errorf("Gate = %q, want %q", gate, "capability")
	}

	// Same proof one layer up: ComputeStage must never reach CompileFunc
	// (the sole channel that can write a pending generation to disk) for a
	// proposal the capability gate rejects.
	if _, err := ComputeStage(pc, panicCompile(t), validProposal(t)); err == nil {
		t.Fatal("ComputeStage: want an error for an unqualified installed host version, got nil")
	} else if gate := rejectedGate(t, err); gate != "capability" {
		t.Errorf("ComputeStage Gate = %q, want %q", gate, "capability")
	}
}

// TestComputePropose_RealKnowledgeRepository_QualifiedHostVersion_AllowsWriteExpansion
// is the positive control: the exact same real Repository, gate wiring, and
// proposal succeed once the installed version genuinely falls inside the
// published Pack's versionRange -- proving the previous test's rejection is
// really caused by Resolve's Qualified outcome, not by some unrelated gate
// or a CapabilityFunc that always fails.
func TestComputePropose_RealKnowledgeRepository_QualifiedHostVersion_AllowsWriteExpansion(t *testing.T) {
	repo := loadKnowledgeGateFixtureRepo(t)

	resolution := repo.Resolve("codex", "cli", "1.5.0")
	if !resolution.Qualified {
		t.Fatalf("test premise broken: repo.Resolve(codex, cli, 1.5.0) unexpectedly NOT Qualified: %s", resolution.Reason)
	}

	capFn := realCapabilityFuncFromResolve(repo, "cli", "1.5.0")
	pc := ProposeContext{Artifact: proposeFixtureArtifact(), CapabilityFor: capFn}

	if _, err := ComputePropose(pc, validProposal(t)); err != nil {
		t.Fatalf("ComputePropose: want no error for a qualified installed host version, got %v", err)
	}

	rec := &recordingCompile{
		pending: stagePendingGeneration("codex", []domain.GenerationSourceEntry{newlyIncludedSkillSource("codex", "code-review")}),
		current: map[string]domain.Generation{"codex": {Metadata: domain.GenerationMetadata{ID: "generation:sha256:current-fixture"}}},
	}
	if _, err := ComputeStage(pc, rec.fn(), validProposal(t)); err != nil {
		t.Fatalf("ComputeStage: want no error for a qualified installed host version, got %v", err)
	}
	if rec.calls != 1 {
		t.Errorf("CompileFunc calls = %d, want 1 -- a qualified host's write expansion must actually reach compile", rec.calls)
	}
}
