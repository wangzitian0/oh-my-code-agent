package runtime

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
	"github.com/wangzitian0/oh-my-code-agent/internal/observe"
)

// TestBootstrap_Manifest_EverySourceHasAReason is issue #13 AC #3, "The
// manifest lists every included and excluded source with a reason": for a
// mixed fixture (native Instructions/MCP/Skill all present at user scope,
// repository Instructions/MCP/Skill all present at workspace scope), every
// one of the resulting sources entries -- included or excluded -- carries a
// non-empty Reason, domain.ValidateGeneration accepts the whole document,
// and manifest.json on disk round-trips to the same content.
func TestBootstrap_Manifest_EverySourceHasAReason(t *testing.T) {
	tr := newCodexFixtureTree(t)
	mustWriteFile(t, filepath.Join(tr.CodexHome, "AGENTS.md"), "# native user instructions\n")
	mustWriteFile(t, filepath.Join(tr.CodexHome, "config.toml"), "[mcp_servers.native]\ncommand = \"npx\"\n")
	mustWriteFile(t, filepath.Join(tr.CodexHome, "skills", "native-skill", "SKILL.md"), "---\nname: native-skill\n---\nbody\n")
	mustWriteFile(t, filepath.Join(tr.WorktreeRoot, "AGENTS.md"), "# repo instructions\n")
	mustWriteFile(t, filepath.Join(tr.WorktreeRoot, ".codex", "config.toml"), "[mcp_servers.proj]\ncommand = \"./run.sh\"\n")
	mustWriteFile(t, filepath.Join(tr.WorktreeRoot, ".agents", "skills", "proj-skill", "SKILL.md"), "---\nname: proj-skill\n---\nbody\n")

	obs, err := observe.Observe(tr.request("0.144.5"))
	if err != nil {
		t.Fatalf("observe.Observe: %v", err)
	}
	if len(obs) != 6 {
		t.Fatalf("sanity check: got %d observations, want 6", len(obs))
	}

	req := BootstrapRequest{
		Detection:    tr.detection("0.144.5"),
		Worktree:     tr.worktree(t),
		Observations: obs,
		Now:          time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC),
	}
	outputDir := filepath.Join(t.TempDir(), "generation")
	gen, err := Bootstrap(req, outputDir)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	restoreWritable(t, outputDir)

	if err := domain.ValidateGeneration(gen); err != nil {
		t.Fatalf("ValidateGeneration: %v", err)
	}
	if len(gen.Spec.Sources) != len(obs) {
		t.Fatalf("sources = %d entries, want one per observation (%d)", len(gen.Spec.Sources), len(obs))
	}

	var includedCount, excludedCount int
	for _, s := range gen.Spec.Sources {
		if s.Reason == "" {
			t.Errorf("source %+v has an empty reason", s)
		}
		if s.Concept == "" {
			t.Errorf("source %+v has an empty concept", s)
		}
		if s.Included {
			includedCount++
		} else {
			excludedCount++
		}
	}
	// Exactly the repository AGENTS.md should be included; the other five
	// (3 native, repo MCP, repo Skill) are excluded per the M1 policy.
	if includedCount != 1 {
		t.Errorf("includedCount = %d, want 1 (only the repository Instructions chain)", includedCount)
	}
	if excludedCount != 5 {
		t.Errorf("excludedCount = %d, want 5", excludedCount)
	}

	// manifest.json on disk must equal what Bootstrap returned.
	raw, err := os.ReadFile(filepath.Join(outputDir, "manifest.json"))
	if err != nil {
		t.Fatalf("read manifest.json: %v", err)
	}
	var onDisk domain.Generation
	if err := json.Unmarshal(raw, &onDisk); err != nil {
		t.Fatalf("decode manifest.json: %v", err)
	}
	if onDisk.Metadata.ID != gen.Metadata.ID {
		t.Errorf("manifest.json metadata.id = %q, want %q (returned value)", onDisk.Metadata.ID, gen.Metadata.ID)
	}
	if len(onDisk.Spec.Sources) != len(gen.Spec.Sources) {
		t.Errorf("manifest.json sources length = %d, want %d", len(onDisk.Spec.Sources), len(gen.Spec.Sources))
	}
}

// TestBootstrap_Manifest_DesiredGraphDigest_IsBootstrapPolicyDigest proves
// the documented schema-extension decision in doc.go: a bootstrap
// generation's desiredGraphDigest is BootstrapPolicyDigest(), not empty and
// not a real Profile digest (there is no Profile before PR-12).
func TestBootstrap_Manifest_DesiredGraphDigest_IsBootstrapPolicyDigest(t *testing.T) {
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
	outputDir := filepath.Join(t.TempDir(), "generation")
	gen, err := Bootstrap(req, outputDir)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	restoreWritable(t, outputDir)

	want, err := BootstrapPolicyDigest()
	if err != nil {
		t.Fatalf("BootstrapPolicyDigest: %v", err)
	}
	if gen.Spec.DesiredGraphDigest != want {
		t.Errorf("desiredGraphDigest = %q, want BootstrapPolicyDigest() = %q", gen.Spec.DesiredGraphDigest, want)
	}
	if !domain.IsCanonicalDigest(gen.Spec.DesiredGraphDigest) {
		t.Errorf("desiredGraphDigest %q is not a canonical sha256 digest", gen.Spec.DesiredGraphDigest)
	}
}
