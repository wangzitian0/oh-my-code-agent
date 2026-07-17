package report

import (
	"encoding/json"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	hostcontext "github.com/wangzitian0/oh-my-code-agent/internal/context"
	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
	"github.com/wangzitian0/oh-my-code-agent/internal/knowledge"
	"github.com/wangzitian0/oh-my-code-agent/internal/observe"
	"github.com/wangzitian0/oh-my-code-agent/internal/qualify"
)

// repoRootForTest locates this repository's root relative to this source
// file's own location, the same runtime.Caller trick internal/effective's
// own fixture_test.go, internal/ontology, and internal/qualify use.
func repoRootForTest() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..")
}

// buildCodexHostInput drives the real observe.Observe pipeline over one
// committed fixture case's input tree for codex, mirroring internal/
// effective/fixture_test.go's buildObserveRequest — this package's own copy
// since that helper is unexported across the package boundary.
func buildCodexHostInput(t *testing.T, sb *qualify.Sandbox, c *qualify.Case) HostInput {
	t.Helper()
	detection := hostcontext.HostDetection{
		Host:      c.Host,
		Surface:   "cli",
		Installed: true,
		Version:   c.Version,
		NativeHomes: []hostcontext.NativeHome{
			{Name: "CODEX_HOME", Path: sb.CodexHome},
			{Name: "HOME/.agents/skills", Path: filepath.Join(sb.Home, ".agents", "skills")},
		},
	}
	obs, err := observe.Observe(observe.Request{Detection: detection, WorktreeRoot: sb.Project})
	if err != nil {
		t.Fatalf("observe.Observe: %v", err)
	}
	return HostInput{Detection: detection, Observations: obs}
}

func loadCaseSandbox(t *testing.T, root, rel string) (*qualify.Case, *qualify.Sandbox) {
	t.Helper()
	dir := filepath.Join(root, rel)
	c, err := qualify.LoadCase(dir)
	if err != nil {
		t.Fatalf("LoadCase(%s): %v", dir, err)
	}
	sb, err := qualify.NewSandbox(t.TempDir(), c.Host)
	if err != nil {
		t.Fatalf("NewSandbox: %v", err)
	}
	if err := sb.PopulateFromInput(c.InputDir()); err != nil {
		t.Fatalf("PopulateFromInput: %v", err)
	}
	return c, sb
}

// TestBuild_EndToEnd_RealFixture_ProducesSourceDrift proves the
// PR-18-anticipated adapter actually runs end-to-end on PR-17's real
// resolver output, not only hand-built fixtures: the committed mcp-merge
// fixture for codex has a genuine multi-source collision for
// "shared-tools" (both real Knowledge Packs declare resolve: UNKNOWN for
// mcp_server, per internal/effective/fixture_test.go's own
// TestFixtureCorpus_KnowledgePacksHaveConceptCoverage), so ComputeEffective
// Graph must leave it as a Conflict, and this adapter must turn that
// Conflict into a SOURCE_DRIFT signal that survives classification and
// grouping into an ActionCard.
func TestBuild_EndToEnd_RealFixture_ProducesSourceDrift(t *testing.T) {
	root := repoRootForTest()
	c, sb := loadCaseSandbox(t, root, filepath.Join("fixtures", "codex", "0.144.5", "mcp-merge"))
	hostInput := buildCodexHostInput(t, sb, c)

	repo, err := knowledge.LoadRepository(filepath.Join(root, "knowledge", "hosts"))
	if err != nil {
		t.Fatalf("LoadRepository: %v", err)
	}

	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	req := BuildRequest{
		Worktree:   hostcontext.Worktree{ID: "worktree:sha256:test", Root: sb.Project},
		Hosts:      []HostInput{hostInput},
		Repository: repo,
		Now:        now,
	}

	artifact, err := Build(req)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	if err := domain.ValidateReport(artifact.Report); err != nil {
		t.Errorf("Build produced an invalid domain.Report: %v", err)
	}

	var sourceDriftCard *DriftCard
	for i := range artifact.ActionCards {
		if artifact.ActionCards[i].Category == domain.DriftSourceDrift {
			sourceDriftCard = &artifact.ActionCards[i]
		}
	}
	if sourceDriftCard == nil {
		t.Fatalf("expected at least one SOURCE_DRIFT action card from mcp-merge's real collision, got categories: %v", cardCategories(artifact.ActionCards))
	}
	if sourceDriftCard.Impact.Artifacts == 0 {
		t.Errorf("SOURCE_DRIFT card has zero-artifact impact: %+v", sourceDriftCard.Impact)
	}
	if len(sourceDriftCard.Matrix) == 0 {
		t.Error("SOURCE_DRIFT card's Matrix is empty")
	}

	// Reproducibility: two Build calls over identical logical input (same
	// Now) must produce the same content fingerprint (docs/architecture/
	// reporting.md §1's "reproducible" trust property).
	artifact2, err := Build(req)
	if err != nil {
		t.Fatalf("Build (second call): %v", err)
	}
	if artifact.Report.Spec.Fingerprint != artifact2.Report.Spec.Fingerprint {
		t.Errorf("fingerprint not stable across identical Build calls: %q vs %q", artifact.Report.Spec.Fingerprint, artifact2.Report.Spec.Fingerprint)
	}
	if len(artifact.ActionCards) != len(artifact2.ActionCards) || (len(artifact.ActionCards) > 0 && artifact.ActionCards[0].ID != artifact2.ActionCards[0].ID) {
		t.Errorf("ActionCards not stable across identical Build calls")
	}

	// Stable JSON contract: marshaling the same artifact twice produces
	// byte-identical output.
	b1, err := json.Marshal(artifact)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	b2, err := json.Marshal(artifact)
	if err != nil {
		t.Fatalf("json.Marshal (again): %v", err)
	}
	if string(b1) != string(b2) {
		t.Error("JSON marshaling the same Artifact value twice produced different output")
	}
}

