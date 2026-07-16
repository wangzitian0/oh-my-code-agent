package qualify

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewSandboxCodex(t *testing.T) {
	root := t.TempDir()
	sb, err := NewSandbox(root, "codex")
	if err != nil {
		t.Fatalf("NewSandbox: %v", err)
	}
	for _, dir := range []string{sb.Home, sb.Project, sb.Outside, sb.CodexHome} {
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			t.Errorf("expected directory %s to exist, stat err = %v", dir, err)
		}
	}
	if sb.ClaudeConfigDir != "" {
		t.Errorf("codex sandbox should not set ClaudeConfigDir, got %q", sb.ClaudeConfigDir)
	}
}

func TestNewSandboxClaudeCode(t *testing.T) {
	root := t.TempDir()
	sb, err := NewSandbox(root, "claude-code")
	if err != nil {
		t.Fatalf("NewSandbox: %v", err)
	}
	if sb.CodexHome != "" {
		t.Errorf("claude-code sandbox should not set CodexHome, got %q", sb.CodexHome)
	}
	if info, err := os.Stat(sb.ClaudeConfigDir); err != nil || !info.IsDir() {
		t.Errorf("expected ClaudeConfigDir %s to exist, stat err = %v", sb.ClaudeConfigDir, err)
	}
}

func TestNewSandboxUnsupportedHost(t *testing.T) {
	if _, err := NewSandbox(t.TempDir(), "opencode"); err == nil {
		t.Error("NewSandbox(unsupported host) error = nil, want error")
	}
}

func TestSandboxEnvNeverInheritsAmbient(t *testing.T) {
	t.Setenv("QUALIFY_TEST_AMBIENT_SECRET", "leak-me-not")
	sb, err := NewSandbox(t.TempDir(), "codex")
	if err != nil {
		t.Fatal(err)
	}
	env := sb.Env("/usr/bin:/bin")
	for _, kv := range env {
		if kv == "QUALIFY_TEST_AMBIENT_SECRET=leak-me-not" {
			t.Fatal("Sandbox.Env leaked an ambient environment variable it must never inherit")
		}
	}
	wantHome := "HOME=" + sb.Home
	wantCodexHome := "CODEX_HOME=" + sb.CodexHome
	foundHome, foundCodexHome := false, false
	for _, kv := range env {
		if kv == wantHome {
			foundHome = true
		}
		if kv == wantCodexHome {
			foundCodexHome = true
		}
	}
	if !foundHome || !foundCodexHome {
		t.Errorf("Sandbox.Env(codex) = %v, want HOME and CODEX_HOME redirected into the sandbox", env)
	}
}

func TestPopulateFromInput(t *testing.T) {
	inputDir := t.TempDir()
	if err := writeFile(filepath.Join(inputDir, "home", "AGENTS.md"), "user instructions", 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(filepath.Join(inputDir, "project", "AGENTS.md"), "project instructions", 0o644); err != nil {
		t.Fatal(err)
	}

	sb, err := NewSandbox(t.TempDir(), "codex")
	if err != nil {
		t.Fatal(err)
	}
	if err := sb.PopulateFromInput(inputDir); err != nil {
		t.Fatalf("PopulateFromInput: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(sb.Home, "AGENTS.md"))
	if err != nil || string(got) != "user instructions" {
		t.Errorf("home/AGENTS.md = %q, %v, want %q", got, err, "user instructions")
	}
	got, err = os.ReadFile(filepath.Join(sb.Project, "AGENTS.md"))
	if err != nil || string(got) != "project instructions" {
		t.Errorf("project/AGENTS.md = %q, %v, want %q", got, err, "project instructions")
	}
}

func TestPopulateFromInputMissingSubdirIsNotError(t *testing.T) {
	sb, err := NewSandbox(t.TempDir(), "codex")
	if err != nil {
		t.Fatal(err)
	}
	if err := sb.PopulateFromInput(t.TempDir()); err != nil {
		t.Errorf("PopulateFromInput(empty input dir) error = %v, want nil", err)
	}
}

func TestPlantOutsideCanaryNotExecuted(t *testing.T) {
	sb, err := NewSandbox(t.TempDir(), "codex")
	if err != nil {
		t.Fatal(err)
	}
	marker, err := sb.PlantOutsideCanary()
	if err != nil {
		t.Fatalf("PlantOutsideCanary: %v", err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Errorf("canary marker should not exist before execution, stat err = %v", err)
	}
	canaryScript := filepath.Join(sb.Outside, "bin", "canary.sh")
	info, err := os.Stat(canaryScript)
	if err != nil {
		t.Fatalf("expected canary script to exist: %v", err)
	}
	if info.Mode()&0o111 == 0 {
		t.Errorf("canary script mode = %v, want executable", info.Mode())
	}
}

func TestCopyTreePreservesExecutableMode(t *testing.T) {
	src := t.TempDir()
	scriptPath := filepath.Join(src, "bin", "run.sh")
	if err := writeFile(scriptPath, "#!/bin/sh\necho hi\n", 0o755); err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(t.TempDir(), "dst")
	if err := copyTree(src, dst); err != nil {
		t.Fatalf("copyTree: %v", err)
	}

	info, err := os.Stat(filepath.Join(dst, "bin", "run.sh"))
	if err != nil {
		t.Fatalf("copied file missing: %v", err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Errorf("copied file mode = %v, want 0755", info.Mode().Perm())
	}
}
