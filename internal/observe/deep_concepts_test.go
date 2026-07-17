package observe

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
)

// This file holds one golden fixture (issue #20's round-3 acceptance
// criterion: "Each concept x host pair has at least one fixture with a
// golden expected inventory (entry count + logical IDs)") for each of the
// three concepts PR-16 adds — hook, policy, plugin — for each host, so
// "inventoried" is falsifiable per concept x host cell rather than only
// demonstrated incidentally inside the broader *_FullLayout/system/
// directory tests elsewhere in this package. Instructions/Skills/MCP
// already have this from PR-08's TestObserve_Codex_FullLayout/
// TestObserve_ClaudeCode_FullLayout.

// TestObserve_Hook_Codex_Golden: user-scope config.toml multiplexed as a
// hook source (rules.go's codexUserRules doc comment explains the
// multiplexing rationale).
func TestObserve_Hook_Codex_Golden(t *testing.T) {
	tr := newCodexTree(t)
	mustWriteFile(t, filepath.Join(tr.CodexHome, "config.toml"), "[mcp_servers.x]\ncommand = \"npx\"\n")

	obs, err := Observe(tr.request("0.144.5"))
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	assertExactIDs(t, filterByConcept(obs, conceptHook), []string{
		"codex:hook:" + filepath.Join(tr.CodexHome, "config.toml"),
	})
}

// TestObserve_Hook_ClaudeCode_Golden: user-scope settings.json/
// settings.local.json.
func TestObserve_Hook_ClaudeCode_Golden(t *testing.T) {
	tr := newClaudeTree(t)
	mustWriteFile(t, filepath.Join(tr.ClaudeConfigDir, "settings.json"),
		`{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo pre"}]}]}}`)
	mustWriteFile(t, filepath.Join(tr.ClaudeConfigDir, "settings.local.json"), `{"hooks":{}}`)

	obs, err := Observe(tr.request("2.1.211"))
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	assertExactIDs(t, filterByConcept(obs, conceptHook), []string{
		"claude-code:hook:" + filepath.Join(tr.ClaudeConfigDir, "settings.json"),
		"claude-code:hook:" + filepath.Join(tr.ClaudeConfigDir, "settings.local.json"),
	})

	// Prove the "event, command, observed prompt/tool fields" AC:
	// settings.json is JSON-shaped, so its hooks key is structurally
	// parsed and every field (event name "PreToolUse", the tool matcher,
	// the command string) survives losslessly.
	hook := findObservation(t, obs, conceptHook, filepath.Join(tr.ClaudeConfigDir, "settings.json"))
	content, ok := hook.Spec.OpaqueVendorFields["content"].(map[string]any)
	if !ok {
		t.Fatalf("settings.json OpaqueVendorFields[content] type = %T, want map[string]any", hook.Spec.OpaqueVendorFields["content"])
	}
	hooksField, ok := content["hooks"].(map[string]any)
	if !ok {
		t.Fatalf("parsed settings.json missing hooks field: %+v", content)
	}
	if _, ok := hooksField["PreToolUse"]; !ok {
		t.Errorf("parsed settings.json lost the PreToolUse event key: %+v", hooksField)
	}
}

