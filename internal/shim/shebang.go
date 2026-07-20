package shim

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// ShebangEnvIndirectInterpreter reports the interpreter name a
// "#!/usr/bin/env <name> ..." shebang line names, when scriptPath's first
// line is exactly that form. This is deliberately narrow -- it does not
// parse an absolute-path shebang ("#!/bin/sh"), which already names a
// concrete binary with no PATH/HOME-dependent lookup left to do -- because
// the only failure mode this function exists to catch (see Build's own doc
// comment on the asdf+virtualized-HOME class of bug) is specifically the
// "env indirection defers the real lookup to exec time" case: the OS's own
// shebang handling resolves <name> via the exec'd process's own PATH at
// exec time, which is exactly when Exec has already virtualized HOME out
// from under an asdf-managed <name>.
//
// ok is false (with no error) for a non-shebang file, an unreadable path,
// or any shebang line that isn't the "env <name>" form -- every one of
// those is "nothing more for this function to resolve," not a failure the
// caller should react to; Build falls back to today's existing behavior
// (exec RealBinaryPath as-is, relying on the OS's own shebang handling)
// exactly as it did before this function existed.
func ShebangEnvIndirectInterpreter(scriptPath string) (name string, ok bool) {
	f, err := os.Open(scriptPath)
	if err != nil {
		return "", false
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	if !scanner.Scan() {
		return "", false
	}
	line := scanner.Text()
	rest, isShebang := strings.CutPrefix(line, "#!")
	if !isShebang {
		return "", false
	}
	fields := strings.Fields(rest)
	if len(fields) < 2 {
		return "", false
	}
	if filepath.Base(fields[0]) != "env" {
		return "", false
	}
	return fields[1], true
}
