package main

import (
	"fmt"
	"io"

	hostcontext "github.com/wangzitian0/oh-my-code-agent/internal/context"
	"github.com/wangzitian0/oh-my-code-agent/internal/mcp"
	"github.com/wangzitian0/oh-my-code-agent/internal/runtime"
)

// sessionHostFromEnv determines which host actually launched this `omca mcp
// serve` process, for mcp.ComputeStatusRequest.SessionHost (issue #19's
// restart_required wiring) -- a documented judgment call, not a value this
// project passes explicitly anywhere today: internal/runtime.
// NativeHomeEnvVar names a distinct environment variable per host
// (CODEX_HOME, CLAUDE_CONFIG_DIR), and every managed launch path this
// project has (cmd/omca/run.go's runIsolated, internal/shim.Plan.Exec) sets
// exactly one of them, pointing into the generation directory that host was
// launched with, before exec'ing the host binary that in turn spawns this
// process as its own MCP server subprocess (docs/architecture/runtime.md
// §3/§7.1). Seeing one of these variables set is therefore a reliable-in-
// practice (if not schema-guaranteed) signal of which host this session
// belongs to; seeing none (or, defensively, both) means SessionHost stays
// empty and restart_required is left unreported rather than guessed.
func sessionHostFromEnv(env hostcontext.Environment) string {
	var found string
	for _, host := range hostcontext.DetectedHostIDs {
		envVar, err := runtime.NativeHomeEnvVar(host)
		if err != nil {
			continue
		}
		if env.Get(envVar) == "" {
			continue
		}
		if found != "" {
			// Both native-home variables are set (should not happen through
			// any managed launch path this project has) -- ambiguous,
			// report unknown rather than guessing.
			return ""
		}
		found = host
	}
	return found
}

// runMCP implements `omca mcp serve` (issue #15, docs/architecture/
// runtime.md §6's OMCA MCP server): starts the stdio JSON-RPC 2.0 server
// (internal/mcp.Serve) against stdin/stdout, answering omca_status from the
// CURRENT process's environment. This is the exact command
// internal/runtime/compile.go's hostConfigFiles registers as a managed
// generation's MCP server entry — a host that launches this managed session
// spawns `<omcaBinaryPath> mcp serve` as a subprocess, and that subprocess
// inherits the launching process's environment (the same OMCA_RUN_ID/
// OMCA_STATE_DIR/OMCA_WORKTREE_ID/OMCA_CONTEXT_ID docs/architecture/
// runtime.md §7.1 shows `omca run`/the PATH shim setting before exec'ing
// the host binary), so reading them here — exactly like checkSessionManaged/
// checkPathBypass in doctor.go already read managed-session state from the
// environment — is how this process learns which worktree/generation it is
// answering for, without any argument or config file of its own.
//
// stdin/stdout/stderr are accepted as explicit parameters (like every other
// runX function in this package) so the pre-Serve argument-validation path
// stays testable without a real subprocess; the stdio MCP loop itself
// (internal/mcp.Serve) is exercised directly against os.Stdin/os.Stdout only
// from main(), consistent with this project's "syscall.Exec-adjacent code
// gets a real subprocess test, decision logic gets an in-process one"
// precedent (cmd/omca/shim_test.go's doc comment).
func runMCP(stdin io.Reader, stdout, stderr io.Writer, args []string) int {
	if len(args) != 1 || args[0] != "serve" {
		fmt.Fprintln(stderr, "usage: omca mcp serve")
		return 2
	}

	env := hostcontext.RealEnvironment()
	sessionHost := sessionHostFromEnv(env)
	statusFn := func() (mcp.StatusResult, error) {
		return mcp.ComputeStatus(mcp.ComputeStatusRequest{
			WorktreeID:          env.Get("OMCA_WORKTREE_ID"),
			ContextID:           env.Get("OMCA_CONTEXT_ID"),
			WorktreeStateDir:    env.Get("OMCA_STATE_DIR"),
			Hosts:               hostcontext.DetectedHostIDs,
			SessionHost:         sessionHost,
			SessionGenerationID: env.Get("OMCA_RUN_ID"),
		})
	}

	if err := mcp.Serve(stdin, stdout, statusFn); err != nil {
		fmt.Fprintf(stderr, "omca: mcp: %v\n", err)
		return 1
	}
	return 0
}
