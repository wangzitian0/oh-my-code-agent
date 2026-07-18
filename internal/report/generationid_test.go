package report

import (
	"path/filepath"
	"testing"
	"time"

	hostcontext "github.com/wangzitian0/oh-my-code-agent/internal/context"
	"github.com/wangzitian0/oh-my-code-agent/internal/knowledge"
	"github.com/wangzitian0/oh-my-code-agent/internal/runtime"
)

// TestBuild_HostDebug_CarriesCurrentGenerationID proves issue #24's small
// HostDebug addition (CurrentGenerationID/PendingGenerationID): Build
// already reads each generation's full manifest.json inside
// generationSources to compute CurrentSources/PendingSources and the
// context-cost estimate, and this just keeps the ID it already read in hand
// rather than discarding it. A real CURRENT generation is compiled and
// activated (the same runtime.Bootstrap/SetCurrentGeneration sequence
// planes_fixture_test.go's TestComparePlanes_MCPServerFragmentCorrelation_
// RealFixture uses), and Build's resulting HostDebug.CurrentGenerationID
// must equal that exact generation's own Metadata.ID -- the identical value
// `omca status`/omca_status already reports via runtime.
// ReadGenerationManifest(dir).Metadata.ID for the same on-disk generation.
// PendingGenerationID stays empty, honestly reflecting that no pending
// generation was ever compiled in this test's worktree state directory.
func TestBuild_HostDebug_CarriesCurrentGenerationID(t *testing.T) {
	root := repoRootForTest()
	c, sb := loadCaseSandbox(t, root, filepath.Join("fixtures", "codex", "0.144.5", "mcp-merge"))
	hostInput := buildCodexHostInput(t, sb, c)

	repo, err := knowledge.LoadRepository(filepath.Join(root, "knowledge", "hosts"))
	if err != nil {
		t.Fatalf("LoadRepository: %v", err)
	}

	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	worktree := hostcontext.Worktree{ID: "worktree:sha256:generationid-test", Root: sb.Project}

	bootstrapReq := runtime.BootstrapRequest{
		Detection:    hostInput.Detection,
		Worktree:     worktree,
		Observations: hostInput.Observations,
		Now:          now,
	}
	generationDir := filepath.Join(t.TempDir(), "generation")
	gen, err := runtime.Bootstrap(bootstrapReq, generationDir)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	restoreGenerationDirWritable(t, generationDir)
	worktreeStateDir := t.TempDir()
	if err := runtime.SetCurrentGeneration(worktreeStateDir, hostInput.Detection.Host, generationDir, gen, hostInput.Detection, now); err != nil {
		t.Fatalf("SetCurrentGeneration: %v", err)
	}
	if gen.Metadata.ID == "" {
		t.Fatal("sanity check failed: Bootstrap produced an empty Metadata.ID")
	}

	artifact, err := Build(BuildRequest{
		Worktree:         worktree,
		WorktreeStateDir: worktreeStateDir,
		Hosts:            []HostInput{hostInput},
		Repository:       repo,
		Now:              now,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	hd, ok := artifact.Debug["codex"]
	if !ok {
		t.Fatal("artifact.Debug has no codex entry")
	}
	if hd.CurrentGenerationID != gen.Metadata.ID {
		t.Errorf("CurrentGenerationID = %q, want %q (this exact generation's own Metadata.ID)", hd.CurrentGenerationID, gen.Metadata.ID)
	}
	if hd.PendingGenerationID != "" {
		t.Errorf("PendingGenerationID = %q, want empty (no pending generation was compiled in this worktree)", hd.PendingGenerationID)
	}
}

// TestBuild_HostDebug_NoGeneration_EmptyGenerationIDs is the negative
// control: a worktree that has never run `omca env` reports both
// CurrentGenerationID and PendingGenerationID as empty, honestly (never a
// synthesized placeholder ID for a generation that does not exist),
// matching generationSources' own "unknown reported as unknown" doc
// comment.
func TestBuild_HostDebug_NoGeneration_EmptyGenerationIDs(t *testing.T) {
	root := repoRootForTest()
	c, sb := loadCaseSandbox(t, root, filepath.Join("fixtures", "codex", "0.144.5", "skill-collision"))
	hostInput := buildCodexHostInput(t, sb, c)

	repo, err := knowledge.LoadRepository(filepath.Join(root, "knowledge", "hosts"))
	if err != nil {
		t.Fatalf("LoadRepository: %v", err)
	}

	artifact, err := Build(BuildRequest{
		Worktree:         hostcontext.Worktree{ID: "worktree:sha256:no-generation-test", Root: sb.Project},
		WorktreeStateDir: t.TempDir(),
		Hosts:            []HostInput{hostInput},
		Repository:       repo,
		Now:              time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	hd := artifact.Debug["codex"]
	if hd.CurrentGenerationID != "" || hd.PendingGenerationID != "" {
		t.Errorf("generation IDs = (%q, %q), want (\"\", \"\") for a worktree with no generation history", hd.CurrentGenerationID, hd.PendingGenerationID)
	}
}
