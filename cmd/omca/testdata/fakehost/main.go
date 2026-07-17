// Command fakehost is a test-only fixture binary standing in for a real
// codex/claude installation. It is never invoked by anything except
// cmd/omca's own shim/run integration tests, and it is built explicitly by
// those tests (`go build ./testdata/fakehost`) — this directory is named
// testdata precisely so `go build ./...`/`go vet ./...`/`golangci-lint run
// ./...` at the repository root skip it (the Go toolchain's own
// documented convention), keeping this fixture out of every real build.
//
// Invoked as `fakehost --version`, it prints a single loose-pattern
// "MAJOR.MINOR.PATCH" version line instead (so internal/context.DetectHost's
// probeVersion, which real `omca run`/`omca env` invocations call before
// ever reaching the shim/exec path, succeeds against this fixture exactly
// like it would against a real host binary). Any other invocation dumps its
// full environment to stdout, one "KEY=VALUE" line per variable — the
// literal "fake host binary that dumps its env" issue #14's acceptance
// criteria call for, letting a test assert the shim/`omca run` actually
// injected CODEX_HOME/CLAUDE_CONFIG_DIR pointing into a real compiled
// generation directory. If FAKEHOST_MARKER names a file, that same
// non-version invocation also appends one line to it, giving a
// non-recursion test concrete, countable evidence ("invoked exactly once")
// beyond "the process didn't hang."
package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		fmt.Println("9.9.9")
		return
	}
	if marker := os.Getenv("FAKEHOST_MARKER"); marker != "" {
		f, err := os.OpenFile(marker, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err == nil {
			fmt.Fprintln(f, "invoked")
			_ = f.Close()
		}
	}
	for _, kv := range os.Environ() {
		fmt.Println(kv)
	}
}
