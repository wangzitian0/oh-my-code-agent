package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunAuditCLIHumanAndJSON(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "audit_cli_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	_ = os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte("package main\nfunc main(){}"), 0644)
	_ = os.WriteFile(filepath.Join(tmpDir, "main_test.go"), []byte("package main\nimport \"testing\"\nfunc TestMain(t *testing.T){}"), 0644)

	// Test Human Markdown output
	var stdout, stderr bytes.Buffer
	code := runAudit(&stdout, &stderr, []string{tmpDir})
	if code != 0 {
		t.Errorf("expected exit code 0, got %d. stderr: %s", code, stderr.String())
	}
	output := stdout.String()
	if !strings.Contains(output, "3 马仔精简代码审计报告") {
		t.Errorf("expected markdown report, got: %s", output)
	}
	if !strings.Contains(output, "PASS") {
		t.Errorf("expected PASS verdict on clean test repo, got: %s", output)
	}

	// Test JSON output
	stdout.Reset()
	stderr.Reset()
	code = runAudit(&stdout, &stderr, []string{"--json", tmpDir})
	if code != 0 {
		t.Errorf("expected exit code 0 for json mode, got %d", code)
	}
	jsonOut := stdout.String()
	if !strings.Contains(jsonOut, `"total_scouts_deployed": 3`) {
		t.Errorf("expected json with total_scouts_deployed: 3, got: %s", jsonOut)
	}
}

func TestRunAuditCatchesPPTProject(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "audit_cli_fake_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Docs only, 0 code!
	_ = os.WriteFile(filepath.Join(tmpDir, "README.md"), []byte("# Big Distributed System"), 0644)

	var stdout, stderr bytes.Buffer
	code := runAudit(&stdout, &stderr, []string{tmpDir})
	if code != 1 {
		t.Errorf("expected exit code 1 (BLOCKED) for PPT project, got %d", code)
	}
	output := stdout.String()
	if !strings.Contains(output, "PPT Only Project") {
		t.Errorf("expected PPT blocker, got: %s", output)
	}
}

// Doomsday is no longer the default, so it needs explicit coverage of its own —
// otherwise flipping the default would have silently dropped the 9-scout path.
func TestRunAuditDoomsdayModeCLI(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "audit_cli_doomsday_*")
	if err != nil {
		t.Fatalf("mkdtemp: %v", err)
	}
	defer os.RemoveAll(tmpDir)
	_ = os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte("package main\nfunc main(){}"), 0644)
	_ = os.WriteFile(filepath.Join(tmpDir, "main_test.go"), []byte("package main\nimport \"testing\"\nfunc TestMain(t *testing.T){}"), 0644)

	var stdout, stderr bytes.Buffer
	if code := runAudit(&stdout, &stderr, []string{"--mode", "doomsday", tmpDir}); code != 0 {
		t.Fatalf("expected exit 0, got %d: %s", code, stderr.String())
	}
	if out := stdout.String(); !strings.Contains(out, "9 马仔末日代码审计报告") {
		t.Errorf("expected 9 马仔末日 header, got: %s", out)
	}

	stdout.Reset()
	stderr.Reset()
	if code := runAudit(&stdout, &stderr, []string{"--mode", "doomsday", "--json", tmpDir}); code != 0 {
		t.Fatalf("expected exit 0, got %d: %s", code, stderr.String())
	}
	if out := stdout.String(); !strings.Contains(out, `"total_scouts_deployed": 9`) {
		t.Errorf("expected total_scouts_deployed: 9, got: %s", out)
	}
}

func TestRunAuditLeanModeCLI(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "audit_cli_lean_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	_ = os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte("package main\nfunc main(){}"), 0644)
	_ = os.WriteFile(filepath.Join(tmpDir, "main_test.go"), []byte("package main\nimport \"testing\"\nfunc TestMain(t *testing.T){}"), 0644)

	var stdout, stderr bytes.Buffer
	code := runAudit(&stdout, &stderr, []string{"--mode", "lean", tmpDir})
	if code != 0 {
		t.Errorf("expected exit code 0, got %d. stderr: %s", code, stderr.String())
	}
	output := stdout.String()
	if !strings.Contains(output, "3 马仔精简代码审计报告") {
		t.Errorf("expected 3 马仔精简 header, got: %s", output)
	}
	if !strings.Contains(output, "1 + 1 + 1 = 3 位") {
		t.Errorf("expected 3 scouts deployed text, got: %s", output)
	}

	// Test JSON in lean mode
	stdout.Reset()
	stderr.Reset()
	code = runAudit(&stdout, &stderr, []string{"--mode", "lean", "--json", tmpDir})
	if code != 0 {
		t.Errorf("expected exit code 0 for json lean mode, got %d", code)
	}
	jsonOut := stdout.String()
	if !strings.Contains(jsonOut, `"total_scouts_deployed": 3`) {
		t.Errorf("expected json with total_scouts_deployed: 3, got: %s", jsonOut)
	}
	if !strings.Contains(jsonOut, `"mode": "lean"`) {
		t.Errorf("expected json with mode: lean, got: %s", jsonOut)
	}
}
