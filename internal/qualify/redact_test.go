package qualify

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain/redact"
)

// TestFixtureContentIsRedactedBeforeLeavingTheProcess is the qualification
// suite item 10 proof (docs/knowledge/README.md §10: "secret redaction and
// proof that observation did not execute content") exercised against real,
// committed fixture content rather than a synthetic example: the
// claude-code mcp-merge case's input/claude-config/.claude.json fixture
// deliberately names one of its env values "EXAMPLE_TOKEN" (a field name
// carrying the sensitive-key signal internal/domain/redact's
// sensitiveKeyPattern matches, "token"), so this test proves that content,
// read the same way ObserveSandbox would read it, is fully redacted by the
// existing PR-04 redact package before any report/output path would emit
// it — a real fixture value never reaches output unredacted.
func TestFixtureContentIsRedactedBeforeLeavingTheProcess(t *testing.T) {
	path := filepath.Join(
		repoFixturesDir(), "claude-code", "2.1.211", "mcp-merge",
		"input", "claude-config", ".claude.json",
	)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", path, err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("unmarshal fixture %s: %v", path, err)
	}

	if !strings.Contains(string(raw), "user-level-placeholder-not-a-real-secret") {
		t.Fatal("fixture no longer contains the expected placeholder value; test needs updating")
	}

	redactedJSON, err := redact.JSON(parsed)
	if err != nil {
		t.Fatalf("redact.JSON: %v", err)
	}
	if strings.Contains(string(redactedJSON), "user-level-placeholder-not-a-real-secret") {
		t.Errorf("redact.JSON output still contains the fixture's placeholder secret value: %s", redactedJSON)
	}
	if !strings.Contains(string(redactedJSON), "REDACTED:sha256:") {
		t.Errorf("redact.JSON output does not contain a redaction marker at all: %s", redactedJSON)
	}

	report, err := redact.Report(parsed)
	if err != nil {
		t.Fatalf("redact.Report: %v", err)
	}
	if strings.Contains(report, "user-level-placeholder-not-a-real-secret") {
		t.Errorf("redact.Report output still contains the fixture's placeholder secret value: %s", report)
	}

	// The non-sensitive sibling field must survive redaction untouched --
	// otherwise this test could pass vacuously because everything got
	// wiped, not because redaction is field-selective.
	if !strings.Contains(string(redactedJSON), "user-only-server") {
		t.Errorf("redact.JSON over-redacted: lost the non-sensitive %q field entirely: %s", "user-only-server", redactedJSON)
	}
}
