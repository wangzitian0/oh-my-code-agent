package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/wangzitian0/oh-my-code-agent/internal/report"
)

func TestNewModel_StartsOnOverview(t *testing.T) {
	m := NewModel(report.Artifact{})
	if m.Active() != ViewOverview {
		t.Errorf("Active() = %v, want ViewOverview", m.Active())
	}
}

func TestModel_Update_NavigatesForwardAndWraps(t *testing.T) {
	m := NewModel(report.Artifact{})
	order := []ViewKind{ViewOverview, ViewDrift, ViewAssets, ViewGenerations, ViewOverview}
	for i, want := range order {
		if m.Active() != want {
			t.Fatalf("step %d: Active() = %v, want %v", i, m.Active(), want)
		}
		next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRight})
		if cmd != nil {
			t.Fatalf("step %d: Update(right) returned a non-nil Cmd, want nil", i)
		}
		m = next.(Model)
	}
}

func TestModel_Update_NavigatesBackwardAndWraps(t *testing.T) {
	m := NewModel(report.Artifact{})
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	m = next.(Model)
	if m.Active() != ViewGenerations {
		t.Errorf("Active() after one left from Overview = %v, want ViewGenerations (wrap around)", m.Active())
	}
}

func TestModel_Update_DigitKeysJumpDirectly(t *testing.T) {
	m := NewModel(report.Artifact{})
	cases := []struct {
		key  string
		want ViewKind
	}{
		{"3", ViewAssets},
		{"1", ViewOverview},
		{"4", ViewGenerations},
		{"2", ViewDrift},
	}
	for _, c := range cases {
		next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(c.key)})
		m = next.(Model)
		if m.Active() != c.want {
			t.Errorf("Update(%q): Active() = %v, want %v", c.key, m.Active(), c.want)
		}
	}
}

func TestModel_Update_QuitKeysReturnTeaQuit(t *testing.T) {
	m := NewModel(report.Artifact{})
	for _, key := range []tea.KeyMsg{
		{Type: tea.KeyCtrlC},
		{Type: tea.KeyRunes, Runes: []rune("q")},
	} {
		_, cmd := m.Update(key)
		if cmd == nil {
			t.Fatalf("Update(%v) returned a nil Cmd, want tea.Quit", key)
		}
	}
}

func TestModel_Update_IgnoresNonKeyMessages(t *testing.T) {
	m := NewModel(report.Artifact{})
	next, cmd := m.Update(struct{}{})
	if cmd != nil {
		t.Errorf("Update(non-key msg) returned a non-nil Cmd, want nil")
	}
	if next.(Model).Active() != ViewOverview {
		t.Errorf("Update(non-key msg) changed Active(), want it left untouched")
	}
}

func TestModel_SetActive_RejectsOutOfRangeValue(t *testing.T) {
	m := NewModel(report.Artifact{})
	m = m.SetActive(ViewKind(99))
	if m.Active() != ViewOverview {
		t.Errorf("SetActive(out-of-range) changed Active() to %v, want it left at ViewOverview", m.Active())
	}
}

func TestModel_Init_ReturnsNilCmd(t *testing.T) {
	m := NewModel(report.Artifact{})
	if cmd := m.Init(); cmd != nil {
		t.Errorf("Init() returned a non-nil Cmd")
	}
}

func TestModel_View_IncludesTabBarAndActiveContent(t *testing.T) {
	m := NewModel(report.Artifact{})
	out := m.View()
	for _, title := range []string{"Overview", "Drift", "Assets", "Generations"} {
		if !contains(out, title) {
			t.Errorf("View() missing tab title %q:\n%s", title, out)
		}
	}
}