// TestBuild_EndToEnd_RealFixture_KnowledgeStatusAndContextCost proves the
// round-2 audit's per-host Knowledge status requirement against the same
// real fixture/Pack pair, and that ContextCost is honestly nil (not a fake
// zero) when no current generation exists for this ad hoc worktree.
func TestBuild_EndToEnd_RealFixture_KnowledgeStatusAndContextCost(t *testing.T) {
	root := repoRootForTest()
	c, sb := loadCaseSandbox(t, root, filepath.Join("fixtures", "codex", "0.144.5", "skill-collision"))
	hostInput := buildCodexHostInput(t, sb, c)

	repo, err := knowledge.LoadRepository(filepath.Join(root, "knowledge", "hosts"))
	if err != nil {
		t.Fatalf("LoadRepository: %v", err)
	}

	artifact, err := Build(BuildRequest{
		Worktree:   hostcontext.Worktree{ID: "worktree:sha256:test2", Root: sb.Project},
		Hosts:      []HostInput{hostInput},
		Repository: repo,
		Now:        time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	if len(artifact.Hosts) != 1 {
		t.Fatalf("len(Hosts) = %d, want 1", len(artifact.Hosts))
	}
	host := artifact.Hosts[0]
	if !host.Knowledge.Qualified {
		t.Errorf("expected codex 0.144.5 to qualify against the real committed Knowledge Pack, got Reason=%q", host.Knowledge.Reason)
	}
	if host.Knowledge.Status == "" {
		t.Error("Knowledge.Status is empty for a qualified host")
	}
	if status, ok := artifact.Report.Spec.KnowledgeStatus["codex"]; !ok || status != host.Knowledge.Status {
		t.Errorf("Spec.KnowledgeStatus[codex] = %q, ok=%v, want %q", status, ok, host.Knowledge.Status)
	}
	if host.ContextCost != nil {
		t.Errorf("ContextCost = %+v, want nil (no current generation exists for this ad hoc worktree)", host.ContextCost)
	}
}

func cardCategories(cards []DriftCard) []domain.DriftCategory {
	out := make([]domain.DriftCategory, len(cards))
	for i, c := range cards {
		out[i] = c.Category
	}
	return out
}

// TestSourcesForHost_FiltersOutOtherHosts proves the Copilot-review fix:
// domain.GenerationSourceEntry's own doc comment establishes that a
// Generation can share multiple hosts' artifact trees, so
// Generation.Spec.Sources is one flat list across all of them, each entry
// stamped with its own Host. Before the fix, generationSources returned
// this list unfiltered, so a multi-host generation's CURRENT/PENDING plane
// counts and compare/diff output for one host would incorrectly include
// another host's sources too.
func TestSourcesForHost_FiltersOutOtherHosts(t *testing.T) {
	sources := []domain.GenerationSourceEntry{
		{Concept: "instruction", Source: "codex/AGENTS.md", Host: "codex", Included: true},
		{Concept: "instruction", Source: "claude/CLAUDE.md", Host: "claude-code", Included: true},
		{Concept: "mcpServer", Source: "codex/config.toml", Host: "codex", Included: true},
	}
	got := sourcesForHost(sources, "codex")
	if len(got) != 2 {
		t.Fatalf("sourcesForHost(codex) = %d entries, want 2 (codex-only): %+v", len(got), got)
	}
	for _, s := range got {
		if s.Host != "codex" {
			t.Errorf("sourcesForHost(codex) leaked a %q-host entry: %+v", s.Host, s)
		}
	}
	if got := sourcesForHost(sources, "claude-code"); len(got) != 1 || got[0].Host != "claude-code" {
		t.Errorf("sourcesForHost(claude-code) = %+v, want exactly the one claude-code entry", got)
	}
}
