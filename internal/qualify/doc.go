// Package qualify is the executable qualification lab (PR-06, issue #10):
// it turns a fixture case directory (docs/knowledge/README.md §3) into a
// reproducible proof of what a host adapter would observe, plus an honest
// record of what precedence questions remain unproven.
//
// A fixture case never touches a real, developer-owned host installation's
// configuration. Every invocation this package makes of a real host binary
// (codex, claude) is restricted to a version-only, non-interactive,
// no-network, no-model-call probe with HOME and every host-specific home
// variable (CODEX_HOME, CLAUDE_CONFIG_DIR) redirected into a fresh temporary
// directory tree created for that one case. Before and after every such
// invocation, the harness snapshots a realistic stand-in "outside world"
// directory (disjoint from the sandbox handed to the subprocess) and asserts
// it is byte-for-byte unchanged — the automated, deterministic half of the
// zero-write proof. A second, opt-in check (env-gated, see realhome.go)
// additionally snapshots the actual real host config paths named in
// docs/architecture/runtime.md §7.1/§7.2; it is not part of the default
// `go test ./...`/`make fixtures` path because a live developer machine with
// real Codex/Claude Code sessions running has legitimate, ambient background
// writes to those same paths (observed and documented in the PR-06 PR body)
// that are unrelated to this harness and would make an unconditional
// assertion flaky.
//
// Where a case's precedence question cannot be established this way — no
// safe non-interactive introspection path exists to dump a host's effective,
// merged configuration without starting a real session — the case's
// expected-effective.json records UNKNOWN rather than a guessed winner
// (docs/ontology/README.md: "UNKNOWN is safer than a guessed adapter").
//
// This package is deliberately importable outside its own tests: PR-08's
// real observation code is expected to reuse Sandbox, snapshot/diff, and the
// Observation-building helpers rather than re-deriving them.
package qualify
