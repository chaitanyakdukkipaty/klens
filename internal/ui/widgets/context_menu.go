package widgets

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	appstyles "github.com/chaitanyak/klens/internal/ui/styles"
)

// MenuItem is a single entry in the context menu.
type MenuItem struct {
	Label  string // shown to the user, e.g. "View YAML"
	Action string // dispatched on pick — matches model.dispatchTableAction keys
	Hint   string // single-key keyboard hint, e.g. "y"
}

// ContextMenuPickedMsg is sent when the user picks an item.
type ContextMenuPickedMsg struct{ Action string }

// ContextMenuCancelMsg is sent when the user dismisses the menu without picking.
type ContextMenuCancelMsg struct{}

// ContextMenu is a centered modal listing actions for the currently-selected resource.
type ContextMenu struct {
	visible bool
	title   string
	items   []MenuItem
	cursor  int
	termW   int
	termH   int
}

func NewContextMenu() ContextMenu { return ContextMenu{termW: 80, termH: 24} }

func (m ContextMenu) Show(title string, items []MenuItem) ContextMenu {
	m.visible = true
	m.title = title
	m.items = items
	m.cursor = 0
	return m
}

func (m ContextMenu) Hide() ContextMenu { m.visible = false; return m }
func (m ContextMenu) IsVisible() bool   { return m.visible }

// SetSize records the current terminal dimensions so click coordinates can be
// mapped to menu rows.
func (m ContextMenu) SetSize(w, h int) ContextMenu {
	m.termW = w
	m.termH = h
	return m
}

const (
	contextMenuWidth = 50 // visible box width (incl. border)
)

// innerWidth returns the usable width inside the box (after border + padding).
// Box visual width = border (2) + horizontal padding (4) + content (rest).
func innerWidth() int {
	return contextMenuWidth - 2 - 4 // border (2) + padding 2 cols * 2 sides (4)
}

// titleLines returns how many display rows the title needs after lipgloss wraps it.
// Uses the actual renderer (which does word-aware wrapping at hyphens) instead of
// pure character-count math so that click coordinates match what the user sees.
func (m ContextMenu) titleLines() int {
	t := m.title
	if t == "" {
		t = "Actions"
	}
	w := innerWidth()
	if w <= 0 {
		return 1
	}
	rendered := lipgloss.NewStyle().Width(w).Render(t)
	h := lipgloss.Height(rendered)
	if h < 1 {
		h = 1
	}
	return h
}

// boxHeight is the rendered height of the modal box, including its borders and padding.
func (m ContextMenu) boxHeight() int {
	// Inner rows: title + sep + items + sep + hint
	inner := m.titleLines() + 1 + len(m.items) + 1 + 1
	// + top/bottom border (2) + top/bottom vertical padding (2)
	return inner + 2 + 2
}

// boxStartY returns the screen row of the top border once modalOverlay centers
// this widget on the full terminal.
func (m ContextMenu) boxStartY() int {
	y := (m.termH - m.boxHeight()) / 2
	if y < 0 {
		y = 0
	}
	return y
}

// boxStartX returns the screen column of the box's left border once modalOverlay
// centers this widget on the full terminal.
func (m ContextMenu) boxStartX() int {
	x := (m.termW - contextMenuWidth) / 2
	if x < 0 {
		x = 0
	}
	return x
}

// clickInBox reports whether (screenX, screenY) lies inside the rendered box.
func (m ContextMenu) clickInBox(screenX, screenY int) bool {
	x0 := m.boxStartX()
	y0 := m.boxStartY()
	return screenX >= x0 && screenX < x0+contextMenuWidth &&
		screenY >= y0 && screenY < y0+m.boxHeight()
}

// itemRowAt returns the menu-item index at (screenX, screenY), or -1 if the
// position is not on an item row inside the box.
func (m ContextMenu) itemRowAt(screenX, screenY int) int {
	if !m.clickInBox(screenX, screenY) {
		return -1
	}
	// Layout from boxStartY (with Padding(1,1)):
	//   +0           top border
	//   +1           top padding (blank row)
	//   +2..+1+T     title (T = titleLines)
	//   +2+T         separator
	//   +3+T..       items (one per line)
	first := m.boxStartY() + 2 + m.titleLines() + 1
	idx := screenY - first
	if idx < 0 || idx >= len(m.items) {
		return -1
	}
	return idx
}

