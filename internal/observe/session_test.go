package observe

import (
	"path/filepath"
	"testing"
)

// TestObserve_Session_Codex_Golden is this PR's (issue #20) golden fixture
// for the `session` scope, Codex side: a caller-supplied CLI-flag fact
// (Codex's `-c mcp_servers.foo.command=...` override) and an env fact.
func TestObserve_Session_Codex_Golden(t *testing.T) {
	tr := newCodexTree(t)
	req := tr.request("0.144.5")
	req.SessionInputs = []SessionInput{
		{Concept: conceptMCPServer, Kind: "flag", Name: "-c mcp_servers.override.command", Value: "./local-run.sh"},
		{Concept: conceptPolicy, Kind: "env", Name: "CODEX_SANDBOX_MODE", Value: "workspace-write"},
	}

	obs, err := Observe(req)
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	assertValid(t, obs)

	wantIDs := []string{
		"codex:mcp_server:session:flag:-c mcp_servers.override.command",
		"codex:policy:session:env:CODEX_SANDBOX_MODE",
	}
	assertExactIDs(t, obs, wantIDs)

	for _, o := range obs {
		if o.Spec.Scope.Kind != "session" {
			t.Errorf("%s: scope.kind = %q, want %q", o.Metadata.ID, o.Spec.Scope.Kind, "session")
		}
		if o.Spec.EvidenceLevel != "E1" {
			t.Errorf("%s: evidenceLevel = %s, want E1", o.Metadata.ID, o.Spec.EvidenceLevel)
		}
	}

	flagObs := findObservation(t, obs, conceptMCPServer, "-c mcp_servers.override.command")
	if flagObs.Spec.Source.Kind != "flag" {
		t.Errorf("source.kind = %q, want %q", flagObs.Spec.Source.Kind, "flag")
	}
	content, ok := flagObs.Spec.OpaqueVendorFields["content"].(string)
	if !ok || content != "./local-run.sh" {
		t.Errorf("OpaqueVendorFields[content] = %v, want %q", flagObs.Spec.OpaqueVendorFields["content"], "./local-run.sh")
	}
}

// TestObserve_Session_ClaudeCode_Golden mirrors the Codex session golden
// test for Claude Code's `--mcp-config` flag.
func TestObserve_Session_ClaudeCode_Golden(t *testing.T) {
	tr := newClaudeTree(t)
	req := tr.request("2.1.211")
	req.SessionInputs = []SessionInput{
		{Concept: conceptMCPServer, Kind: "flag", Name: "--mcp-config", Value: filepath.Join("session", "extra.json")},
	}

	obs, err := Observe(req)
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	assertValid(t, obs)

	wantIDs := []string{
		"claude-code:mcp_server:session:flag:--mcp-config",
	}
	assertExactIDs(t, obs, wantIDs)
}

// TestObserve_Session_CombinesWithFilesystemSources proves session-scoped
// records coexist, in one deterministic sorted result, alongside ordinary
// filesystem-discovered records — Observe does not treat SessionInputs as a
// separate call.
func TestObserve_Session_CombinesWithFilesystemSources(t *testing.T) {
	tr := newCodexTree(t)
	mustWriteFile(t, filepath.Join(tr.CodexHome, "AGENTS.md"), "# base\n")

	req := tr.request("0.144.5")
	req.SessionInputs = []SessionInput{
		{Concept: conceptInstruction, Kind: "flag", Name: "--instructions", Value: "inline text"},
	}

	obs, err := Observe(req)
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if len(obs) != 2 {
		t.Fatalf("got %d observations, want 2 (one file, one session): %+v", len(obs), obs)
	}
	findObservation(t, obs, conceptInstruction, filepath.Join(tr.CodexHome, "AGENTS.md"))
	findObservation(t, obs, conceptInstruction, "--instructions")
}

func TestObserve_Session_UnknownConcept_Errors(t *testing.T) {
	tr := newCodexTree(t)
	req := tr.request("0.144.5")
	req.SessionInputs = []SessionInput{{Concept: "not-a-real-concept", Kind: "flag", Name: "--x", Value: "y"}}
	if _, err := Observe(req); err == nil {
		t.Fatal("Observe with an unknown SessionInput.Concept: want error, got nil")
	}
}

func TestObserve_Session_InvalidKind_Errors(t *testing.T) {
	tr := newCodexTree(t)
	req := tr.request("0.144.5")
	req.SessionInputs = []SessionInput{{Concept: conceptMCPServer, Kind: "not-flag-or-env", Name: "--x", Value: "y"}}
	if _, err := Observe(req); err == nil {
		t.Fatal("Observe with an invalid SessionInput.Kind: want error, got nil")
	}
}

func TestObserve_Session_EmptyName_Errors(t *testing.T) {
	tr := newCodexTree(t)
	req := tr.request("0.144.5")
	req.SessionInputs = []SessionInput{{Concept: conceptMCPServer, Kind: "flag", Name: "", Value: "y"}}
	if _, err := Observe(req); err == nil {
		t.Fatal("Observe with an empty SessionInput.Name: want error, got nil")
	}
}

// TestObserve_Session_Deterministic proves SessionInput handling holds the
// same "byte-identical repeat calls" invariant determinism_test.go proves
// for filesystem sources.
func TestObserve_Session_Deterministic(t *testing.T) {
	tr := newCodexTree(t)
	req := tr.request("0.144.5")
	req.SessionInputs = []SessionInput{
		{Concept: conceptPolicy, Kind: "env", Name: "CODEX_SANDBOX_MODE", Value: "workspace-write"},
		{Concept: conceptMCPServer, Kind: "flag", Name: "-c mcp_servers.a.command", Value: "./a"},
	}

	first, err := Observe(req)
	if err != nil {
		t.Fatalf("Observe (first): %v", err)
	}
	second, err := Observe(req)
	if err != nil {
		t.Fatalf("Observe (second): %v", err)
	}
	firstJSON := jsonRoundTrip(t, first)
	secondJSON := jsonRoundTrip(t, second)
	if firstJSON != secondJSON {
		t.Fatalf("Observe over SessionInputs is not deterministic:\nfirst:  %s\nsecond: %s", firstJSON, secondJSON)
	}
}
