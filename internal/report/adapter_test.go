package report

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
	"github.com/wangzitian0/oh-my-code-agent/internal/effective"
	"github.com/wangzitian0/oh-my-code-agent/internal/knowledge"
)

// qualifiedStub is a stand-in knowledge.Resolution for every test in this
// file that is not itself testing the KNOWLEDGE_DRIFT path below: it is
// Qualified so knowledgeDriftSignals contributes nothing, keeping every
// pre-existing assertion in this file (exact signal counts from
// conflictSignals/ambiguousIdentitySignals alone) unaffected by adding a
// third signal source to BuildDriftSignals.
var qualifiedStub = knowledge.Resolution{Qualified: true}

func TestBuildDriftSignals_Conflict_SourceDrift(t *testing.T) {
	g := effective.Graphs{
		Host:        "codex",
		HostVersion: "0.144.5",
		Effective: effective.EffectiveGraph{
			Conflicts: []effective.Conflict{
				{
					Concept:   "mcp_server",
					LogicalID: "stdio|shared-tools",
					Candidates: []effective.Candidate{
						{Ref: "project/.codex/config.toml#mcp_servers.shared-tools"},
						{Ref: "home/.codex/config.toml#mcp_servers.shared-tools"},
					},
					EvidenceLevel: domain.EvidenceLevelDiscovered,
					Reason:        "no qualified precedence program",
				},
			},
		},
	}

	signals := BuildDriftSignals("infra2", g, qualifiedStub, knowledge.Repository{})
	if len(signals) != 1 {
		t.Fatalf("len(signals) = %d, want 1", len(signals))
	}
	sig := signals[0]
	if sig.Category != domain.DriftSourceDrift {
		t.Errorf("Category = %q, want SOURCE_DRIFT", sig.Category)
	}
	if sig.EntityID != "mcp_server/stdio|shared-tools" {
		t.Errorf("EntityID = %q, want mcp_server/stdio|shared-tools", sig.EntityID)
	}
	if sig.Project != "infra2" || sig.Host != "codex" || sig.HostVersion != "0.144.5" {
		t.Errorf("Project/Host/HostVersion = %q/%q/%q", sig.Project, sig.Host, sig.HostVersion)
	}
	if sig.RootCause != "no qualified precedence program" {
		t.Errorf("RootCause = %q", sig.RootCause)
	}
	if sig.EvidenceLevel != domain.EvidenceLevelDiscovered {
		t.Errorf("EvidenceLevel = %q", sig.EvidenceLevel)
	}
	observed, ok := sig.Observed.([]string)
	if !ok || len(observed) != 2 {
		t.Fatalf("Observed = %#v, want a 2-element []string", sig.Observed)
	}
}

func TestBuildDriftSignals_Conflict_DefaultRootCauseWhenReasonEmpty(t *testing.T) {
	g := effective.Graphs{
		Host: "codex",
		Effective: effective.EffectiveGraph{
			Conflicts: []effective.Conflict{
				{Concept: "skill", LogicalID: "review|project", Candidates: []effective.Candidate{{Ref: "a"}, {Ref: "b"}}},
			},
		},
	}
	signals := BuildDriftSignals("infra2", g, qualifiedStub, knowledge.Repository{})
	if len(signals) != 1 {
		t.Fatalf("len(signals) = %d, want 1", len(signals))
	}
	if signals[0].RootCause == "" {
		t.Error("RootCause is empty even though Conflict.Reason was empty -- adapter must fall back to a synthesized reason")
	}
}

func TestBuildDriftSignals_AmbiguousIdentity_Unknown(t *testing.T) {
	g := effective.Graphs{
		Host: "claude-code",
		Effective: effective.EffectiveGraph{
			AmbiguousIdentities: []effective.AmbiguousIdentity{
				{
					Concept: "skill",
					A:       effective.Candidate{LogicalID: "review|project", Ref: "project/.claude/skills/review/SKILL.md"},
					B:       effective.Candidate{LogicalID: "review-v2|project", Ref: "project/.claude/skills/review-v2/SKILL.md"},
					Reason:  "same content digest, different directory names",
				},
			},
		},
	}

	signals := BuildDriftSignals("infra2", g, qualifiedStub, knowledge.Repository{})
	if len(signals) != 1 {
		t.Fatalf("len(signals) = %d, want 1", len(signals))
	}
	sig := signals[0]
	if sig.Category != domain.DriftUnknown {
		t.Errorf("Category = %q, want UNKNOWN", sig.Category)
	}
	if sig.RootCause != "same content digest, different directory names" {
		t.Errorf("RootCause = %q", sig.RootCause)
	}
}

// TestBuildDriftSignals_NoDesiredCorrelation documents this adapter's own
// named scope gap (see adapter.go's "Known follow-up" doc comment): a
// resolved (non-conflicting) EffectiveEntry never produces a signal on its
// own, even when the Desired Graph disagrees, because ID correlation
// between the two graphs is not solved yet.
func TestBuildDriftSignals_NoDesiredCorrelation(t *testing.T) {
	g := effective.Graphs{
		Host: "codex",
		Effective: effective.EffectiveGraph{
			Entries: []effective.EffectiveEntry{
				{Concept: "skill", LogicalID: "review|project", Provenance: effective.Provenance{SelectedSource: "project/.codex/skills/review/SKILL.md"}},
			},
		},
	}
	signals := BuildDriftSignals("infra2", g, qualifiedStub, knowledge.Repository{})
	if len(signals) != 0 {
		t.Fatalf("len(signals) = %d, want 0 (a clean resolved entry produces no signal)", len(signals))
	}
}

