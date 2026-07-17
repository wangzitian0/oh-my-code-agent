package runtime

import (
	"path/filepath"
	"testing"
	"time"

	hostcontext "github.com/wangzitian0/oh-my-code-agent/internal/context"
	"github.com/wangzitian0/oh-my-code-agent/internal/observe"
)

// TestBootstrap_RebuildingIntoFreshOutputDir_YieldsIdenticalID is the
// end-to-end (not just GenerationID-in-isolation) version of issue #13 AC
// #4: compiling the identical BootstrapRequest twice, into two different
// fresh output directories (the realistic "rebuild" scenario -- pending vs.
// a later recompute), yields the identical Generation.metadata.id and an
// identical artifact digest set.
func TestBootstrap_RebuildingIntoFreshOutputDir_YieldsIdenticalID(t *testing.T) {
	tr := newCodexFixtureTree(t)
	mustWriteFile(t, filepath.Join(tr.WorktreeRoot, "AGENTS.md"), "# instructions\n")
	mustWriteFile(t, filepath.Join(tr.CodexHome, "config.toml"), "[mcp_servers.demo]\ncommand = \"npx\"\n")
	obs, err := observe.Observe(tr.request("0.144.5"))
	if err != nil {
		t.Fatalf("observe.Observe: %v", err)
	}
	req := BootstrapRequest{
		Detection:    tr.detection("0.144.5"),
		Worktree:     tr.worktree(t),
		Observations: obs,
		Now:          time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC),
	}

	dir1 := filepath.Join(t.TempDir(), "generation")
	gen1, err := Bootstrap(req, dir1)
	if err != nil {
		t.Fatalf("Bootstrap (1st): %v", err)
	}
	restoreWritable(t, dir1)

	dir2 := filepath.Join(t.TempDir(), "generation")
	gen2, err := Bootstrap(req, dir2)
	if err != nil {
		t.Fatalf("Bootstrap (2nd): %v", err)
	}
	restoreWritable(t, dir2)

	if gen1.Metadata.ID != gen2.Metadata.ID {
		t.Fatalf("rebuilding from identical inputs produced different generation IDs: %q != %q", gen1.Metadata.ID, gen2.Metadata.ID)
	}
	if gen1.Spec.DesiredGraphDigest != gen2.Spec.DesiredGraphDigest {
		t.Errorf("desiredGraphDigest differs across identical rebuilds: %q != %q", gen1.Spec.DesiredGraphDigest, gen2.Spec.DesiredGraphDigest)
	}

	artifacts1 := gen1.Spec.Hosts["codex"].Artifacts
	artifacts2 := gen2.Spec.Hosts["codex"].Artifacts
	if len(artifacts1) != len(artifacts2) {
		t.Fatalf("artifact count differs across identical rebuilds: %d != %d", len(artifacts1), len(artifacts2))
	}
	byPath1 := make(map[string]string, len(artifacts1))
	for _, a := range artifacts1 {
		byPath1[a.Path] = a.Digest
	}
	for _, a := range artifacts2 {
		if byPath1[a.Path] != a.Digest {
			t.Errorf("artifact %s digest differs across identical rebuilds: %q != %q", a.Path, byPath1[a.Path], a.Digest)
		}
	}

	// The generated file trees on disk must also be byte-identical.
	tree1 := walkGeneratedTree(t, dir1)
	tree2 := walkGeneratedTree(t, dir2)
	if len(tree1) != len(tree2) {
		t.Fatalf("generated file count differs across identical rebuilds: %d != %d", len(tree1), len(tree2))
	}
	for path, content1 := range tree1 {
		if path == "manifest.json" {
			continue // manifest.json legitimately differs (createdAt is whatever req.Now was; identical here, but not a property this test needs to pin down further than gen1/gen2 equality above)
		}
		content2, ok := tree2[path]
		if !ok {
			t.Errorf("file %s present in first rebuild, missing in second", path)
			continue
		}
		if string(content1) != string(content2) {
			t.Errorf("file %s content differs across identical rebuilds", path)
		}
	}
}

// TestBootstrap_RejectsRelativeOutputDir proves Bootstrap fails closed on a
// relative outputDir rather than silently resolving it against the
// process's current working directory -- the same "never derive from
// ambient state" posture internal/observe.Observe takes for a relative
// native-home or worktree-root path.
func TestBootstrap_RejectsRelativeOutputDir(t *testing.T) {
	tr := newCodexFixtureTree(t)
	mustWriteFile(t, filepath.Join(tr.WorktreeRoot, "AGENTS.md"), "# instructions\n")
	obs, err := observe.Observe(tr.request("0.144.5"))
	if err != nil {
		t.Fatalf("observe.Observe: %v", err)
	}
	req := BootstrapRequest{
		Detection:    tr.detection("0.144.5"),
		Worktree:     tr.worktree(t),
		Observations: obs,
		Now:          time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC),
	}
	if _, err := Bootstrap(req, "relative/output/dir"); err == nil {
		t.Fatal("Bootstrap with a relative outputDir: want error, got nil")
	}
}

