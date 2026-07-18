package auth

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// InvocationPlan is the exact native login command ADR 0003 rung 3 would
// run for one host. Building this plan is never, by itself, running
// anything — see Invoke and doc.go for the safety discipline around actually
// executing it.
type InvocationPlan struct {
	Host    string
	Command string
	Args    []string
}

// nativeLoginInvocation returns host's real, documented native login
// subcommand. Verified by reading real `codex --help` output ("login
// Manage login" under Commands:) and real `claude auth --help` output
// ("login [options]  Sign in to your Anthropic account" under Commands: of
// the `auth` subcommand) — read-only --help inspection only, never by
// running either login command, the same evidentiary standard
// internal/qualify's allowedInvokeArgs doc comment already documents
// ("verified against `codex --help` / `claude --help` output").
func nativeLoginInvocation(host string) (InvocationPlan, error) {
	switch host {
	case "codex":
		return InvocationPlan{Host: host, Command: "codex", Args: []string{"login"}}, nil
	case "claude-code":
		return InvocationPlan{Host: host, Command: "claude", Args: []string{"auth", "login"}}, nil
	default:
		return InvocationPlan{}, fmt.Errorf("auth: nativeLoginInvocation: host %q has no known native login command in this package", host)
	}
}

// invokeTimeout hard-bounds every invocation Invoke makes, so a hang can
// never be mistaken for a passing test — mirrors internal/qualify's
// invokeTimeout and internal/context's detectTimeout.
const invokeTimeout = 15 * time.Second

// InvocationResult is what one Invoke call observed.
type InvocationResult struct {
	Attempted  bool
	Skipped    bool
	SkipReason string
	Command    string
	Args       []string
	ExitCode   int
	Stdout     string
	Stderr     string
	Err        error
}

// Invoke runs plan.Command with plan.Args, resolved only against pathEnv (an
// explicit, PATH-shaped value the caller supplies) and env (the exact,
// explicit environment list the child process receives) — never
// exec.LookPath's or os/exec's ambient inheritance of the calling process's
// own PATH/environment. This mirrors internal/qualify.RunInvocation's and
// internal/context.DetectHost's shared "resolve and exec against exactly
// what the caller supplied, nothing implicit" discipline.
//
// Unlike internal/qualify.RunInvocation, this is NOT restricted to
// internal/qualify.allowedInvokeArgs' version/help-only allowlist —
// invoking a real login is this function's entire purpose for a future,
// human-qualified milestone. That is exactly why this PR's own tests never
// call Invoke with a real installed codex/claude binary on pathEnv: every
// test builds a small, hermetic fake binary of its own and points pathEnv
// at that fake binary's directory only (see fallback_test.go/invoke_test.go)
// — see doc.go for the full safety rationale.
func Invoke(ctx context.Context, plan InvocationPlan, pathEnv string, env []string) (InvocationResult, error) {
	if plan.Command == "" {
		return InvocationResult{}, fmt.Errorf("auth: Invoke: plan.Command is empty")
	}

	binaryPath, err := lookPathIn(plan.Command, pathEnv)
	if err != nil {
		return InvocationResult{
			Attempted:  true,
			Skipped:    true,
			SkipReason: fmt.Sprintf("host binary %q not found on the supplied PATH: %v", plan.Command, err),
			Command:    plan.Command,
			Args:       plan.Args,
		}, nil
	}

	runCtx, cancel := context.WithTimeout(ctx, invokeTimeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, binaryPath, plan.Args...)
	// A nil Cmd.Env inherits the calling process's entire environment — the
	// opposite of this function's explicit-environment discipline (see
	// internal/context.probeVersion's identical concern); an empty-but-
	// non-nil slice keeps a nil/empty env from silently becoming "inherit
	// everything."
	cmd.Env = append([]string{}, env...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()
	// cmd.ProcessState is nil when the process never actually started (an
	// exec format error, a permission error, the binary vanishing between
	// lookPathIn resolving it and Run attempting it, ...). (*os.ProcessState).
	// ExitCode() is already documented and implemented to be nil-receiver-safe
	// (returns -1 without dereferencing, os/exec_posix.go) -- this call was
	// never actually going to panic even before this explicit check existed
	// (confirmed empirically: a real exec-format-error run still returns a
	// clean -1, not a crash). The check stays anyway: relying on a reader
	// already knowing that specific stdlib nil-safety guarantee is fragile
	// documentation, and being explicit here costs nothing. -1 matches the
	// same "no real exit code" sentinel os/exec.ExitError.ExitCode() itself
	// documents for its own analogous "process was terminated by a signal"
	// case.
	exitCode := -1
	if cmd.ProcessState != nil {
		exitCode = cmd.ProcessState.ExitCode()
	}
	return InvocationResult{
		Attempted: true,
		Command:   plan.Command,
		Args:      plan.Args,
		ExitCode:  exitCode,
		Stdout:    stdout.String(),
		Stderr:    stderr.String(),
		Err:       runErr,
	}, nil
}

// lookPathIn resolves name to an executable path using pathEnv (a
// PATH-shaped, os.PathListSeparator-delimited list of directories) instead
// of the calling process's actual environment. Deliberately a small,
// separate copy of the identical helper in internal/context and
// internal/qualify rather than an import of either: both of those are
// entangled with package-specific concerns (Sandbox, HostDetection) this
// package has no reason to depend on for a ~15-line function — the same
// rationale internal/context/host.go's own lookPathIn doc comment already
// gives for not reusing internal/qualify's copy.
func lookPathIn(name, pathEnv string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("auth: lookPathIn: empty command name")
	}
	if filepath.IsAbs(name) || filepath.Base(name) != name {
		if isExecutableFile(name) {
			return name, nil
		}
		return "", fmt.Errorf("auth: lookPathIn: %s: not found", name)
	}
	for _, dir := range filepath.SplitList(pathEnv) {
		if dir == "" {
			continue
		}
		candidate := filepath.Join(dir, name)
		if isExecutableFile(candidate) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("auth: lookPathIn: %s: not found in supplied PATH", name)
}

// isExecutableFile checks the Unix executable permission bits, matching
// internal/qualify's and internal/context's own isExecutableFile. Does not
// detect an executable on Windows — an accepted, pre-existing scope limit
// this project's first implementation slice already carries elsewhere
// (macOS-first, docs/project/roadmap.md).
func isExecutableFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	return info.Mode()&0o111 != 0
}
