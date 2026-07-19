package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// activeTabStyle/inactiveTabStyle deliberately set only Border/Padding —
// never Foreground/Background/Bold/Underline/any other color or text
// attribute. lipgloss resolves color output through the terminal's
// detected color profile, which differs between an interactive terminal,
// a piped `go test` run, and CI — exactly the "flaky ANSI-escape-sequence
// timing" doc.go's "Library choice" section calls out avoiding. Border
// runes and padding spaces, by contrast, are plain characters lipgloss
// emits unconditionally regardless of color profile, so the tab bar stays
// byte-for-byte identical in every one of this package's golden files no
// matter where `go test` runs.
var (
	activeTabStyle   = lipgloss.NewStyle().Border(lipgloss.NormalBorder()).Padding(0, 1)
	inactiveTabStyle = lipgloss.NewStyle().Padding(0, 1).Border(lipgloss.HiddenBorder())
)

// renderTabBar draws the four-view tab strip with active marked by a box
// border, matching docs/architecture/reporting.md §9's Workspace ordering
// (Overview, Drift, Assets, Generations).
func renderTabBar(active ViewKind) string {
	tabs := make([]string, 0, len(viewOrder))
	for _, v := range viewOrder {
		title := viewTitles[v]
		if v == active {
			tabs = append(tabs, activeTabStyle.Render(title))
		} else {
			tabs = append(tabs, inactiveTabStyle.Render(title))
		}
	}
	return strings.TrimRight(lipgloss.JoinHorizontal(lipgloss.Top, tabs...), " ")
}
