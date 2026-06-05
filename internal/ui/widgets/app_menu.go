package widgets

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	appstyles "github.com/chaitanyak/klens/internal/ui/styles"
)

// AppMenuItem identifies one entry in the ☰ menu.
type AppMenuItem int

const (
	MenuKeybindings AppMenuItem = iota
	MenuSettings
)

// AppMenuAction is emitted when the user picks an item or dismisses the menu.
type AppMenuAction struct {
	Item   AppMenuItem
	Picked bool // false = dismissed
}

// AppMenu is the small ☰ dropdown: Keybindings / Settings. Keyboard (j/k,
// enter, esc) and mouse (hover highlight, click) both work.
type AppMenu struct {
	visible bool
	cursor  int
	termW   int
	termH   int
}

var appMenuItems = []struct {
	item  AppMenuItem
	label string
	hint  string
}{
	{MenuKeybindings, "Keybindings", "?"},
	{MenuSettings, "Settings", ""},
}

func NewAppMenu() AppMenu { return AppMenu{} }

func (m AppMenu) Show() AppMenu {
	m.visible = true
	m.cursor = 0
	return m
}

func (m AppMenu) Hide() AppMenu       { m.visible = false; return m }
func (m AppMenu) IsVisible() bool     { return m.visible }
func (m AppMenu) SetSize(w, h int) AppMenu {
	m.termW = w
	m.termH = h
	return m
}

const appMenuWidth = 26

// boxOrigin returns the screen position of the rendered box's top-left after
// modalOverlay centers it.
func (m AppMenu) boxOrigin() (int, int) {
	box := m.View()
	x := (m.termW - lipgloss.Width(box)) / 2
	y := (m.termH - lipgloss.Height(box)) / 2
	return max(0, x), max(0, y)
}

// itemAt maps a screen click to a menu item index, or -1.
// Box layout: border row, then one row per item, border row.
func (m AppMenu) itemAt(x, y int) int {
	bx, by := m.boxOrigin()
	idx := y - by - 1
	if idx < 0 || idx >= len(appMenuItems) {
		return -1
	}
	if x <= bx || x >= bx+appMenuWidth-1 {
		return -1
	}
	return idx
}

func (m AppMenu) Update(msg tea.Msg) (AppMenu, tea.Cmd) {
	if !m.visible {
		return m, nil
	}
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "up", "k":
			m.cursor = (m.cursor - 1 + len(appMenuItems)) % len(appMenuItems)
		case "down", "j":
			m.cursor = (m.cursor + 1) % len(appMenuItems)
		case "enter":
			m.visible = false
			item := appMenuItems[m.cursor].item
			return m, func() tea.Msg { return AppMenuAction{Item: item, Picked: true} }
		case "esc", "q":
			m.visible = false
			return m, func() tea.Msg { return AppMenuAction{Picked: false} }
		}
	case tea.MouseMotionMsg:
		mouse := msg.Mouse()
		if idx := m.itemAt(mouse.X, mouse.Y); idx >= 0 {
			m.cursor = idx
		}
	case tea.MouseClickMsg:
		mouse := msg.Mouse()
		idx := m.itemAt(mouse.X, mouse.Y)
		m.visible = false
		if idx >= 0 {
			item := appMenuItems[idx].item
			return m, func() tea.Msg { return AppMenuAction{Item: item, Picked: true} }
		}
		return m, func() tea.Msg { return AppMenuAction{Picked: false} }
	}
	return m, nil
}

func (m AppMenu) View() string {
	inner := appMenuWidth - 2
	var rows []string
	for i, it := range appMenuItems {
		label := " " + it.label
		pad := inner - lipgloss.Width(label) - lipgloss.Width(it.hint) - 1
		if pad < 1 {
			pad = 1
		}
		line := label + strings.Repeat(" ", pad) + it.hint + " "
		if i == m.cursor {
			rows = append(rows, lipgloss.NewStyle().
				Background(appstyles.ColorSelection).
				Foreground(appstyles.ColorWhite).
				Width(inner).Render(line))
		} else {
			rows = append(rows, lipgloss.NewStyle().
				Foreground(appstyles.ColorBodyText).
				Width(inner).Render(line))
		}
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(appstyles.ColorPrimary).
		Width(appMenuWidth - 2).
		Render(strings.Join(rows, "\n"))
}
