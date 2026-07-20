package shim

import (
	"os"
	"path/filepath"
	"testing"
)

func writeScript(t *testing.T, dir, name, firstLine string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	body := firstLine
	if body != "" {
		body += "\n"
	}
	body += "echo hi\n"
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("writeScript: %v", err)
	}
	return path
}

// TestShebangEnvIndirectInterpreter_EnvForm proves the one case this
// function exists to catch: a real "#!/usr/bin/env node" line, exactly the
// shape codex's own asdf-installed binary uses, reports the interpreter
// name.
func TestShebangEnvIndirectInterpreter_EnvForm(t *testing.T) {
	path := writeScript(t, t.TempDir(), "codex", "#!/usr/bin/env node")
	name, ok := ShebangEnvIndirectInterpreter(path)
	if !ok || name != "node" {
		t.Errorf("ShebangEnvIndirectInterpreter(%q) = (%q, %v), want (\"node\", true)", path, name, ok)
	}
}

// TestShebangEnvIndirectInterpreter_EnvFormWithExtraArgs proves a shebang
// line naming flags after the interpreter (e.g. "env -S node --flag") is
// not itself in scope -- this function only ever needs the bare "env
// <name>" shape actually observed on this project's fixture machine, and a
// two-word "env <name>" match already stops at the interpreter name
// (fields[1]), so extra trailing words are simply ignored, not misparsed.
func TestShebangEnvIndirectInterpreter_EnvFormWithExtraArgs(t *testing.T) {
	path := writeScript(t, t.TempDir(), "codex", "#!/usr/bin/env node --harmony")
	name, ok := ShebangEnvIndirectInterpreter(path)
	if !ok || name != "node" {
		t.Errorf("ShebangEnvIndirectInterpreter(%q) = (%q, %v), want (\"node\", true)", path, name, ok)
	}
}

// TestShebangEnvIndirectInterpreter_AbsolutePath_NotEnvIndirect proves an
// absolute-path shebang ("#!/bin/sh") is correctly reported as "nothing to
// resolve" -- it already names a concrete binary with no PATH/HOME lookup
// deferred to exec time, the one case this function must not misidentify
// as needing the same treatment as an "env <name>" indirection.
func TestShebangEnvIndirectInterpreter_AbsolutePath_NotEnvIndirect(t *testing.T) {
	path := writeScript(t, t.TempDir(), "myscript", "#!/bin/sh")
	if _, ok := ShebangEnvIndirectInterpreter(path); ok {
		t.Errorf("ShebangEnvIndirectInterpreter(%q) = ok=true, want false (absolute-path shebang, nothing to resolve)", path)
	}
}

// TestShebangEnvIndirectInterpreter_NoShebang_OrdinaryBinary proves the
// overwhelmingly common case -- a real ELF/Mach-O binary with no shebang
// line at all -- is a clean false, not a false positive or a panic.
func TestShebangEnvIndirectInterpreter_NoShebang_OrdinaryBinary(t *testing.T) {
	dir := t.TempDir()
	path := writeFakeExecutable(t, dir, "codex") // "#!/bin/sh\nexit 0\n" per resolve_test.go
	// writeFakeExecutable itself writes a shebang script, not a binary --
	// still exercises the "absolute-path shebang" non-match path, already
	// covered above; this test instead writes genuinely non-shebang content
	// to prove that shape too.
	if err := os.WriteFile(path, []byte("not a script at all, just bytes\x00\x01\x02"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, ok := ShebangEnvIndirectInterpreter(path); ok {
		t.Errorf("ShebangEnvIndirectInterpreter(%q) = ok=true, want false (no shebang line)", path)
	}
}

// TestShebangEnvIndirectInterpreter_MissingFile proves an unreadable path
// (does not exist) is a clean false, not an error the caller must handle --
// Build's own fallback ("nothing more to resolve, proceed as before")
// depends on this.
func TestShebangEnvIndirectInterpreter_MissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist")
	if _, ok := ShebangEnvIndirectInterpreter(path); ok {
		t.Errorf("ShebangEnvIndirectInterpreter(%q) = ok=true, want false (unreadable path)", path)
	}
}

// TestShebangEnvIndirectInterpreter_BareEnvNoName proves a malformed
// "#!/usr/bin/env" line with no interpreter name at all is a clean false,
// not a panic or an empty-string interpreter name silently accepted.
func TestShebangEnvIndirectInterpreter_BareEnvNoName(t *testing.T) {
	path := writeScript(t, t.TempDir(), "codex", "#!/usr/bin/env")
	if name, ok := ShebangEnvIndirectInterpreter(path); ok {
		t.Errorf("ShebangEnvIndirectInterpreter(%q) = (%q, true), want ok=false (no interpreter name)", path, name)
	}
}
