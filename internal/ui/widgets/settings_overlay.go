package widgets

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	appstyles "github.com/chaitanyak/klens/internal/ui/styles"
)

// SettingsChanged is emitted on every settings mutation with the full new
// snapshot; the model applies it (theme switch, read-only, hover) and
// persists to config.json.
type SettingsChanged struct {
	Theme        string
	ReadOnly     bool
	DisableHover bool
}

// SettingsOverlay edits the persisted preferences in place: theme (applied
// live), read-only mode, and hover. Keyboard (j/k, ←/→/enter, esc) and
// mouse (hover, click row to change) both work.
type SettingsOverlay struct {
	visible bool
	cursor  int
	termW   int
	termH   int

	themeIdx     int
	themes       []string
	readOnly     bool
	roForced     bool // --readonly flag: config can't lower it
	disableHover bool
}

const (
	settingTheme = iota
	settingReadOnly
	settingHover
	settingCount
)

func NewSettingsOverlay() SettingsOverlay { return SettingsOverlay{} }

// Show opens the overlay with the current values. themes is the preset name
// list; roForced disables the read-only row (CLI flag overrides config).
func (s SettingsOverlay) Show(themes []string, theme string, readOnly, roForced, disableHover bool) SettingsOverlay {
	s.visible = true
	s.cursor = 0
	s.themes = themes
	s.themeIdx = 0
	for i, t := range themes {
		if t == theme {
			s.themeIdx = i
			break
		}
	}
	s.readOnly = readOnly
	s.roForced = roForced
	s.disableHover = disableHover
	return s
}

func (s SettingsOverlay) Hide() SettingsOverlay { s.visible = false; return s }
func (s SettingsOverlay) IsVisible() bool       { return s.visible }
func (s SettingsOverlay) SetSize(w, h int) SettingsOverlay {
	s.termW = w
	s.termH = h
	return s
}

const settingsWidth = 44

func (s SettingsOverlay) snapshot() tea.Cmd {
	changed := SettingsChanged{
		Theme:        s.themes[s.themeIdx],
		ReadOnly:     s.readOnly,
		DisableHover: s.disableHover,
	}
	return func() tea.Msg { return changed }
}

// change applies ±1 to the setting under the cursor; returns the emit cmd
// (nil when nothing changed).
func (s SettingsOverlay) change(dir int) (SettingsOverlay, tea.Cmd) {
	switch s.cursor {
	case settingTheme:
		n := len(s.themes)
		if n == 0 {
			return s, nil
		}
		s.themeIdx = (s.themeIdx + dir + n) % n
	case settingReadOnly:
		if s.roForced {
			return s, nil
		}
		s.readOnly = !s.readOnly
	case settingHover:
		s.disableHover = !s.disableHover
	}
	return s, s.snapshot()
}

func (s SettingsOverlay) boxOrigin() (int, int) {
	box := s.View()
	x := (s.termW - lipgloss.Width(box)) / 2
	y := (s.termH - lipgloss.Height(box)) / 2
	return max(0, x), max(0, y)
}

// rowAt maps a screen click to a setting row, or -1.
// Box: border, title, blank, rows..., blank, footer, border.
func (s SettingsOverlay) rowAt(x, y int) int {
	bx, by := s.boxOrigin()
	idx := y - by - 3
	if idx < 0 || idx >= settingCount {
		return -1
	}
	if x <= bx || x >= bx+settingsWidth-1 {
		return -1
	}
	return idx
}

func (s SettingsOverlay) Update(msg tea.Msg) (SettingsOverlay, tea.Cmd) {
	if !s.visible {
		return s, nil
	}
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc", "q":
			s.visible = false
		case "up", "k":
			s.cursor = (s.cursor - 1 + settingCount) % settingCount
		case "down", "j":
			s.cursor = (s.cursor + 1) % settingCount
		case "left", "h":
			return s.change(-1)
		case "right", "l", "enter", " ":
			return s.change(1)
		}
	case tea.MouseMotionMsg:
		mouse := msg.Mouse()
		if idx := s.rowAt(mouse.X, mouse.Y); idx >= 0 {
			s.cursor = idx
		}
	case tea.MouseClickMsg:
		mouse := msg.Mouse()
		if idx := s.rowAt(mouse.X, mouse.Y); idx >= 0 {
			s.cursor = idx
			return s.change(1)
		}
		s.visible = false
	}
	return s, nil
}

func (s SettingsOverlay) View() string {
	inner := settingsWidth - 4
	onOff := func(b bool) string {
		if b {
			return "on"
		}
		return "off"
	}

	theme := "—"
	if len(s.themes) > 0 {
		theme = "‹ " + s.themes[s.themeIdx] + " ›"
	}
	ro := onOff(s.readOnly)
	if s.roForced {
		ro += appstyles.Muted.Render(" (--readonly)")
	}
	rows := []struct {
		label string
		value string
	}{
		{"Theme", theme},
		{"Read-only", ro},
		{"Hover", onOff(!s.disableHover)},
	}

	var out []string
	out = append(out, appstyles.Title.Render("Settings"), "")
	for i, r := range rows {
		prefix := "   "
		if i == s.cursor {
			prefix = " ▶ "
		}
		pad := inner - lipgloss.Width(prefix) - lipgloss.Width(r.label) - lipgloss.Width(r.value) - 1
		if pad < 1 {
			pad = 1
		}
		line := prefix + r.label + strings.Repeat(" ", pad) + r.value + " "
		if i == s.cursor {
			out = append(out, appstyles.Primary.Render(line))
		} else {
			out = append(out, lipgloss.NewStyle().Foreground(appstyles.ColorBodyText).Render(line))
		}
	}
	out = append(out, "", appstyles.Muted.Render(" ←/→ change · esc close"))

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(appstyles.ColorPrimary).
		Padding(0, 1).
		Width(settingsWidth).
		Render(strings.Join(out, "\n"))
}
