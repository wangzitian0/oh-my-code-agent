// Package perf measures the startup overhead OMCA ITSELF adds before a
// managed launch hands off to the real host binary (issue #15's own
// framing): detect worktree, detect host, observe native/repository
// sources, compile-or-reuse a bootstrap generation, and — separately —
// resolve an already-established "current" generation the way a PATH shim
// invocation does. It never measures the wrapped host binary's own startup
// time, and it never calls syscall.Exec; both are explicitly out of scope
// (see the "stopping short of syscall.Exec" safety boundary this PR's own
// issue text states, and internal/shim.ExecReplace's doc comment for why
// that call is this project's one, isolated exec boundary).
//
// # Two measurement modes, never conflated
//
//   - MeasureSynthetic exercises the exact same production code
//     (internal/context, internal/observe, internal/runtime, internal/shim)
//     against a hermetic, temp-directory fixture — no real ~/.codex or
//     ~/.claude is ever touched, and the one subprocess it spawns
//     (internal/context.DetectHost's `--version` probe) targets a fake,
//     --version-only script this package builds itself, never a real
//     codex/claude binary (PR-10's own testdata/fakehost-style pattern).
//     This is what `make perf`'s CI-safe, flake-free ceiling assertion
//     (TestPerf_Synthetic...) and the committed evidence file's
//     "synthetic-fixture" numbers both come from.
//   - MeasureRealEnvironment exercises the identical code against THIS
//     machine's actual, real ~/.codex and ~/.claude (if installed):
//     internal/context.DetectHost's `--version` probe runs against the real
//     installed binaries, and internal/observe.Observe walks the real
//     native home directories. Both are read-only by construction and
//     already exercised extensively by this codebase's own fixture-based
//     test suite (see internal/context/host_test.go and
//     internal/observe/observe_test.go's zero-write proofs) — using them
//     against real local machine state for this PR's evidence-gathering is
//     the "real-environment proof" the round-2 acceptance-criteria addendum
//     asks for, not a new risk. Every generation MeasureRealEnvironment
//     compiles lands under a caller-supplied, scoped temp directory — never
//     the real $XDG_STATE_HOME/omca — so repeated runs never accumulate or
//     depend on real managed state. MeasureRealEnvironment has no ceiling
//     assertion: a CI runner with neither host installed reports that
//     honestly (zero hosts measured) rather than failing or fabricating a
//     number, matching this project's "UNKNOWN is safer than a guessed
//     number" ethos (see internal/knowledge's knownUnknowns precedent).
//
// A synthetic-fixture number and a real-environment number are structurally
// different measurements (a controlled, fixed-size fixture vs. whatever
// this machine's actual installation happens to contain) and must never be
// merged into one figure — every caller of this package keeps them in
// separate, clearly labeled fields (Result.Synthetic vs. RealEnvironment
// Result.Hosts) for exactly this reason.
package perf
