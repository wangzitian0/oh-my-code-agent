package observe

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	hostcontext "github.com/wangzitian0/oh-my-code-agent/internal/context"
	"github.com/wangzitian0/oh-my-code-agent/internal/domain/redact"
)

// claudeJSONSiblingFixtureDir is fixtures/claude-code/2.1.211/
// user-config-sibling, a new, purpose-built synthetic fixture proving this
// fix against real Claude Code's actual layout: home/.claude.json sits
// directly under a bare-$HOME stand-in, a SIBLING of home/.claude/ (the
// CLAUDE_CONFIG_DIR default asset directory), never nested inside it. It
// also plants a decoy .claude.json at the OLD, wrong location
// (home/.claude/.claude.json) so the regression proof below is not
// trivially true just because nothing exists there.
//
// This fixture deliberately has no invocation.yaml, so internal/qualify's
// discoverFixtureCases (which walks fixtures/ for that exact filename) never
// picks it up — it exists purely for this package's own Observe() pipeline,
// not qualify's decoupled ObserveSandbox harness (which fixtures/claude-code/
// 2.1.211/mcp-merge already established has no relationship to
// claudeUserRules' real-path logic at all; see this PR's description).
//
// The fixture content itself is entirely synthetic: fake server names
// (matching this project's own real, safe-to-reference MCP server names —
// omsc, skynet-base, skynet-plan-and-gen-fast — used only as labels, never
// paired with any real credential), a fake-secret-shaped placeholder value
// under several sensitive-key-named fields (env, headers.Authorization,
// oauthAccount tokens, a nested per-project env block), and a multi-entry
// synthetic `projects` map with its own nested mcpServers/
// disabledMcpServers/enabledPlugins fields — the real, richer shape a
// production ~/.claude.json has, not the simplified single-mcpServers-map
// shape fixtures/claude-code/2.1.211/mcp-merge's fixture used.
func claudeJSONSiblingFixtureDir() string {
	return filepath.Join(repoFixturesDir(), "claude-code", "2.1.211", "user-config-sibling", "home")
}

// fixtureFakeSecret is the placeholder value planted under every
// sensitive-key-named field in the user-config-sibling fixture. It
// deliberately does NOT match internal/domain/redact's secretShapePattern
// (no "sk-"/"bearer "/"AKIA"-style prefix, no ENV_NAME=value shape) — like
// redact_test.go's existing realSecretPlaceholder, this isolates the
// key-name-based redaction path specifically: if sensitiveKeyPattern ever
// stopped matching these field names, this exact value would leak
// unredacted, since nothing about its own shape would catch it.
const fixtureFakeSecret = "fixture-fake-value-not-a-real-secret-7f3a9c"

// TestObserve_ClaudeJSONSibling_FoundAtSiblingLocation is the mandatory
// "positive" proof this fix requires: claudeUserRules/Observe() now finds
// mcp_server and policy candidates from a .claude.json placed as a sibling
// of the CLAUDE_CONFIG_DIR-equivalent asset directory, using the new
// synthetic fixture — the exact real-machine layout that produced 0
// mcp_server candidates before this fix despite several being configured.
func TestObserve_ClaudeJSONSibling_FoundAtSiblingLocation(t *testing.T) {
	home := claudeJSONSiblingFixtureDir()
	configDir := filepath.Join(home, ".claude")

	req := Request{
		Detection: hostcontext.HostDetection{
			Host:    "claude-code",
			Surface: "cli",
			Version: "2.1.211",
			NativeHomes: []hostcontext.NativeHome{
				{Name: "CLAUDE_CONFIG_DIR", Path: configDir},
				{Name: "HOME/.claude.json", Path: home},
			},
		},
	}

	obs, err := Observe(req)
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	assertValid(t, obs)

	siblingPath := filepath.Join(home, ".claude.json")
	mcp := findObservation(t, obs, conceptMCPServer, siblingPath)
	policy := findObservation(t, obs, conceptPolicy, siblingPath)

	content, ok := mcp.Spec.OpaqueVendorFields["content"].(map[string]any)
	if !ok {
		t.Fatalf(".claude.json OpaqueVendorFields[content] type = %T, want map[string]any", mcp.Spec.OpaqueVendorFields["content"])
	}
	servers, ok := content["mcpServers"].(map[string]any)
	if !ok || len(servers) != 3 {
		t.Fatalf("mcpServers parsed content = %+v, want a 3-entry map (omsc, skynet-base, skynet-plan-and-gen-fast)", content["mcpServers"])
	}
	for _, name := range []string{"omsc", "skynet-base", "skynet-plan-and-gen-fast"} {
		if _, ok := servers[name]; !ok {
			t.Errorf("mcpServers missing expected server %q: %+v", name, servers)
		}
	}
	projects, ok := content["projects"].(map[string]any)
	if !ok || len(projects) != 3 {
		t.Fatalf("projects parsed content = %+v, want a 3-entry map (alpha, beta, gamma)", content["projects"])
	}

	if policy.Spec.Source.Path != siblingPath {
		t.Errorf("policy observation Source.Path = %q, want %q", policy.Spec.Source.Path, siblingPath)
	}

	// Positive control: the sibling config-dir asset directory (CLAUDE.md)
	// must still be found normally, proving the two NativeHomes are
	// observed independently, not one at the expense of the other.
	findObservation(t, obs, conceptInstruction, filepath.Join(configDir, "CLAUDE.md"))
}

