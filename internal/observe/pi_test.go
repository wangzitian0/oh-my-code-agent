package observe

import (
	"path/filepath"
	"testing"

	hostcontext "github.com/wangzitian0/oh-my-code-agent/internal/context"
	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
)

// piTree is pi's synthetic host layout, mirroring codexTree/claudeTree: a
// relocated-or-default agent dir (PI_CODING_AGENT_DIR), the shared
// $HOME/.agents/skills root, and a worktree root. WorkingDir allows the
// directory-chain rules (piDirectoryChainRules) to be exercised: pi
// documents context files and .agents/skills as resolving from the project
// root down to cwd, so an intermediate directory adds records the two
// root-only scopes do not see.
type piTree struct {
	PiAgentDir    string
	HomeAgentsDir string
	WorktreeRoot  string
	WorkingDir    string
}

func newPiTree(t *testing.T) piTree {
	t.Helper()
	root := t.TempDir()
	worktree := filepath.Join(root, "project")
	return piTree{
		PiAgentDir:    filepath.Join(root, "pi-agent"),
		HomeAgentsDir: filepath.Join(root, "home", ".agents", "skills"),
		WorktreeRoot:  worktree,
		WorkingDir:    filepath.Join(worktree, "pkg"),
	}
}

func (tr piTree) request(version string) Request {
	return Request{
		Detection: hostcontext.HostDetection{
			Host:    "pi",
			Surface: "cli",
			Version: version,
			NativeHomes: []hostcontext.NativeHome{
				{Name: "PI_CODING_AGENT_DIR", Path: tr.PiAgentDir, FromEnvVar: "PI_CODING_AGENT_DIR"},
				{Name: "HOME/.agents/skills", Path: tr.HomeAgentsDir},
			},
		},
		WorktreeRoot:    tr.WorktreeRoot,
		WorkingDirectory: tr.WorkingDir,
	}
}

