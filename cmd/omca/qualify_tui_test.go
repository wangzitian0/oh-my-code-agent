package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	hostcontext "github.com/wangzitian0/oh-my-code-agent/internal/context"
)

func TestQualificationEnvironmentPreservesHumanTerminal(t *testing.T) {
	root := t.TempDir()
	realEnv := hostcontext.Environment{Vars: []string{
		"HOME=/native/home", "TERM=xterm-256color", "LANG=en_US.UTF-8",
	}}
	env, _, err := tuiQualificationEnvironment(root, nil, realEnv)
	if err != nil {
		t.Fatal(err)
	}
	got := hostcontext.Environment{Vars: env}
	if got.Get("TERM") != "xterm-256color" || got.Get("LANG") != "en_US.UTF-8" || got.Get("NO_COLOR") != "" {
		t.Fatalf("human terminal capabilities were overridden: %v", env)
	}
	if got.Get("HOME") != filepath.Join(root, "home") || got.Get("XDG_STATE_HOME") != filepath.Join(root, "state") {
		t.Fatalf("preserving the terminal must retain isolation: %v", env)
	}
}

func TestQualificationDiagnosticRetainsHostFailureAfterLaunchReport(t *testing.T) {
	stderr := strings.Repeat("launch context-cost estimate; ", 30) + "\nError: unsupported host setting"
	detail := boundedDiagnostic(stderr, errors.New("exit status 1"))
	if !strings.Contains(detail, "Error: unsupported host setting") || !strings.HasSuffix(detail, "exit status 1") {
		t.Fatalf("host failure hidden by launch report: %q", detail)
	}
	if len(detail) > 303 {
		t.Fatalf("diagnostic is unbounded: %d bytes", len(detail))
	}
}

func TestParseTUIQualificationArgs_DefaultsAndClaudeAlias(t *testing.T) {
	got, err := parseTUIQualificationArgs(nil)
	if err != nil {
		t.Fatalf("parseTUIQualificationArgs(nil): %v", err)
	}
	if got.Host != "all" || got.JSON || got.Keep || got.Interactive {
		t.Fatalf("defaults = %+v", got)
	}

	got, err = parseTUIQualificationArgs([]string{"--host", "claude", "--json", "--keep"})
	if err != nil {
		t.Fatalf("parseTUIQualificationArgs(alias): %v", err)
	}
	if got.Host != "claude-code" || !got.JSON || !got.Keep {
		t.Fatalf("alias parse = %+v", got)
	}
}

func TestParseTUIQualificationArgs_InteractiveRequiresHumanAck(t *testing.T) {
	t.Setenv(interactiveAckEnv, "")
	_, err := parseTUIQualificationArgs([]string{"--interactive"})
	if err == nil || !strings.Contains(err.Error(), interactiveAckEnv) {
		t.Fatalf("interactive without acknowledgement err = %v", err)
	}

	t.Setenv(interactiveAckEnv, "1")
	got, err := parseTUIQualificationArgs([]string{"--interactive", "--host", "codex"})
	if err != nil {
		t.Fatalf("interactive with acknowledgement: %v", err)
	}
	if !got.Interactive || got.Host != "codex" {
		t.Fatalf("interactive parse = %+v", got)
	}
}

func TestEvaluateCodexMCPList_RequiresOMCAAndExcludesSentinel(t *testing.T) {
	pass := evaluateCodexMCPList(`[{"name":"omca"}]`, "", 0, nil)
	if pass.Status != "PASS" || pass.Evidence != "E3" {
		t.Fatalf("pass = %+v", pass)
	}

	fail := evaluateCodexMCPList(`[{"name":"omca"},{"name":"omca-native-sentinel"}]`, "", 0, nil)
	if fail.Status != "FAIL" {
		t.Fatalf("sentinel leak = %+v", fail)
	}
}

func TestEvaluateClaudeMCPList_RequiresConnectedOMCAAndExcludesSentinel(t *testing.T) {
	pass := evaluateClaudeMCPList("Checking MCP server health…\n\nomca: /tmp/omca mcp serve - ✔ Connected\n", "", 0, nil)
	if pass.Status != "PASS" || pass.Evidence != "E3" {
		t.Fatalf("pass = %+v", pass)
	}

	fail := evaluateClaudeMCPList("omca: connected\nomca-native-sentinel: connected\n", "", 0, nil)
	if fail.Status != "FAIL" {
		t.Fatalf("sentinel leak = %+v", fail)
	}
}

func TestEvaluateCodexSkillsList_DecodesStreamAndExcludesSentinel(t *testing.T) {
	stream := strings.Join([]string{
		`{"id":1,"result":{"userAgent":"test"}}`,
		`{"method":"skills/changed","params":{}}`,
		`{"id":2,"result":{"data":[{"cwd":"/repo","skills":[{"name":"bundled","path":"/isolated/.system/bundled/SKILL.md"},{"name":"omca-managed-sentinel","path":"/repo/.agents/skills/omca-managed-sentinel/SKILL.md"}],"errors":[]}]}}`,
	}, "\n")
	pass := evaluateCodexSkillsList(stream, "", 0, nil, "/repo")
	if pass.Status != "PASS" || !strings.Contains(pass.Detail, "2 visible Skills") {
		t.Fatalf("pass = %+v", pass)
	}

	leak := strings.Replace(stream, `"name":"bundled"`, `"name":"`+qualificationSentinel+`"`, 1)
	fail := evaluateCodexSkillsList(leak, "", 0, nil, "/repo")
	if fail.Status != "FAIL" {
		t.Fatalf("sentinel leak = %+v", fail)
	}
}

