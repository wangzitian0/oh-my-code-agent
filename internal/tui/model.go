package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/wangzitian0/oh-my-code-agent/internal/report"
)

// ViewKind names one of this package's four read-only views (issue #34's
// own named list). Order here is also tab order and Model's own
// left/right navigation order.
type ViewKind int

const (
	ViewOverview ViewKind = iota
	ViewDrift
	ViewAssets
	ViewGenerations
)

// viewOrder is the fixed navigation cycle basic left/right/tab navigation
// walks — issue #34's AC requires nothing more elaborate than this.
var viewOrder = []ViewKind{ViewOverview, ViewDrift, ViewAssets, ViewGenerations}

// viewTitles labels each ViewKind for the tab bar (tabbar.go) and for
// error messages; kept next to viewOrder since both enumerate the same
// closed set.
var viewTitles = map[ViewKind]string{
	ViewOverview:    "Overview",
	ViewDrift:       "Drift",
	ViewAssets:      "Assets",
	ViewGenerations: "Generations",
}

// Model is this package's bubbletea.Model: an immutable report.Artifact
// (the same one the CLI renders from — never recomputed, never mutated by
// this package) plus which of the four views is currently active. Model
// carries no other mutable state: this PR is the read-only foundation
// layer only (doc.go's "Scope" section), so there is nothing here yet for
// issue #35's future actions (activation/restart/rollback confirmations)
// to thread through beyond this same Artifact/active-view shape.
type Model struct {
	Artifact report.Artifact
	active   ViewKind
}

// NewModel constructs a Model over a already-built report.Artifact,
// starting on the Overview view — the same starting point `omca report`
// gives the CLI.
func NewModel(a report.Artifact) Model {
	return Model{Artifact: a, active: ViewOverview}
}

// Active returns the currently selected view.
func (m Model) Active() ViewKind {
	return m.active
}

// SetActive selects v as the current view, ignoring any value outside
// viewOrder (defensive: Model's own Update never produces one, but a test
// or future caller constructing a Model directly should not be able to put
// it in a state View() cannot render).
func (m Model) SetActive(v ViewKind) Model {
	for _, candidate := range viewOrder {
		if candidate == v {
			m.active = v
			return m
		}
	}
	return m
}

// Init satisfies tea.Model. This package has no async work to kick off —
// the Artifact is already fully built by the time NewModel is called
// (cmd/omca's TUI entry point builds it up front, the same
// buildArtifactForCLI pipeline the CLI report commands use), so there is
// nothing to Init.
func (m Model) Init() tea.Cmd {
	return nil
}

// Update handles basic navigation between the four views — arrow/tab keys
// to cycle, digit keys 1-4 to jump directly, q/ctrl+c to quit. Issue #34's
// own AC requires nothing more than this ("no interactivity beyond basic
// navigation between the four views"); issue #35 is where richer key
// handling (activation/restart/rollback confirmations) belongs.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	switch keyMsg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "right", "l", "tab":
		m.active = viewOrder[(indexOf(m.active)+1)%len(viewOrder)]
	case "left", "h", "shift+tab":
		m.active = viewOrder[(indexOf(m.active)-1+len(viewOrder))%len(viewOrder)]
	case "1":
		m.active = ViewOverview
	case "2":
		m.active = ViewDrift
	case "3":
		m.active = ViewAssets
	case "4":
		m.active = ViewGenerations
	}
	return m, nil
}

func indexOf(v ViewKind) int {
	for i, candidate := range viewOrder {
		if candidate == v {
			return i
		}
	}
	return 0
}

// View renders the tab bar plus the active view's content — the pure
// function doc.go's "Library choice" section documents as this package's
// entire snapshot-testing strategy: no terminal, no event loop, just
// Model -> string.
func (m Model) View() string {
	return renderTabBar(m.active) + "\n\n" + m.renderActive()
}

func (m Model) renderActive() string {
	switch m.active {
	case ViewOverview:
		return RenderOverview(m.Artifact)
	case ViewDrift:
		return RenderDrift(m.Artifact)
	case ViewAssets:
		return RenderAssets(m.Artifact)
	case ViewGenerations:
		return RenderGenerations(m.Artifact)
	default:
		return ""
	}
}
