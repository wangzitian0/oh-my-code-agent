package main

import (
	"fmt"
	"io"

	hostcontext "github.com/wangzitian0/oh-my-code-agent/internal/context"
	"github.com/wangzitian0/oh-my-code-agent/internal/mcp"
)

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
	statusFn := func() (mcp.StatusResult, error) {
		return mcp.ComputeStatus(mcp.ComputeStatusRequest{
			WorktreeID:       env.Get("OMCA_WORKTREE_ID"),
			ContextID:        env.Get("OMCA_CONTEXT_ID"),
			WorktreeStateDir: env.Get("OMCA_STATE_DIR"),
			Hosts:            hostcontext.DetectedHostIDs,
		})
	}

	if err := mcp.Serve(stdin, stdout, statusFn); err != nil {
		fmt.Fprintf(stderr, "omca: mcp: %v\n", err)
		return 1
	}
	return 0
}