// TestObserve_Policy_Codex_Golden: config.toml multiplexed as policy, plus
// the discoverOnly auth.json credential file — proving both halves of
// issue #20's "permission/trust inventoried WITHOUT reading credential
// material" requirement in one fixture.
func TestObserve_Policy_Codex_Golden(t *testing.T) {
	tr := newCodexTree(t)
	mustWriteFile(t, filepath.Join(tr.CodexHome, "config.toml"), "approval_policy = \"never\"\n")
	const realSecret = "this-is-a-real-looking-oauth-token-that-must-never-be-read"
	mustWriteFile(t, filepath.Join(tr.CodexHome, "auth.json"), `{"OPENAI_API_KEY":"`+realSecret+`"}`)

	obs, err := Observe(tr.request("0.144.5"))
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	assertExactIDs(t, filterByConcept(obs, conceptPolicy), []string{
		"codex:policy:" + filepath.Join(tr.CodexHome, "config.toml"),
		"codex:policy:" + filepath.Join(tr.CodexHome, "auth.json"),
	})

	authObs := findObservation(t, obs, conceptPolicy, filepath.Join(tr.CodexHome, "auth.json"))
	if authObs.Spec.EvidenceLevel != domain.EvidenceLevelDiscovered {
		t.Errorf("auth.json evidenceLevel = %s, want E0 (discovered, never read)", authObs.Spec.EvidenceLevel)
	}
	if _, ok := authObs.Spec.OpaqueVendorFields["content"]; ok {
		t.Error("auth.json OpaqueVendorFields must never carry a content field — its content is never read")
	}
	raw := jsonRoundTrip(t, obs)
	if strings.Contains(raw, realSecret) {
		t.Fatalf("Observe() output leaked auth.json content it must never have read:\n%s", raw)
	}
}

// TestObserve_Policy_ClaudeCode_Golden: .claude.json (already read for MCP,
// re-tagged policy) plus settings.json's permissions block.
func TestObserve_Policy_ClaudeCode_Golden(t *testing.T) {
	tr := newClaudeTree(t)
	mustWriteFile(t, filepath.Join(tr.ClaudeConfigDir, ".claude.json"), `{"mcpServers":{},"projects":{}}`)
	mustWriteFile(t, filepath.Join(tr.ClaudeConfigDir, "settings.json"), `{"permissions":{"deny":["Bash(rm -rf *)"]}}`)

	obs, err := Observe(tr.request("2.1.211"))
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	assertExactIDs(t, filterByConcept(obs, conceptPolicy), []string{
		"claude-code:policy:" + filepath.Join(tr.ClaudeConfigDir, ".claude.json"),
		"claude-code:policy:" + filepath.Join(tr.ClaudeConfigDir, "settings.json"),
	})
}

// TestObserve_Plugin_Codex_Golden: this package's own convention-picked
// plugin.json marker walk under CODEX_HOME (system.go/rules.go document
// this is NOT an independently confirmed vendor path).
func TestObserve_Plugin_Codex_Golden(t *testing.T) {
	tr := newCodexTree(t)
	mustWriteFile(t, filepath.Join(tr.CodexHome, "plugins", "shipit", ".codex-plugin", "plugin.json"), `{"name":"shipit"}`)
	// A non-plugin.json file elsewhere in CODEX_HOME must not be swept up.
	mustWriteFile(t, filepath.Join(tr.CodexHome, "plugins", "shipit", "README.md"), "not a plugin marker\n")

	obs, err := Observe(tr.request("0.144.5"))
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	assertExactIDs(t, filterByConcept(obs, conceptPlugin), []string{
		"codex:plugin:" + filepath.Join(tr.CodexHome, "plugins", "shipit", ".codex-plugin", "plugin.json"),
	})
}

// TestObserve_Plugin_ClaudeCode_Golden: settings.json's enabled-plugin
// settings (rules.go's claudeUserRules doc comment).
func TestObserve_Plugin_ClaudeCode_Golden(t *testing.T) {
	tr := newClaudeTree(t)
	mustWriteFile(t, filepath.Join(tr.ClaudeConfigDir, "settings.json"), `{"enabledPlugins":["my-marketplace/deploy-helper"]}`)

	obs, err := Observe(tr.request("2.1.211"))
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	assertExactIDs(t, filterByConcept(obs, conceptPlugin), []string{
		"claude-code:plugin:" + filepath.Join(tr.ClaudeConfigDir, "settings.json"),
	})
}

// filterByConcept returns every observation in obs with the given concept.
func filterByConcept(obs []domain.Observation, concept string) []domain.Observation {
	var out []domain.Observation
	for _, o := range obs {
		if o.Spec.Concept == concept {
			out = append(out, o)
		}
	}
	return out
}