// TestBootstrap_RejectsMismatchedObservationHost proves req.validate()
// (exercised through the public Bootstrap/GenerationID entry points) really
// runs: an Observation belonging to a different host than Detection.Host is
// a caller composition bug this package must not silently accept.
func TestBootstrap_RejectsMismatchedObservationHost(t *testing.T) {
	tr := newClaudeFixtureTree(t)
	mustWriteFile(t, filepath.Join(tr.WorktreeRoot, "CLAUDE.md"), "# instructions\n")
	obs, err := observe.Observe(tr.request("2.1.211"))
	if err != nil {
		t.Fatalf("observe.Observe: %v", err)
	}

	req := BootstrapRequest{
		// Detection.Host is codex, but obs was computed for claude-code.
		Detection:    hostcontext.HostDetection{Host: "codex", Version: "0.144.5"},
		Worktree:     tr.worktree(t),
		Observations: obs,
		Now:          time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC),
	}
	if _, err := GenerationID(req); err == nil {
		t.Fatal("GenerationID with mismatched observation host: want error, got nil")
	}
	if _, err := Bootstrap(req, filepath.Join(t.TempDir(), "generation")); err == nil {
		t.Fatal("Bootstrap with mismatched observation host: want error, got nil")
	}
}

// TestBootstrap_RejectsMissingNow proves Bootstrap never silently falls
// back to time.Now(): an unset req.Now is a caller error.
func TestBootstrap_RejectsMissingNow(t *testing.T) {
	tr := newCodexFixtureTree(t)
	mustWriteFile(t, filepath.Join(tr.WorktreeRoot, "AGENTS.md"), "# instructions\n")
	obs, err := observe.Observe(tr.request("0.144.5"))
	if err != nil {
		t.Fatalf("observe.Observe: %v", err)
	}
	req := BootstrapRequest{
		Detection:    tr.detection("0.144.5"),
		Worktree:     tr.worktree(t),
		Observations: obs,
		// Now intentionally left zero.
	}
	if _, err := Bootstrap(req, filepath.Join(t.TempDir(), "generation")); err == nil {
		t.Fatal("Bootstrap with a zero Now: want error, got nil")
	}
}

// TestBootstrap_NativeHomeDirectoryNaming proves each host's generated
// native-home directory is named after the real environment variable a
// launch shim would relocate (docs/architecture/runtime.md §7.1/§7.2):
// "codex-home" under hosts/codex/<surface>/, "claude-config" under
// hosts/claude-code/<surface>/.
func TestBootstrap_NativeHomeDirectoryNaming(t *testing.T) {
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)

	trCodex := newCodexFixtureTree(t)
	mustWriteFile(t, filepath.Join(trCodex.WorktreeRoot, "AGENTS.md"), "# instructions\n")
	obsCodex, err := observe.Observe(trCodex.request("0.144.5"))
	if err != nil {
		t.Fatalf("observe.Observe (codex): %v", err)
	}
	reqCodex := BootstrapRequest{Detection: trCodex.detection("0.144.5"), Worktree: trCodex.worktree(t), Observations: obsCodex, Now: now}
	dirCodex := filepath.Join(t.TempDir(), "generation")
	if _, err := Bootstrap(reqCodex, dirCodex); err != nil {
		t.Fatalf("Bootstrap (codex): %v", err)
	}
	restoreWritable(t, dirCodex)
	treeCodex := walkGeneratedTree(t, dirCodex)
	if _, ok := treeCodex[filepath.Join("hosts", "codex", "cli", "codex-home", "config.toml")]; !ok {
		t.Errorf("expected hosts/codex/cli/codex-home/config.toml, got %v", keysOf(treeCodex))
	}

	trClaude := newClaudeFixtureTree(t)
	mustWriteFile(t, filepath.Join(trClaude.WorktreeRoot, "CLAUDE.md"), "# instructions\n")
	obsClaude, err := observe.Observe(trClaude.request("2.1.211"))
	if err != nil {
		t.Fatalf("observe.Observe (claude): %v", err)
	}
	reqClaude := BootstrapRequest{Detection: trClaude.detection("2.1.211"), Worktree: trClaude.worktree(t), Observations: obsClaude, Now: now}
	dirClaude := filepath.Join(t.TempDir(), "generation")
	if _, err := Bootstrap(reqClaude, dirClaude); err != nil {
		t.Fatalf("Bootstrap (claude): %v", err)
	}
	restoreWritable(t, dirClaude)
	treeClaude := walkGeneratedTree(t, dirClaude)
	if _, ok := treeClaude[filepath.Join("hosts", "claude-code", "cli", "claude-config", "settings.json")]; !ok {
		t.Errorf("expected hosts/claude-code/cli/claude-config/settings.json, got %v", keysOf(treeClaude))
	}
}

func keysOf(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