// TestObserve_ClaudeJSONSibling_OldNestedLocationNotObserved is the
// mandatory regression proof: the OLD (wrong) nested location
// ($CLAUDE_CONFIG_DIR-default/.claude.json) is correctly NOT where this
// package looks for .claude.json anymore. The fixture plants a decoy file
// at exactly that path (home/.claude/.claude.json) with obviously-fake
// content ("decoy-should-never-be-observed") — if this regressed back to
// the old behavior, this test would find and report the decoy's content
// instead of failing silently.
func TestObserve_ClaudeJSONSibling_OldNestedLocationNotObserved(t *testing.T) {
	home := claudeJSONSiblingFixtureDir()
	configDir := filepath.Join(home, ".claude")
	oldWrongPath := filepath.Join(configDir, ".claude.json")

	req := Request{
		Detection: hostcontext.HostDetection{
			Host:    "claude-code",
			Surface: "cli",
			Version: "2.1.211",
			NativeHomes: []hostcontext.NativeHome{
				{Name: "CLAUDE_CONFIG_DIR", Path: configDir},
				{Name: "HOME/.claude.json", Path: home},
			},
		},
	}

	obs, err := Observe(req)
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}

	if hasObservation(obs, conceptMCPServer, oldWrongPath) {
		t.Errorf("found an mcp_server observation at the OLD wrong nested path %s; the decoy planted there must never be observed", oldWrongPath)
	}
	if hasObservation(obs, conceptPolicy, oldWrongPath) {
		t.Errorf("found a policy observation at the OLD wrong nested path %s; the decoy planted there must never be observed", oldWrongPath)
	}

	raw := jsonRoundTrip(t, obs)
	if strings.Contains(raw, "decoy-should-never-be-observed") {
		t.Errorf("Observe() output contains the decoy file's content; it must never be read:\n%s", raw)
	}
}

// TestObserve_ClaudeJSONSibling_RedactionSafe extends
// TestObserve_RedactionSafe_ParsedJSONEnvBlock's pattern (key-name-based
// redaction over structurally-parsed JSON) against this richer, more
// realistic fixture shape: a top-level mcpServers map with env AND headers
// secret-shaped fields, an oauthAccount block, and a multi-entry projects
// map whose own nested mcpServers carry a project-scoped secret too —
// proving the redaction boundary holds at every nesting depth this shape
// actually has, not just the single-level shape the older, simpler fixture
// exercised.
func TestObserve_ClaudeJSONSibling_RedactionSafe(t *testing.T) {
	home := claudeJSONSiblingFixtureDir()
	configDir := filepath.Join(home, ".claude")

	req := Request{
		Detection: hostcontext.HostDetection{
			Host:    "claude-code",
			Surface: "cli",
			Version: "2.1.211",
			NativeHomes: []hostcontext.NativeHome{
				{Name: "CLAUDE_CONFIG_DIR", Path: configDir},
				{Name: "HOME/.claude.json", Path: home},
			},
		},
	}

	obs, err := Observe(req)
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if len(obs) == 0 {
		t.Fatal("Observe returned no observations; this test would be vacuous")
	}

	rawJSON, err := json.Marshal(obs)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	// Sanity: the fixture's fake secret must appear in raw (unredacted)
	// output at least 4 times (top-level env, top-level headers, two
	// oauthAccount token fields, one nested per-project env) -- otherwise
	// this test is not exercising every nesting depth it claims to.
	if got := strings.Count(string(rawJSON), fixtureFakeSecret); got < 4 {
		t.Fatalf("raw (unredacted) Observe() output contains the fixture secret only %d times, want >= 4 (top-level env, headers, 2 oauth tokens, nested project env); this test would under-exercise the redaction boundary:\n%s", got, rawJSON)
	}

	redacted, err := redact.JSON(obs)
	if err != nil {
		t.Fatalf("redact.JSON: %v", err)
	}
	if strings.Contains(string(redacted), fixtureFakeSecret) {
		t.Fatalf("redact.JSON(Observe() output) leaked the fixture secret:\n%s", redacted)
	}
	if !strings.Contains(string(redacted), "REDACTED:sha256:") {
		t.Error("expected the redacted output to contain at least one REDACTED marker")
	}

	// Non-secret content nested inside the projects map (a project-scoped
	// server name and an enabled-plugin id, both several levels deep) must
	// survive redaction -- this must not be a redact-everything result that
	// would trivially satisfy the checks above.
	for _, mustSurvive := range []string{"alpha-only-server", "gamma-only-server", "my-marketplace/deploy-helper"} {
		if !strings.Contains(string(redacted), mustSurvive) {
			t.Errorf("expected non-secret nested content %q to survive redaction", mustSurvive)
		}
	}
}
