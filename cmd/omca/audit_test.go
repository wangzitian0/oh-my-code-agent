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
	if !strings.Contains(output, "9 马仔末日代码审计报告") {
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
	if !strings.Contains(jsonOut, `"total_scouts_deployed": 9`) {
		t.Errorf("expected json with total_scouts_deployed: 9, got: %s", jsonOut)
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
