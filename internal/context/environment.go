package context

import (
	"os"
	"strings"
)

// Environment is the ambient process input worktree, host, and native-home
// detection read. It is always constructed explicitly — RealEnvironment for
// production, a literal Environment{Vars: []string{...}} in tests — and
// never read implicitly deep inside detection logic, so detection stays
// hermetically testable without depending on the real machine's environment
// or on real host binaries being installed (docs/architecture/runtime.md:
// "Observe native configuration, but do not inherit it implicitly" applies
// to OMCA's own detection code, not only to the hosts it detects).
type Environment struct {
	// Vars is the exact process environment ("KEY=VALUE" entries). It is
	// used both to resolve individual values (PATH, HOME, CODEX_HOME,
	// CLAUDE_CONFIG_DIR, via Get) and, completely unmodified, as the
	// environment for any host binary invocation this package makes: a
	// --version probe sees exactly what detection itself observed, never a
	// silently different or widened environment built by appending onto
	// it.
	Vars []string
}

// RealEnvironment snapshots the real process environment once, at the call
// site. This is the only place in this package that reads os.Environ();
// every detection function below takes an Environment value explicitly
// instead.
func RealEnvironment() Environment {
	return Environment{Vars: os.Environ()}
}

// Get returns the value of the last Vars entry named key ("KEY=VALUE"
// parsing; last-occurrence-wins, matching os/exec.Cmd.Env and typical shell
// semantics for a repeated variable), or "" if key is not present.
func (e Environment) Get(key string) string {
	prefix := key + "="
	value := ""
	for _, kv := range e.Vars {
		if rest, ok := strings.CutPrefix(kv, prefix); ok {
			value = rest
		}
	}
	return value
}
