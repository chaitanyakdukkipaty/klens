package widgets

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

func TestAppMenuKeyboardPick(t *testing.T) {
	m := NewAppMenu().SetSize(100, 40).Show()
	m, _ = m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	m, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.IsVisible() {
		t.Error("menu still visible after enter")
	}
	if cmd == nil {
		t.Fatal("no action emitted")
	}
	act, ok := cmd().(AppMenuAction)
	if !ok || !act.Picked || act.Item != MenuSettings {
		t.Errorf("action = %+v, want picked MenuSettings", act)
	}
}

func TestAppMenuClickMapsToItems(t *testing.T) {
	m := NewAppMenu().SetSize(100, 40).Show()
	bx, by := m.boxOrigin()
	// Row 0 (Keybindings) sits one row below the top border.
	if idx := m.itemAt(bx+3, by+1); idx != 0 {
		t.Errorf("itemAt first row = %d, want 0", idx)
	}
	if idx := m.itemAt(bx+3, by+2); idx != 1 {
		t.Errorf("itemAt second row = %d, want 1", idx)
	}
	if idx := m.itemAt(bx+3, by); idx != -1 {
		t.Errorf("itemAt border row = %d, want -1", idx)
	}
}

func TestSettingsChangeEmitsSnapshot(t *testing.T) {
	s := NewSettingsOverlay().SetSize(100, 40).
		Show([]string{"klens-dark", "catppuccin-mocha"}, "klens-dark", false, false, false)
	// Cursor starts on Theme; → cycles to the next preset.
	s, cmd := s.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	if cmd == nil {
		t.Fatal("no SettingsChanged emitted")
	}
	ch := cmd().(SettingsChanged)
	if ch.Theme != "catppuccin-mocha" {
		t.Errorf("Theme = %q, want catppuccin-mocha", ch.Theme)
	}
	// Down to Read-only, toggle on.
	s, _ = s.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	s, cmd = s.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if ch := cmd().(SettingsChanged); !ch.ReadOnly {
		t.Error("ReadOnly not toggled")
	}
	_ = s
}

func TestSettingsReadOnlyForcedIsInert(t *testing.T) {
	s := NewSettingsOverlay().SetSize(100, 40).
		Show([]string{"klens-dark"}, "klens-dark", true, true, false)
	s, _ = s.Update(tea.KeyPressMsg{Code: 'j', Text: "j"}) // onto Read-only
	s, cmd := s.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil {
		t.Error("forced read-only row emitted a change")
	}
	_ = s
}

func TestKeybindingsOverlayScrollClamps(t *testing.T) {
	k := NewKeybindingsOverlay().SetSize(100, 20).Show()
	k, _ = k.Update(tea.KeyPressMsg{Code: 'G', Text: "G"})
	if k.scroll != k.maxScroll() {
		t.Errorf("G: scroll = %d, want %d", k.scroll, k.maxScroll())
	}
	k, _ = k.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	if k.scroll > k.maxScroll() {
		t.Error("scroll exceeded max")
	}
	k, _ = k.Update(tea.KeyPressMsg{Code: 'g', Text: "g"})
	if k.scroll != 0 {
		t.Errorf("g: scroll = %d, want 0", k.scroll)
	}
	if v := k.View(); lipgloss.Height(v) > 20 {
		t.Errorf("overlay height %d exceeds terminal", lipgloss.Height(v))
	}
}
