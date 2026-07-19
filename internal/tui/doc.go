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
// This package is the FOUNDATION layer only (issue #34): read-only
// rendering of the four named views, plus basic navigation between them.
// It never stages, activates, or rolls back anything — issue #35 ("TUI
// actions": activation/restart/rollback/confirmations) and issue #36
// ("TUI debug views": precedence trace/evidence) are separate, later PRs
// that build on this Model rather than modifying its read-only contract.
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
