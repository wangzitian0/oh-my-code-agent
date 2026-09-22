package runtime_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wangzitian0/oh-my-code-agent/internal/runtime"
)

func initTestGitRepo(t *testing.T) string {
	t.Helper()
	repoDir := t.TempDir()

	runGit := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", repoDir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s failed: %v (output: %s)", strings.Join(args, " "), err, string(out))
		}
	}

	runGit("init")
	runGit("config", "user.email", "test@example.com")
	runGit("config", "user.name", "Test User")

	testFile := filepath.Join(repoDir, "README.md")
	if err := os.WriteFile(testFile, []byte("# Test Repo\n"), 0o644); err != nil {
		t.Fatalf("write file failed: %v", err)
	}

	runGit("add", "README.md")
	runGit("commit", "-m", "initial commit")

	return repoDir
}

func TestSandbox_CreationAndCleanup(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	repoRoot := initTestGitRepo(t)

	sbx, err := runtime.NewWorktreeSandbox(ctx, repoRoot, runtime.SandboxConfig{})
	if err != nil {
		t.Fatalf("NewWorktreeSandbox failed: %v", err)
	}

	// Verify worktree directory exists and contains checked out file
	readmePath := filepath.Join(sbx.WorktreeDir, "README.md")
	if _, err := os.Stat(readmePath); os.IsNotExist(err) {
		t.Errorf("expected README.md in worktree dir %q, but not found", sbx.WorktreeDir)
	}

	// Verify home directory exists
	if fi, err := os.Stat(sbx.HomeDir); os.IsNotExist(err) || !fi.IsDir() {
		t.Errorf("expected home dir %q to exist as directory", sbx.HomeDir)
	}

	// Run command inside sandbox
	out, err := sbx.RunCommand(ctx, "git", "status", "--porcelain")
	if err != nil {
		t.Fatalf("RunCommand(git status) failed: %v (output: %s)", err, string(out))
	}
	if len(strings.TrimSpace(string(out))) != 0 {
		t.Errorf("expected clean git status, got: %s", string(out))
	}

	// Verify branch was created in git repo
	checkBranchCmd := exec.CommandContext(ctx, "git", "-C", repoRoot, "rev-parse", "--verify", sbx.BranchName)
	if err := checkBranchCmd.Run(); err != nil {
		t.Errorf("expected branch %q to exist in repo, but rev-parse failed", sbx.BranchName)
	}

	branchName := sbx.BranchName
	worktreeDir := sbx.WorktreeDir
	homeDir := sbx.HomeDir

	// Clean up
	if err := sbx.Cleanup(ctx); err != nil {
		t.Fatalf("Cleanup failed: %v", err)
	}

	// Verify worktree is gone
	if _, err := os.Stat(worktreeDir); !os.IsNotExist(err) {
		t.Errorf("expected worktree dir %q to be removed after cleanup", worktreeDir)
	}

	// Verify home is gone
	if _, err := os.Stat(homeDir); !os.IsNotExist(err) {
		t.Errorf("expected home dir %q to be removed after cleanup", homeDir)
	}

	// Verify ephemeral branch was deleted
	if err := exec.CommandContext(ctx, "git", "-C", repoRoot, "rev-parse", "--verify", branchName).Run(); err == nil {
		t.Errorf("expected branch %q to be deleted after cleanup, but it still exists", branchName)
	}
}

func TestSandbox_VirtualHomeIsolation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	repoRoot := initTestGitRepo(t)
	sbx, err := runtime.NewWorktreeSandbox(ctx, repoRoot, runtime.SandboxConfig{})
	if err != nil {
		t.Fatalf("NewWorktreeSandbox failed: %v", err)
	}
	defer func() { _ = sbx.Cleanup(ctx) }()

	env := sbx.BuildEnv(map[string]string{
		"TEST_VAR": "sandbox_isolated",
	})

	hasHome := false
	hasTestVar := false
	for _, e := range env {
		if strings.HasPrefix(e, "HOME=") {
			hasHome = true
			if e != "HOME="+sbx.HomeDir {
				t.Errorf("HOME in env = %q, want %q", e, "HOME="+sbx.HomeDir)
			}
		}
		if e == "TEST_VAR=sandbox_isolated" {
			hasTestVar = true
		}
	}

	if !hasHome {
		t.Error("expected HOME to be present in BuildEnv")
	}
	if !hasTestVar {
		t.Error("expected TEST_VAR to be present in BuildEnv")
	}
}