func TestEvaluateCodexSkillsList_FailsClosedOnErrorOrMalformedResult(t *testing.T) {
	tests := []struct {
		name   string
		stream string
	}{
		{name: "error response", stream: `{"id":2,"error":{"code":-32602,"message":"bad params"}}`},
		{name: "missing result", stream: `{"id":2}`},
		{name: "missing data", stream: `{"id":2,"result":{}}`},
		{name: "wrong cwd", stream: `{"id":2,"result":{"data":[{"cwd":"/other","skills":[]}]}}`},
		{name: "managed Skill missing", stream: `{"id":2,"result":{"data":[{"cwd":"/repo","skills":[{"name":"bundled"}]}]}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := evaluateCodexSkillsList(tt.stream, "", 0, nil, "/repo")
			if got.Status != "FAIL" {
				t.Fatalf("evaluateCodexSkillsList() = %+v, want FAIL", got)
			}
		})
	}
}

func TestQualificationPendingChecksUseDetectedHostTruth(t *testing.T) {
	claude := unavailableClaudeSkillInventory("2.1.228")
	if claude.Status != "UNKNOWN" || !strings.Contains(claude.Detail, "2.1.228") || strings.Contains(claude.Detail, "2.1.220") {
		t.Fatalf("Claude Skill UNKNOWN = %+v", claude)
	}

	pending := pendingHumanTUIQualification("codex")
	if pending.Status != "UNKNOWN" || pending.ID != "human-interactive-tui" || allQualificationChecksPass([]tuiQualificationCheck{pending}) {
		t.Fatalf("pending human gate = %+v", pending)
	}
}

func TestSeedTUIQualificationScratch_CreatesOnlyScratchSentinels(t *testing.T) {
	root := t.TempDir()
	if err := seedTUIQualificationScratch(root); err != nil {
		t.Fatalf("seedTUIQualificationScratch: %v", err)
	}
	for _, path := range []string{
		filepath.Join(root, "home", ".codex", "config.toml"),
		filepath.Join(root, "home", ".claude.json"),
		filepath.Join(root, "home", ".agents", "skills", qualificationSentinel, "SKILL.md"),
		filepath.Join(root, "home", ".codex", "skills", qualificationSentinel, "SKILL.md"),
		filepath.Join(root, "home", ".claude", "skills", qualificationSentinel, "SKILL.md"),
		filepath.Join(root, "repo", ".agents", "skills", qualificationManagedSkill, "SKILL.md"),
		filepath.Join(root, "repo", ".claude", "skills", qualificationManagedSkill, "SKILL.md"),
		filepath.Join(root, "repo", ".git"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("expected scratch asset %s: %v", path, err)
		}
	}
}

func TestRemoveQualificationScratch_NeverChmodsExternalSymlinkTarget(t *testing.T) {
	root := t.TempDir()
	externalDir := t.TempDir()
	external := filepath.Join(externalDir, "host-binary")
	if err := os.WriteFile(external, []byte("host"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(root, "scratch-link")); err != nil {
		t.Fatal(err)
	}

	removeQualificationScratch(root)

	info, err := os.Stat(external)
	if err != nil {
		t.Fatalf("external target was removed: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o755 {
		t.Fatalf("external target mode = %o, want 755 (cleanup followed the scratch symlink)", got)
	}
}

func TestApplyHumanTUIQualification_UpgradesOnlyClaudeSkillUnknown(t *testing.T) {
	host := tuiHostQualification{
		Host: "claude-code",
		Checks: []tuiQualificationCheck{
			{ID: "knowledge-pack", Status: "PASS", Evidence: "E2"},
			{ID: "native-mcp-exclusion", Status: "PASS", Evidence: "E3"},
			{ID: "skill-isolation", Status: "UNKNOWN", Evidence: "E1"},
		},
	}
	human := tuiQualificationCheck{ID: "human-interactive-tui", Status: "PASS", Evidence: "E4"}
	applyHumanTUIQualification(&host, human)
	if got := host.Checks[2]; got.Status != "PASS" || got.Evidence != "E4" {
		t.Fatalf("Claude Skill check after human qualification = %+v", got)
	}

	host.Checks[2] = tuiQualificationCheck{ID: "skill-isolation", Status: "UNKNOWN", Evidence: "E1"}
	applyHumanTUIQualification(&host, tuiQualificationCheck{ID: "human-interactive-tui", Status: "UNKNOWN", Evidence: "E0"})
	if got := host.Checks[2]; got.Status != "UNKNOWN" {
		t.Fatalf("unattested human qualification upgraded check: %+v", got)
	}
}