func (m ContextMenu) Update(msg tea.Msg) (ContextMenu, tea.Cmd) {
	if !m.visible {
		return m, nil
	}
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		key := msg.String()
		// Direct hint-key shortcut: pressing 'y' in the menu picks View YAML,
		// 'l' picks logs, etc. Mirrors the same letters shown on each row.
		for _, item := range m.items {
			if item.Hint != "" && item.Hint == key {
				action := item.Action
				m.visible = false
				return m, func() tea.Msg { return ContextMenuPickedMsg{Action: action} }
			}
		}
		switch key {
		case "esc":
			m.visible = false
			return m, func() tea.Msg { return ContextMenuCancelMsg{} }
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.items)-1 {
				m.cursor++
			}
		case "home", "g":
			m.cursor = 0
		case "end", "G":
			if len(m.items) > 0 {
				m.cursor = len(m.items) - 1
			}
		case "enter", " ":
			if len(m.items) == 0 {
				return m, nil
			}
			action := m.items[m.cursor].Action
			m.visible = false
			return m, func() tea.Msg { return ContextMenuPickedMsg{Action: action} }
		}
	case tea.MouseWheelMsg:
		switch msg.Button {
		case tea.MouseWheelUp:
			if m.cursor > 0 {
				m.cursor--
			}
		case tea.MouseWheelDown:
			if m.cursor < len(m.items)-1 {
				m.cursor++
			}
		}
	case tea.MouseMotionMsg:
		// Best-effort hover-to-highlight; only fires while a button is pressed
		// in cell-motion mode but harmless when no events arrive.
		mouse := msg.Mouse()
		if idx := m.itemRowAt(mouse.X, mouse.Y); idx >= 0 {
			m.cursor = idx
		}
	case tea.MouseClickMsg:
		if msg.Button != tea.MouseLeft && msg.Button != tea.MouseRight {
			break
		}
		mouse := msg.Mouse()
		// Clicks outside the modal box dismiss; clicks inside but off an item
		// row (e.g. on title / separator / hint) are absorbed without picking.
		if !m.clickInBox(mouse.X, mouse.Y) {
			m.visible = false
			return m, func() tea.Msg { return ContextMenuCancelMsg{} }
		}
		idx := m.itemRowAt(mouse.X, mouse.Y)
		if idx < 0 {
			return m, nil
		}
		m.cursor = idx
		action := m.items[idx].Action
		m.visible = false
		return m, func() tea.Msg { return ContextMenuPickedMsg{Action: action} }
	}
	return m, nil
}

func (m ContextMenu) View() string {
	if !m.visible {
		return ""
	}

	inner := innerWidth()
	titleLineStyle := lipgloss.NewStyle().Width(inner)
	rowStyle := lipgloss.NewStyle().Width(inner)

	titleText := m.title
	if titleText == "" {
		titleText = "Actions"
	}

	var sb strings.Builder
	// Title — let lipgloss wrap it within innerWidth.
	sb.WriteString(titleLineStyle.Bold(true).Foreground(appstyles.ColorPrimary).Render(titleText))
	sb.WriteString("\n")
	sb.WriteString(appstyles.Muted.Render(strings.Repeat("─", inner)))
	sb.WriteString("\n")

	for i, item := range m.items {
		selected := i == m.cursor

		prefix := "  "
		if selected {
			prefix = "▶ "
		}
		hint := ""
		if item.Hint != "" {
			hint = "[" + item.Hint + "]"
		}

		// Build a single fixed-width row: "  Label                [y]"
		left := prefix + item.Label
		pad := inner - lipgloss.Width(left) - lipgloss.Width(hint)
		if pad < 1 {
			pad = 1
		}

		var row string
		if selected {
			row = rowStyle.
				Bold(true).
				Foreground(appstyles.ColorPrimary).
				Render(left + strings.Repeat(" ", pad) + hint)
		} else {
			// Two-tone: label as default, hint muted. Width is pre-padded so
			// the styled hint sits at the right edge cleanly.
			row = left + strings.Repeat(" ", pad) + appstyles.Muted.Render(hint)
		}
		sb.WriteString(row)
		sb.WriteString("\n")
	}

	sb.WriteString(appstyles.Muted.Render(strings.Repeat("─", inner)))
	sb.WriteString("\n")
	sb.WriteString(appstyles.Muted.Render("[↑↓] nav  [enter] pick  [esc] cancel"))

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(appstyles.ColorPrimary).
		Padding(1, 2).
		Render(sb.String())

	// Centering on the full terminal is done by the caller (modalOverlay),
	// so just return the bare box here.
	return box
}
