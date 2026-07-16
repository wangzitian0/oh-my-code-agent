// Package conformance is the reusable conformance suite for the
// internal/plugin HostAdapter contract (docs/architecture/README.md §9;
// PR-05 issue #9). Run exercises every contract method against an arbitrary
// HostAdapter, including the sandboxed zero-write/zero-exec proof for
// Observe. FakeAdapter is a well-behaved, in-memory reference
// implementation: it passes Run and doubles as the illustration of what a
// conformant adapter's error-taxonomy and side-effect-free Observe look
// like.
//
// Real adapters (Codex, Claude Code — PR-07/08 onward) and PR-06's fixture
// harness are expected to run their own HostAdapter through
// conformance.Run(t, adapter) as part of their own test suite.
package conformance
