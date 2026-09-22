package runtime

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// WorktreeSandbox encapsulates an isolated execution environment.
// It combines a git worktree (to prevent dirtying or colliding with the primary workspace)
// and an isolated virtual HOME (to prevent leaking or polluting user credentials and global configuration).
// It also provides process-group lifecycle management (Setpgid) to prevent orphan/zombie processes.
type WorktreeSandbox struct {
	RepoRoot            string // Root directory of the main git repository
	WorktreeDir         string // Directory containing the isolated worktree checkout
	BranchName          string // Ephemeral branch associated with this worktree
	HomeDir             string // Isolated temporary HOME directory
	BaseDir             string // Base temporary directory created for this sandbox
	createdBranch       bool
	keepBranchOnCleanup bool
	autoCreatedBase     bool

	mu      sync.Mutex
	cleaned bool
}

// SandboxConfig specifies creation parameters for a WorktreeSandbox.
type SandboxConfig struct {
	// BaseDir is the directory in which the worktree and home directories will be placed.
	// If empty, an ephemeral directory under os.TempDir() is created and cleaned up.
	BaseDir string

	// BranchName specifies the git branch to create for the worktree.
	// If empty, an ephemeral branch "omca-sbx-<timestamp>-<rand>" is generated.
	BranchName string

	// BaseCommit specifies the revision to branch from (defaults to "HEAD").
	BaseCommit string

	// KeepBranchOnCleanup indicates whether to preserve the git branch upon cleanup.
	KeepBranchOnCleanup bool
}

// NewWorktreeSandbox creates a new isolated worktree and virtual home environment.
func NewWorktreeSandbox(ctx context.Context, repoRoot string, cfg SandboxConfig) (*WorktreeSandbox, error) {
	if repoRoot == "" {
		return nil, errors.New("runtime: repoRoot cannot be empty")
	}

	// Verify repoRoot is a git repository
	topLevelCmd := exec.CommandContext(ctx, "git", "-C", repoRoot, "rev-parse", "--show-toplevel")
	out, err := topLevelCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("runtime: not a git repository %q: %w", repoRoot, err)
	}
	canonicalRepoRoot := strings.TrimSpace(string(out))

	autoCreatedBase := false
	baseDir := cfg.BaseDir
	if baseDir == "" {
		b, err := os.MkdirTemp("", "omca-sandbox-*")
		if err != nil {
			return nil, fmt.Errorf("runtime: create sandbox temp dir: %w", err)
		}
		baseDir = b
		autoCreatedBase = true
	}

	worktreeDir := filepath.Join(baseDir, "worktree")
	homeDir := filepath.Join(baseDir, "home")

	if err := os.MkdirAll(homeDir, 0o700); err != nil {
		if autoCreatedBase {
			_ = os.RemoveAll(baseDir)
		}
		return nil, fmt.Errorf("runtime: create home dir: %w", err)
	}

	branchName := cfg.BranchName
	createdBranch := false
	if branchName == "" {
		randBytes := make([]byte, 4)
		if _, err := rand.Read(randBytes); err != nil {
			branchName = fmt.Sprintf("omca-sbx-%d", time.Now().UnixNano())
		} else {
			branchName = fmt.Sprintf("omca-sbx-%d-%s", time.Now().Unix(), hex.EncodeToString(randBytes))
		}
		createdBranch = true
	}

	baseCommit := cfg.BaseCommit
	if baseCommit == "" {
		baseCommit = "HEAD"
	}

	// Add git worktree
	var wtArgs []string
	if createdBranch {
		wtArgs = []string{"-C", canonicalRepoRoot, "worktree", "add", "-b", branchName, worktreeDir, baseCommit}
	} else {
		wtArgs = []string{"-C", canonicalRepoRoot, "worktree", "add", worktreeDir, branchName}
	}

	wtCmd := exec.CommandContext(ctx, "git", wtArgs...)
	if wtOut, err := wtCmd.CombinedOutput(); err != nil {
		// If adding worktree failed and branch wasn't auto-generated, try fallback with -b branchName
		if !createdBranch {
			fallbackCmd := exec.CommandContext(ctx, "git", "-C", canonicalRepoRoot, "worktree", "add", "-b", branchName, worktreeDir, baseCommit)
			if fbOut, fbErr := fallbackCmd.CombinedOutput(); fbErr != nil {
				if autoCreatedBase {
					_ = os.RemoveAll(baseDir)
				}
				return nil, fmt.Errorf("runtime: git worktree add failed: %w (output: %s)", fbErr, string(fbOut))
			}
			createdBranch = true
		} else {
			if autoCreatedBase {
				_ = os.RemoveAll(baseDir)
			}
			return nil, fmt.Errorf("runtime: git worktree add failed: %w (output: %s)", err, string(wtOut))
		}
	}

	sbx := &WorktreeSandbox{
		RepoRoot:            canonicalRepoRoot,
		WorktreeDir:         worktreeDir,
		BranchName:          branchName,
		HomeDir:             homeDir,
		BaseDir:             baseDir,
		createdBranch:       createdBranch,
		keepBranchOnCleanup: cfg.KeepBranchOnCleanup,
		autoCreatedBase:     autoCreatedBase,
	}

	return sbx, nil
}

