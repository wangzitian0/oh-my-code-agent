package qualify

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// allowedInvokeArgs is the closed set of arguments this harness will ever
// pass to a real host binary, enforced in code rather than trusted from a
// fixture's own invocation.yaml. This is the structural half of the safety
// boundary: even a future fixture author who mistakenly (or maliciously)
// wrote an interactive or model-invoking flag into invocation.yaml cannot
// make RunInvocation execute it. Every value here is a flag that both
// codex(1) and claude(1) document as printing information and exiting,
// never starting a session, reaching a model, or touching the network
// (verified against `codex --help` / `claude --help` output, see
// fixtures/README.md).
var allowedInvokeArgs = map[string]bool{
	"--version": true,
	"-v":        true,
	"--help":    true,
	"-h":        true,
}

// invokeTimeout hard-bounds every real host invocation regardless of what a
// caller's context allows, so a hang can never be mistaken for a passing
// test.
const invokeTimeout = 15 * time.Second

// InvocationResult is what one RunInvocation call observed.
type InvocationResult struct {
	Attempted  bool
	Skipped    bool
	SkipReason string
	Command    string
	Args       []string
	Stdout     string
	Stderr     string
	ExitCode   int
	Err        error
}

// RunInvocation runs the real host binary named by manifest.Invoke.Command
// with manifest.Invoke.Args, entirely inside sandbox: HOME (and CODEX_HOME /
// CLAUDE_CONFIG_DIR, whichever the sandbox carries) point only at sandbox
// paths, and the environment list is built from scratch (Sandbox.Env), never
// inherited from the calling process. pathEnv is the real PATH value needed
// to resolve the binary and its own runtime/loader dependencies; it is the
// only ambient value ever forwarded, and deliberately supplied by the
// caller rather than read implicitly.
//
// If manifest.Invoke.Attempted is false, RunInvocation does nothing and
// returns a Skipped result carrying manifest.Invoke.Reason — this is the
// expected path whenever a case's precedence question has no safe
// non-interactive way to be probed (see doc.go).
//
// Every argument is checked against allowedInvokeArgs before exec; an
// argument outside that closed set is refused rather than run.
func RunInvocation(ctx context.Context, sb *Sandbox, manifest InvocationManifest, pathEnv string) (InvocationResult, error) {
	if !manifest.Invoke.Attempted {
		return InvocationResult{
			Attempted:  false,
			Skipped:    true,
			SkipReason: manifest.Invoke.Reason,
		}, nil
	}

	for _, arg := range manifest.Invoke.Args {
		if !allowedInvokeArgs[arg] {
			return InvocationResult{}, fmt.Errorf(
				"qualify: RunInvocation: argument %q is not in the closed safe-invocation allowlist %v; refusing to run %q",
				arg, sortedKeys(allowedInvokeArgs), manifest.Invoke.Command,
			)
		}
	}

	// Deliberately not exec.LookPath: it resolves against the calling
	// process's own real PATH, which is exactly the ambient value this
	// function must not depend on. lookPathIn resolves against pathEnv,
	// the one explicit, caller-supplied PATH value this invocation uses.
	binaryPath, err := lookPathIn(manifest.Invoke.Command, pathEnv)
	if err != nil {
		return InvocationResult{
			Attempted:  true,
			Skipped:    true,
			SkipReason: fmt.Sprintf("host binary %q not found on the supplied PATH: %v", manifest.Invoke.Command, err),
			Command:    manifest.Invoke.Command,
			Args:       manifest.Invoke.Args,
		}, nil
	}

	runCtx, cancel := context.WithTimeout(ctx, invokeTimeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, binaryPath, manifest.Invoke.Args...)
	cmd.Env = sb.Env(pathEnv)
	cmd.Dir = sb.Home

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()

	result := InvocationResult{
		Attempted: true,
		Command:   manifest.Invoke.Command,
		Args:      manifest.Invoke.Args,
		Stdout:    stdout.String(),
		Stderr:    stderr.String(),
		ExitCode:  cmd.ProcessState.ExitCode(),
		Err:       runErr,
	}
	return result, nil
}

// lookPathIn resolves name to an executable path using pathEnv (a
// PATH-shaped, os.PathListSeparator-delimited list of directories) instead
// of the calling process's actual environment. This is the qualify package's
// own PATH lookup, deliberately separate from exec.LookPath/os.LookPath,
// because those both consult the real process environment's PATH — the one
// ambient value this package must never depend on when resolving which
// binary a sandboxed invocation runs.
func lookPathIn(name, pathEnv string) (string, error) {
	if name == "" {
		return "", errors.New("empty command name")
	}
	if filepath.IsAbs(name) || filepath.Base(name) != name {
		if isExecutableFile(name) {
			return name, nil
		}
		return "", fmt.Errorf("%s: not found", name)
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
	return "", fmt.Errorf("%s: not found in supplied PATH", name)
}

func isExecutableFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	return info.Mode()&0o111 != 0
}

func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// Small, fixed set; simple insertion sort keeps this dependency-free.
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j-1] > keys[j]; j-- {
			keys[j-1], keys[j] = keys[j], keys[j-1]
		}
	}
	return keys
}
