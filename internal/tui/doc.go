// Package tui is the root-cause-first management TUI's pure rendering
// layer (issue #34, docs/architecture/reporting.md §9's "Human Information
// Architecture"): the Overview, Drift, Assets, and Generations views, each
// projected from the exact same immutable [report.Artifact] the `omca`
// CLI's own report/drift/explain/matrix commands already render (cmd/omca/
// report.go and siblings) — never a second, independently computed view of
// the world.
//
// # Library choice
//
// This package is built on github.com/charmbracelet/bubbletea (the event
// loop/Model) and github.com/charmbracelet/lipgloss (layout), the standard,
// actively maintained modern Go TUI stack, chosen deliberately for a reason
// specific to this issue's own round-3 review requirement ("every TUI view
// is snapshot-tested against a committed report artifact"): bubbletea's
// tea.Model.View() method is a pure function from model state to a
// rendered string. A view can therefore be snapshot-tested by constructing
// a [Model] from a fixture report.Artifact, calling View(), and comparing
// the resulting string against a committed golden file — no real
// terminal, no interactive event loop, no flaky ANSI-escape-sequence
// timing in CI. See fixture_test.go/golden_test.go for the harness this
// enables, and overview_test.go/drift_test.go/assets_test.go/
// generations_test.go for the four views' own golden tests.
//
// Every exported Render* function in this package (RenderOverview,
// RenderDrift, RenderAssets, RenderGenerations) deliberately renders plain
// text: no ANSI color/attribute codes (no Foreground/Background/Bold/
// Underline — see tabbar.go's doc comment for why). This keeps golden
// files stable across every environment (CI, a real terminal, a piped
// `go test` run) regardless of terminal color-profile auto-detection,
// without needing to force lipgloss's global renderer at test time. Only
// the tab-bar navigation chrome (tabbar.go) uses lipgloss's layout
// primitives (Border, Padding, JoinHorizontal) — profile-independent,
// since no color is ever set on them either.
//
// # Scope
//
// PR-30 (issue #34) built the FOUNDATION layer: read-only rendering of the
// four named views, plus basic navigation between them. PR-31 (issue #35,
// this PR) adds the action layer on top, without changing that read-only
// rendering contract: Model gained an optional ActionContext (see
// actions.go) that, once attached (WithActionContext), unlocks 'a'
// (activate an AVAILABLE asset), 'y'/'n' (approve/cancel a reviewed Change
// Set), and 'r' (roll back) — every one of PR-30's own tests, and every
// caller that never attaches an ActionContext, keeps behaving exactly as
// before (ActionContext.enabled()'s doc comment). Issue #36 ("TUI debug
// views": precedence trace/evidence) remains separate, later scope.
//
// # Action layer (issue #35)
//
// activate/rollback/confirmation are NOT reimplemented here: this package
// calls the exact same internal/runtime functions cmd/omca's own
// `omca activate`/`omca rollback` commands call (runtime.DiffProposedChanges,
// runtime.ClassifyChange, runtime.RequireConfirmation, runtime.
// ActivateAndVerify, runtime.Rollback — see actions.go's doc comments for
// exactly where each one is used). The one thing this package genuinely
// duplicates is a handful of SMALL, cmd/omca-private composition helpers
// (compositionDirsFor, composeFreshCompileRequest, compileFuncForMCP's
// staging sequence, buildArtifactForCLI) that cmd/omca/activate.go, cmd/
// omca/mcp.go, and cmd/omca/reportbuild.go keep unexported — internal/tui
// cannot import cmd/omca at all (it is `package main`), and cmd/omca
// already imports internal/tui for its Model, so the dependency cannot run
// the other way for this PR either. Each mirrored function in actions.go
// carries its own doc comment naming its cmd/omca counterpart explicitly
// and states the sequence is intentionally kept IDENTICAL (same
// internal/profiles, internal/context, internal/observe, internal/runtime
// calls, same order) — a reviewed, visible trade-off (see this PR's own
// description) rather than a silent, driftable duplication. A future PR is
// free to factor these into one shared package both cmd/omca and
// internal/tui import; that refactor is out of scope here.
//
// # Default-view field discipline
//
// Every Render* function here follows docs/architecture/reporting.md §9's
// "the default view uses logical IDs, intent, impact, reason, and action"
// rule literally: it never prints a Candidate.Ref/native file path, a
// resolver Provenance.Operator, or a precedence-program name — those
// belong to Explain/Debug (issue #36), not to this package's four default
// views.
package tui