// BuildEnv returns an environment slice where HOME is set to the sandbox's virtual home,
// and all host/repository Git environment variables are strictly filtered out.
func (s *WorktreeSandbox) BuildEnv(extraEnv map[string]string) []string {
	baseEnv := os.Environ()
	env := make([]string, 0, len(baseEnv)+len(extraEnv)+1)

	for _, e := range baseEnv {
		// Intercept HOME and all GIT_ environment variables to ensure zero leakage
		if strings.HasPrefix(e, "HOME=") || strings.HasPrefix(e, "GIT_") {
			continue
		}
		env = append(env, e)
	}

	env = append(env, "HOME="+s.HomeDir)
	for k, v := range extraEnv {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}
	return env
}

// Command prepares an *exec.Cmd configured for execution inside the sandbox.
// It sets WorkingDir to WorktreeDir, isolates HOME, configures Setpgid for process-group
// isolation, and wires up ctx cancellation to terminate the entire process group.
func (s *WorktreeSandbox) Command(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = s.WorktreeDir
	cmd.Env = s.BuildEnv(nil)
	setProcessGroup(cmd)

	// When ctx is cancelled or times out, kill the whole process group
	cmd.Cancel = func() error {
		killProcessGroup(cmd)
		return nil
	}
	cmd.WaitDelay = 2 * time.Second
	return cmd
}

// RunCommand executes a command inside the sandbox worktree with process-group cleanup on timeout.
func (s *WorktreeSandbox) RunCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := s.Command(ctx, name, args...)
	return cmd.CombinedOutput()
}

// Cleanup safely removes the worktree, the virtual home directory, and deletes the ephemeral branch.
// It is idempotent and safe to call multiple times, and uses a decoupled context so it does not fail
// if the caller's context timed out.
func (s *WorktreeSandbox) Cleanup(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.cleaned {
		return nil
	}
	s.cleaned = true

	// Decouple from potentially canceled/timed-out caller context, giving cleanup 5s budget
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()

	var errs []error

	// 1. Remove git worktree
	if s.WorktreeDir != "" && s.RepoRoot != "" {
		rmCmd := exec.CommandContext(cleanupCtx, "git", "-C", s.RepoRoot, "worktree", "remove", "--force", s.WorktreeDir)
		if rmOut, err := rmCmd.CombinedOutput(); err != nil {
			_ = rmOut
			// Fallback order: physical remove FIRST, then prune metadata
			_ = os.RemoveAll(s.WorktreeDir)
			_ = exec.CommandContext(cleanupCtx, "git", "-C", s.RepoRoot, "worktree", "prune").Run()
		}
	}

	// 2. Delete ephemeral branch if created and not configured to keep
	if s.createdBranch && !s.keepBranchOnCleanup && s.BranchName != "" && s.RepoRoot != "" {
		brCmd := exec.CommandContext(cleanupCtx, "git", "-C", s.RepoRoot, "branch", "-D", s.BranchName)
		_ = brCmd.Run()
	}

	// 3. Remove HomeDir or BaseDir
	if s.autoCreatedBase && s.BaseDir != "" {
		if err := os.RemoveAll(s.BaseDir); err != nil && !os.IsNotExist(err) {
			errs = append(errs, fmt.Errorf("runtime: remove sandbox base dir: %w", err))
		}
	} else if s.HomeDir != "" {
		if err := os.RemoveAll(s.HomeDir); err != nil && !os.IsNotExist(err) {
			errs = append(errs, fmt.Errorf("runtime: remove sandbox home dir: %w", err))
		}
	}

	return errors.Join(errs...)
}