// TestObserve_Pi_FullLayout mirrors TestObserve_Codex_FullLayout for pi:
// every user-scope and workspace-scope source piUserRules/piWorkspaceRules
// know about is present exactly once, the discoverOnly auth.json is
// reported at E0 without its content, and no fabricated records appear.
func TestObserve_Pi_FullLayout(t *testing.T) {
	tr := newPiTree(t)

	// PI_CODING_AGENT_DIR (user scope).
	mustWriteFile(t, filepath.Join(tr.PiAgentDir, "AGENTS.md"), "# global pi instructions\n")
	mustWriteFile(t, filepath.Join(tr.PiAgentDir, "SYSTEM.md"), "# replaces base prompt\n")
	mustWriteFile(t, filepath.Join(tr.PiAgentDir, "APPEND_SYSTEM.md"), "# appends to base prompt\n")
	mustWriteFile(t, filepath.Join(tr.PiAgentDir, "settings.json"), "{\"theme\":\"light\"}\n")
	mustWriteFile(t, filepath.Join(tr.PiAgentDir, "trust.json"), "{\"/some/project\":true}\n")
	mustWriteFile(t, filepath.Join(tr.PiAgentDir, "auth.json"), "{\"token\":\"secret-material\"}\n")
	mustWriteFile(t, filepath.Join(tr.PiAgentDir, "skills", "deploy", "SKILL.md"), "---\nname: deploy\n---\nbody\n")
	// Non-marker file inside the skills tree must not be reported.
	mustWriteFile(t, filepath.Join(tr.PiAgentDir, "skills", "deploy", "README.md"), "not a skill marker\n")
	mustWriteFile(t, filepath.Join(tr.PiAgentDir, "extensions", "a.ts"), "export const x = 1\n")
	mustWriteFile(t, filepath.Join(tr.PiAgentDir, "extensions", "b", "index.ts"), "export const y = 2\n")
	mustWriteFile(t, filepath.Join(tr.PiAgentDir, "npm", "pkg", "package.json"), "{\"name\":\"pkg\"}\n")

	// $HOME/.agents/skills (user scope, shared root).
	mustWriteFile(t, filepath.Join(tr.HomeAgentsDir, "shared", "SKILL.md"), "---\nname: shared\n---\nbody\n")

	// Worktree root (workspace scope): all three instruction candidates
	// exist; the inventory reports each without precedence filtering.
	mustWriteFile(t, filepath.Join(tr.WorktreeRoot, "AGENTS.override.md"), "# override\n")
	mustWriteFile(t, filepath.Join(tr.WorktreeRoot, "AGENTS.md"), "# project\n")
	mustWriteFile(t, filepath.Join(tr.WorktreeRoot, "CLAUDE.md"), "# fallback\n")
	mustWriteFile(t, filepath.Join(tr.WorktreeRoot, ".pi", "SYSTEM.md"), "# project system\n")
	mustWriteFile(t, filepath.Join(tr.WorktreeRoot, ".pi", "APPEND_SYSTEM.md"), "# project append\n")
	mustWriteFile(t, filepath.Join(tr.WorktreeRoot, ".pi", "settings.json"), "{\"skills\":[]}\n")
	mustWriteFile(t, filepath.Join(tr.WorktreeRoot, ".pi", "skills", "proj", "SKILL.md"), "---\nname: proj\n---\nbody\n")
	mustWriteFile(t, filepath.Join(tr.WorktreeRoot, ".agents", "skills", "proj-skill", "SKILL.md"), "---\nname: proj-skill\n---\nbody\n")
	mustWriteFile(t, filepath.Join(tr.WorktreeRoot, ".pi", "extensions", "e.ts"), "export const z = 3\n")
	mustWriteFile(t, filepath.Join(tr.WorktreeRoot, ".pi", "npm", "p2", "package.json"), "{\"name\":\"p2\"}\n")

	// Intermediate directory (directory scope): context candidates and the
	// ancestor .agents/skills root are documented as resolving through the
	// root-to-cwd chain; .pi/* sources deliberately are not.
	mustWriteFile(t, filepath.Join(tr.WorkingDir, "AGENTS.md"), "# nested instructions\n")
	mustWriteFile(t, filepath.Join(tr.WorkingDir, ".agents", "skills", "nested", "SKILL.md"), "---\nname: nested\n---\nbody\n")
	mustWriteFile(t, filepath.Join(tr.WorkingDir, ".pi", "settings.json"), "{\"ignored\":true}\n")

	obs, err := Observe(tr.request("0.85.1"))
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	assertValid(t, obs)

	type want struct {
		concept string
		path    string
		scope   string
		root    string
		e0      bool
	}
	wants := []want{
		// User scope, PI_CODING_AGENT_DIR.
		{conceptInstruction, filepath.Join(tr.PiAgentDir, "AGENTS.md"), "user", tr.PiAgentDir, false},
		{conceptInstruction, filepath.Join(tr.PiAgentDir, "SYSTEM.md"), "user", tr.PiAgentDir, false},
		{conceptInstruction, filepath.Join(tr.PiAgentDir, "APPEND_SYSTEM.md"), "user", tr.PiAgentDir, false},
		{conceptPolicy, filepath.Join(tr.PiAgentDir, "settings.json"), "user", tr.PiAgentDir, false},
		{conceptPolicy, filepath.Join(tr.PiAgentDir, "trust.json"), "user", tr.PiAgentDir, false},
		{conceptPolicy, filepath.Join(tr.PiAgentDir, "auth.json"), "user", tr.PiAgentDir, true},
		{conceptSkill, filepath.Join(tr.PiAgentDir, "skills", "deploy", "SKILL.md"), "user", tr.PiAgentDir, false},
		{conceptPlugin, filepath.Join(tr.PiAgentDir, "extensions", "a.ts"), "user", tr.PiAgentDir, false},
		{conceptPlugin, filepath.Join(tr.PiAgentDir, "extensions", "b", "index.ts"), "user", tr.PiAgentDir, false},
		{conceptPlugin, filepath.Join(tr.PiAgentDir, "npm", "pkg", "package.json"), "user", tr.PiAgentDir, false},
		// User scope, shared root.
		{conceptSkill, filepath.Join(tr.HomeAgentsDir, "shared", "SKILL.md"), "user", tr.HomeAgentsDir, false},
		// Workspace scope, worktree root.
		{conceptInstruction, filepath.Join(tr.WorktreeRoot, "AGENTS.override.md"), "workspace", tr.WorktreeRoot, false},
		{conceptInstruction, filepath.Join(tr.WorktreeRoot, "AGENTS.md"), "workspace", tr.WorktreeRoot, false},
		{conceptInstruction, filepath.Join(tr.WorktreeRoot, "CLAUDE.md"), "workspace", tr.WorktreeRoot, false},
		{conceptInstruction, filepath.Join(tr.WorktreeRoot, ".pi", "SYSTEM.md"), "workspace", tr.WorktreeRoot, false},
		{conceptInstruction, filepath.Join(tr.WorktreeRoot, ".pi", "APPEND_SYSTEM.md"), "workspace", tr.WorktreeRoot, false},
		{conceptPolicy, filepath.Join(tr.WorktreeRoot, ".pi", "settings.json"), "workspace", tr.WorktreeRoot, false},
		{conceptSkill, filepath.Join(tr.WorktreeRoot, ".pi", "skills", "proj", "SKILL.md"), "workspace", tr.WorktreeRoot, false},
		{conceptSkill, filepath.Join(tr.WorktreeRoot, ".agents", "skills", "proj-skill", "SKILL.md"), "workspace", tr.WorktreeRoot, false},
		{conceptPlugin, filepath.Join(tr.WorktreeRoot, ".pi", "extensions", "e.ts"), "workspace", tr.WorktreeRoot, false},
		{conceptPlugin, filepath.Join(tr.WorktreeRoot, ".pi", "npm", "p2", "package.json"), "workspace", tr.WorktreeRoot, false},
		// Directory scope, intermediate chain segment.
		{conceptInstruction, filepath.Join(tr.WorkingDir, "AGENTS.md"), "directory", tr.WorkingDir, false},
		{conceptSkill, filepath.Join(tr.WorkingDir, ".agents", "skills", "nested", "SKILL.md"), "directory", tr.WorkingDir, false},
	}
	for _, w := range wants {
		o := findObservation(t, obs, w.concept, w.path)
		if o.Spec.Scope.Kind != w.scope {
			t.Errorf("%s: scope.kind = %q, want %q", w.path, o.Spec.Scope.Kind, w.scope)
		}
		if o.Spec.Scope.Root != w.root {
			t.Errorf("%s: scope.root = %q, want %q", w.path, o.Spec.Scope.Root, w.root)
		}
		if w.e0 {
			if o.Spec.EvidenceLevel != domain.EvidenceLevelDiscovered {
				t.Errorf("%s: evidenceLevel = %s, want E0 (discoverOnly credential state)", w.path, o.Spec.EvidenceLevel)
			}
		} else if o.Spec.EvidenceLevel != domain.EvidenceLevelParsed {
			t.Errorf("%s: evidenceLevel = %s, want E1", w.path, o.Spec.EvidenceLevel)
		}
		if o.Spec.Host.ID != "pi" || o.Spec.Host.Version != "0.85.1" {
			t.Errorf("%s: host = %+v, want pi 0.85.1", w.path, o.Spec.Host)
		}
	}
	if hasObservation(obs, conceptSkill, filepath.Join(tr.PiAgentDir, "skills", "deploy", "README.md")) {
		t.Error("README.md next to a skill package must not be reported as a skill (marker-restricted)")
	}
	// The intermediate directory's .pi/settings.json must NOT appear: pi's
	// .pi/* project sources are root-only, not ancestor-chain (see
	// piDirectoryChainRules's doc comment).
	if hasObservation(obs, conceptPolicy, filepath.Join(tr.WorkingDir, ".pi", "settings.json")) {
		t.Error("intermediate-directory .pi/settings.json must not be reported (pi .pi/* sources are workspace-root-only)")
	}
	if len(obs) != len(wants) {
		t.Errorf("got %d observations, want exactly %d", len(obs), len(wants))
		for _, o := range obs {
			t.Logf("  got: %s %s (%s)", o.Spec.Concept, o.Spec.Source.Path, o.Spec.Scope.Kind)
		}
	}
}

// TestObserve_Pi_AuthJsonContentNeverRead proves the credential-state file
// is inventoried without its content ever being exposed through the parsed
// representation — the same zerowrite/redact discipline Codex's auth.json
// follows (rules.go's discoverOnly doc comment).
func TestObserve_Pi_AuthJsonContentNeverRead(t *testing.T) {
	tr := newPiTree(t)
	secret := "sk-pi-secret-token-material"
	mustWriteFile(t, filepath.Join(tr.PiAgentDir, "auth.json"), "{\"apiKey\":\""+secret+"\"}\n")

	obs, err := Observe(tr.request("0.85.1"))
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	o := findObservation(t, obs, conceptPolicy, filepath.Join(tr.PiAgentDir, "auth.json"))
	if o.Spec.EvidenceLevel != domain.EvidenceLevelDiscovered {
		t.Errorf("evidenceLevel = %s, want E0", o.Spec.EvidenceLevel)
	}
	for _, v := range o.Spec.OpaqueVendorFields {
		if s, ok := v.(string); ok && len(s) > 0 {
			t.Errorf("OpaqueVendorFields contains non-empty content for a discoverOnly credential file: %q", s)
		}
	}
}
