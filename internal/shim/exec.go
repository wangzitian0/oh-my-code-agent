package shim

import (
	"fmt"
	"syscall"
)

// ExecReplace replaces the calling process's image with binaryPath, argv,
// and envp via syscall.Exec — this project's one production call to it (see
// doc.go's "exec, not fork+wait" section for why issue #14 requires this
// specifically, over os/exec.Cmd.Run). On success it never returns: the
// process that called it IS binaryPath from this point on, so the OS
// delivers every signal directly to it and its own exit code becomes this
// process's exit code, with no forwarding code required anywhere in this
// project. A non-nil return means the exec syscall itself failed (binary
// missing, not executable, argv/envp malformed, ...); the caller is still
// running and must handle it like any other error.
//
// This is macOS-first scope (matching internal/context's and
// internal/qualify's existing stance, and this PR's own instructions):
// syscall.Exec exists on every unix GOOS this project's CI touches
// (macos-latest for build/test, ubuntu-latest for lint) but not on
// windows — no other package in this module guards Windows either, so this
// one does not gold-plate a fallback for a platform nothing else here
// supports.
func ExecReplace(binaryPath string, argv, envp []string) error {
	if binaryPath == "" {
		return fmt.Errorf("shim: ExecReplace: empty binaryPath")
	}
	if len(argv) == 0 {
		return fmt.Errorf("shim: ExecReplace: empty argv (argv[0] is required, conventionally binaryPath itself)")
	}
	err := syscall.Exec(binaryPath, argv, envp) //nolint:gosec // deliberate: this package's entire purpose is to exec a caller-resolved binary
	return fmt.Errorf("shim: ExecReplace: exec %s: %w", binaryPath, err)
}

// InjectEnv returns a copy of environ with every existing entry whose key
// appears in overrides removed, followed by one "KEY=VALUE" entry per
// overrides in a stable (sorted-by-key) order. Removing the old entry
// first — rather than merely appending the override and relying on
// "last occurrence wins" — matters because not every consumer of an
// environment slice honors that convention the same way
// internal/context.Environment.Get does; producing a clean, single-occurrence
// slice is the only representation that behaves identically everywhere.
func InjectEnv(environ []string, overrides map[string]string) []string {
	keys := make([]string, 0, len(overrides))
	for k := range overrides {
		keys = append(keys, k)
	}
	sortStrings(keys)

	out := make([]string, 0, len(environ)+len(overrides))
	for _, kv := range environ {
		if !hasOverrideKey(kv, overrides) {
			out = append(out, kv)
		}
	}
	for _, k := range keys {
		out = append(out, k+"="+overrides[k])
	}
	return out
}

func hasOverrideKey(kv string, overrides map[string]string) bool {
	for k := range overrides {
		if len(kv) > len(k) && kv[:len(k)+1] == k+"=" {
			return true
		}
	}
	return false
}

// sortStrings is a tiny dependency-free insertion sort, matching
// internal/qualify/invoke.go's sortedKeys precedent for the same "small,
// fixed set, not worth importing sort for" judgment call.
func sortStrings(keys []string) {
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j-1] > keys[j]; j-- {
			keys[j-1], keys[j] = keys[j], keys[j-1]
		}
	}
}

// Exec builds the injected environment for p (p.NativeHomeEnvVar ->
// p.NativeHomeDir, plus OMCA_RUN_ID -> p.GenerationID when known) and calls
// ExecReplace against p.RealBinaryPath with args appended to argv. Like
// ExecReplace, this never returns on success.
func (p Plan) Exec(args []string, environ []string) error {
	overrides := map[string]string{p.NativeHomeEnvVar: p.NativeHomeDir}
	if p.GenerationID != "" {
		overrides["OMCA_RUN_ID"] = p.GenerationID
	}
	envp := InjectEnv(environ, overrides)
	argv := append([]string{p.RealBinaryPath}, args...)
	return ExecReplace(p.RealBinaryPath, argv, envp)
}