func TestSandbox_ProcessGroupTimeout(t *testing.T) {
	repoRoot := initTestGitRepo(t)

	setupCtx, setupCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer setupCancel()

	sbx, err := runtime.NewWorktreeSandbox(setupCtx, repoRoot, runtime.SandboxConfig{})
	if err != nil {
		t.Fatalf("NewWorktreeSandbox failed: %v", err)
	}
	defer func() { _ = sbx.Cleanup(context.Background()) }()

	// Run a process that would run for 10 seconds, with 200ms context timeout
	runCtx, runCancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer runCancel()

	start := time.Now()
	_, err = sbx.RunCommand(runCtx, "sh", "-c", "sleep 10")
	duration := time.Since(start)

	if err == nil {
		t.Error("expected error from timed-out command, got nil")
	}
	if duration >= 5*time.Second {
		t.Errorf("command took %v, process group was not terminated promptly", duration)
	}
}

func TestSandbox_IdempotentCleanup(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	repoRoot := initTestGitRepo(t)
	sbx, err := runtime.NewWorktreeSandbox(ctx, repoRoot, runtime.SandboxConfig{})
	if err != nil {
		t.Fatalf("NewWorktreeSandbox failed: %v", err)
	}

	if err := sbx.Cleanup(ctx); err != nil {
		t.Errorf("first cleanup failed: %v", err)
	}
	if err := sbx.Cleanup(ctx); err != nil {
		t.Errorf("second cleanup failed: %v", err)
	}
}

func TestSandbox_KeepBranchOnCleanup(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	repoRoot := initTestGitRepo(t)
	sbx, err := runtime.NewWorktreeSandbox(ctx, repoRoot, runtime.SandboxConfig{
		KeepBranchOnCleanup: true,
	})
	if err != nil {
		t.Fatalf("NewWorktreeSandbox failed: %v", err)
	}

	branchName := sbx.BranchName
	if err := sbx.Cleanup(ctx); err != nil {
		t.Fatalf("Cleanup failed: %v", err)
	}

	// Ephemeral branch must STILL exist because KeepBranchOnCleanup=true
	if err := exec.CommandContext(ctx, "git", "-C", repoRoot, "rev-parse", "--verify", branchName).Run(); err != nil {
		t.Errorf("expected branch %q to be preserved, but rev-parse failed: %v", branchName, err)
	}
}

func TestSandbox_CleanupWithCanceledContext(t *testing.T) {
	setupCtx, setupCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer setupCancel()

	repoRoot := initTestGitRepo(t)
	sbx, err := runtime.NewWorktreeSandbox(setupCtx, repoRoot, runtime.SandboxConfig{})
	if err != nil {
		t.Fatalf("NewWorktreeSandbox failed: %v", err)
	}

	// Pass an already-canceled context to simulate cleanup after task deadline exceeded
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := sbx.Cleanup(canceledCtx); err != nil {
		t.Errorf("Cleanup with canceled context should succeed via decoupled context, got: %v", err)
	}

	if _, err := os.Stat(sbx.WorktreeDir); !os.IsNotExist(err) {
		t.Errorf("expected worktreeDir %q to be removed, but still exists", sbx.WorktreeDir)
	}
}

func TestSandbox_StrictGitEnvFiltering(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	repoRoot := initTestGitRepo(t)
	sbx, err := runtime.NewWorktreeSandbox(ctx, repoRoot, runtime.SandboxConfig{})
	if err != nil {
		t.Fatalf("NewWorktreeSandbox failed: %v", err)
	}
	defer func() { _ = sbx.Cleanup(ctx) }()

	t.Setenv("GIT_INDEX_FILE", "/tmp/fake_index")
	t.Setenv("GIT_AUTHOR_EMAIL", "leak@example.com")
	t.Setenv("GIT_DIR", "/tmp/fake_git")

	env := sbx.BuildEnv(nil)
	for _, e := range env {
		if strings.HasPrefix(e, "GIT_") {
			t.Errorf("BuildEnv leaked Git env var: %q", e)
		}
	}
}