func TestBuildDriftSignals_Empty(t *testing.T) {
	signals := BuildDriftSignals("infra2", effective.Graphs{Host: "codex"}, qualifiedStub, knowledge.Repository{})
	if len(signals) != 0 {
		t.Fatalf("len(signals) = %d, want 0", len(signals))
	}
}

// --- knowledgeDriftSignals / KNOWLEDGE_DRIFT ---------------------------

func TestBuildDriftSignals_UnqualifiedHost_KnowledgeDrift(t *testing.T) {
	repo := loadFixtureRepo(t, "singlehost")
	g := effective.Graphs{Host: "codex", HostVersion: "9.9.9"}
	resolution := repo.Resolve("codex", "cli", "9.9.9")
	if resolution.Qualified {
		t.Fatalf("test fixture setup: resolution unexpectedly Qualified")
	}

	signals := BuildDriftSignals("infra2", g, resolution, repo)
	if len(signals) != 1 {
		t.Fatalf("len(signals) = %d, want 1", len(signals))
	}
	sig := signals[0]
	if sig.Category != domain.DriftKnowledgeDrift {
		t.Errorf("Category = %q, want KNOWLEDGE_DRIFT", sig.Category)
	}
	if sig.EntityID != "host/codex" {
		t.Errorf("EntityID = %q, want host/codex", sig.EntityID)
	}
	if sig.Host != "codex" || sig.HostVersion != "9.9.9" || sig.Project != "infra2" {
		t.Errorf("Host/HostVersion/Project = %q/%q/%q", sig.Host, sig.HostVersion, sig.Project)
	}
	observed, ok := sig.Observed.(string)
	if !ok || observed == "" {
		t.Fatalf("Observed = %#v, want a non-empty string naming the published ranges", sig.Observed)
	}
	if !strings.Contains(observed, ">=1.0.0 <2.0.0") {
		t.Errorf("Observed = %q, want it to name the one published Pack range that does not cover 9.9.9", observed)
	}
	if sig.RootCause == "" {
		t.Error("RootCause is empty, want resolution.Reason to flow through")
	}
	if sig.Guarantee != domain.GuaranteeObserved {
		t.Errorf("Guarantee = %q, want OBSERVED", sig.Guarantee)
	}
}

func TestBuildDriftSignals_UnqualifiedHost_NoPacksPublishedAtAll(t *testing.T) {
	g := effective.Graphs{Host: "codex", HostVersion: "1.2.3"}
	resolution := knowledge.Resolution{Host: "codex", Surface: "cli", Version: "1.2.3", Qualified: false, Reason: "no qualified pack: no Knowledge Pack's versionRange matches codex/cli version 1.2.3"}

	signals := BuildDriftSignals("infra2", g, resolution, knowledge.Repository{})
	if len(signals) != 1 {
		t.Fatalf("len(signals) = %d, want 1", len(signals))
	}
	if !strings.Contains(signals[0].Observed.(string), "no Knowledge Pack is published") {
		t.Errorf("Observed = %q, want it to say no Pack is published at all for this host", signals[0].Observed)
	}
}

func TestBuildDriftSignals_QualifiedHost_NoKnowledgeDriftSignal(t *testing.T) {
	g := effective.Graphs{Host: "codex", HostVersion: "0.144.5"}
	signals := BuildDriftSignals("infra2", g, knowledge.Resolution{Qualified: true}, knowledge.Repository{})
	if len(signals) != 0 {
		t.Fatalf("len(signals) = %d, want 0 (a qualified resolution produces no KNOWLEDGE_DRIFT signal)", len(signals))
	}
}

// singleHostPackJSON is a minimal, test-local Knowledge Pack for host
// "codex" covering only >=1.0.0 <2.0.0 -- deliberately not the real
// committed knowledge/hosts/codex Pack (whose exact version range could
// shift independently of this test's intent), mirroring
// internal/knowledge/repository_test.go's own inline-JSON-fixture
// convention rather than a testdata directory.
const singleHostPackJSON = `{
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
  "capabilities": { "skill": { "discover": "PARTIAL", "resolve": "UNKNOWN" } }
}`

// loadFixtureRepo loads a small, single-Pack, test-local Knowledge
// repository for host "codex" (versionRange >=1.0.0 <2.0.0), so a test can
// resolve a version well outside it (e.g. "9.9.9") and get a real,
// non-fabricated unqualified knowledge.Resolution from knowledge.Resolve
// itself, not a hand-built Resolution value.
func loadFixtureRepo(t *testing.T, name string) knowledge.Repository {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "codex", "cli", "1.0")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, knowledge.PackFileName), []byte(singleHostPackJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	repo, err := knowledge.LoadRepository(root)
	if err != nil {
		t.Fatalf("loadFixtureRepo(%s): %v", name, err)
	}
	return repo
}
