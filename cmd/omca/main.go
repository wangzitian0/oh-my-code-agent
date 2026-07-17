// Command omca is the entry point for the oh-my-code-agent control plane.
package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/wangzitian0/oh-my-code-agent/internal/shim"
	"github.com/wangzitian0/oh-my-code-agent/internal/version"
)

// main dispatches into shim mode before anything else, based on the
// invoked program name (filepath.Base(os.Args[0])) — the "multi-call
// binary" design issue #14 recommends: the shim directory's codex/claude
// PATH entries are symlinks back to this same compiled omca binary
// (env.go's installShims), so when the OS resolves and execs one of them,
// os.Args[0] carries the symlink's own basename ("codex"/"claude"), not
// "omca". This is checked first, and unconditionally — before flag/argument
// parsing for the normal `omca <command>` surface — because a shim
// invocation's own arguments (`args[1:]`) belong entirely to the target
// host binary, not to omca's own CLI grammar; run() below must never see
// them.
//
// A successful shim launch calls syscall.Exec (internal/shim.ExecReplace)
// and never returns, so os.Exit below it is unreachable on that path; it
// only executes if shim dispatch itself failed before ever reaching exec
// (e.g. no managed generation for this host yet).
func main() {
	prog := filepath.Base(os.Args[0])
	if shim.IsShimInvocation(prog) {
		os.Exit(runShim(prog, os.Args[1:], os.Environ()))
	}
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

const usage = "usage: omca <version|context|env|run|doctor> ..."

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, usage)
		return 2
	}

	switch args[0] {
	case "version":
		fmt.Fprintln(stdout, version.String())
		return 0
	case "context":
		return runContext(stdout, stderr)
	case "env":
		return runEnv(stdout, stderr, args[1:])
	case "run":
		return runRun(stdout, stderr, args[1:])
	case "doctor":
		return runDoctor(stdout, stderr)
	default:
		fmt.Fprintf(stderr, "omca: unknown command %q\n%s\n", args[0], usage)
		return 2
	}
}

// runShim builds and executes internal/shim.Plan for one PATH-shim
// invocation (issue #14's non-recursive codex/claude entry points,
// docs/architecture/runtime.md §4). On success, Plan.Exec never returns —
// the calling process becomes the real host binary via syscall.Exec, so
// this int return value is only ever observed on the error path (an
// unrecognized invocation, no managed generation yet, or the exec syscall
// itself failing).
func runShim(invokedName string, args []string, environ []string) int {
	plan, err := shim.Build(invokedName, environ)
	if err != nil {
		fmt.Fprintf(os.Stderr, "omca: shim: %v\n", err)
		return 1
	}
	if err := plan.Exec(args, environ); err != nil {
		fmt.Fprintf(os.Stderr, "omca: shim: %v\n", err)
		return 1
	}
	return 0 // unreachable on success
}
