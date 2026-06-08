package widgets

import (
	"fmt"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	appstyles "github.com/chaitanyak/klens/internal/ui/styles"
)

// NamespacePickedMsg is sent when the user selects or adds a namespace.
type NamespacePickedMsg struct {
	Namespace string
	Save      bool // true = user typed it manually (should be persisted)
}

// NamespaceRemovedMsg is sent when the user removes a saved namespace.
type NamespaceRemovedMsg struct{ Namespace string }

// NamespacePickerCancelMsg is sent when the user cancels without selecting.
type NamespacePickerCancelMsg struct{}

type nsEntry struct {
	name  string
	saved bool // manually persisted by the user
}

// NamespacePicker is a modal overlay for switching and managing namespaces.
type NamespacePicker struct {
	visible     bool
	entries     []nsEntry
	cursor      int
	scroll      int // index of the first visible row (viewport top)
	filter      string
	pendingSave string // non-empty when awaiting confirmation to save a new namespace
	termW       int
	termH       int
}

func NewNamespacePicker() NamespacePicker { return NamespacePicker{} }

// SetSize records the terminal dimensions so click/hover hit-testing can
// locate the centered box (modalOverlay centers View() in termW×termH).
func (p NamespacePicker) SetSize(w, h int) NamespacePicker {
	p.termW = w
	p.termH = h
	return p
}

// Show opens the picker with cluster-discovered namespaces and persisted ones.
// clusterNs may be empty when the user lacks cluster-wide namespace listing.
func (p NamespacePicker) Show(clusterNs, savedNs []string) NamespacePicker {
	p.visible = true
	p.cursor = 0
	p.scroll = 0
	p.filter = ""

	seen := map[string]bool{}
	var entries []nsEntry

	sorted := append([]string(nil), clusterNs...)
	sort.Strings(sorted)
	for _, ns := range sorted {
		if !seen[ns] {
			entries = append(entries, nsEntry{name: ns})
			seen[ns] = true
		}
	}
	for _, ns := range savedNs {
		if !seen[ns] {
			entries = append(entries, nsEntry{name: ns, saved: true})
			seen[ns] = true
		} else {
			for i := range entries {
				if entries[i].name == ns {
					entries[i].saved = true
				}
			}
		}
	}
	p.entries = entries
	return p
}

func (p NamespacePicker) Hide() NamespacePicker { p.visible = false; return p }
func (p NamespacePicker) IsVisible() bool       { return p.visible }

func (p NamespacePicker) filtered() []nsEntry {
	if p.filter == "" {
		return p.entries
	}
	var out []nsEntry
	for _, e := range p.entries {
		if strings.Contains(e.name, p.filter) {
			out = append(out, e)
		}
	}
	return out
}

// clampScroll keeps the viewport top within [0, n-pickerMaxItems].
func (p NamespacePicker) clampScroll(n int) int {
	maxStart := n - pickerMaxItems
	if maxStart < 0 {
		maxStart = 0
	}
	if p.scroll < 0 {
		return 0
	}
	if p.scroll > maxStart {
		return maxStart
	}
	return p.scroll
}

// window returns the [start,end) slice of filtered entries currently shown.
// The viewport top is explicit state (p.scroll), not derived from the cursor —
// so hovering a visible row never re-scrolls the list. Shared by View and the
// click hit-test so pixels and click targets can't drift.
func (p NamespacePicker) window(filtered []nsEntry) (start, end int) {
	start = p.clampScroll(len(filtered))
	end = start + pickerMaxItems
	if end > len(filtered) {
		end = len(filtered)
	}
	return start, end
}

// ensureVisible scrolls the viewport the minimum amount to keep the cursor in
// view. Called after cursor moves via keyboard or wheel — not on hover, where
// the cursor is set to an already-visible row.
func (p NamespacePicker) ensureVisible(n int) NamespacePicker {
	p.scroll = p.clampScroll(n)
	if p.cursor < p.scroll {
		p.scroll = p.cursor
	} else if p.cursor >= p.scroll+pickerMaxItems {
		p.scroll = p.cursor - pickerMaxItems + 1
	}
	p.scroll = p.clampScroll(n)
	return p
}

func (p NamespacePicker) clampCursor(list []nsEntry) int {
	if len(list) == 0 {
		return 0
	}
	if p.cursor >= len(list) {
		return len(list) - 1
	}
	return p.cursor
}

func (p NamespacePicker) Update(msg tea.Msg) (NamespacePicker, tea.Cmd) {
	if !p.visible {
		return p, nil
	}

	// Mouse interactions are inert while the save-confirmation prompt is up —
	// there's no list to scroll or click there.
	switch mm := msg.(type) {
	case tea.MouseWheelMsg:
		// Wheel scrolls the list (same cursor moves as ↑↓).
		if p.pendingSave == "" {
			filtered := p.filtered()
			switch mm.Button {
			case tea.MouseWheelUp:
				if p.cursor > 0 {
					p.cursor--
				}
			case tea.MouseWheelDown:
				if p.cursor < len(filtered)-1 {
					p.cursor++
				}
			}
			p = p.ensureVisible(len(filtered))
		}
		return p, nil

	case tea.MouseMotionMsg:
		// Hover highlights the row under the pointer.
		if p.pendingSave == "" {
			if idx := p.rowAt(mm.Mouse().X, mm.Mouse().Y); idx >= 0 {
				p.cursor = idx
			}
		}
		return p, nil

	case tea.MouseClickMsg:
		if p.pendingSave != "" || mm.Button != tea.MouseLeft {
			return p, nil
		}
		idx := p.rowAt(mm.Mouse().X, mm.Mouse().Y)
		if idx < 0 {
			// Click outside the list dismisses the picker.
			p.visible = false
			return p, func() tea.Msg { return NamespacePickerCancelMsg{} }
		}
		filtered := p.filtered()
		ns := filtered[idx].name
		p.visible = false
		return p, func() tea.Msg { return NamespacePickedMsg{Namespace: ns} }
	}

	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return p, nil
	}

	// When awaiting save confirmation, only enter/esc are active.
	if p.pendingSave != "" {
		switch key.String() {
		case "enter":
			ns := p.pendingSave
			p.visible = false
			p.pendingSave = ""
			return p, func() tea.Msg { return NamespacePickedMsg{Namespace: ns, Save: true} }
		case "esc":
			p.pendingSave = ""
		}
		return p, nil
	}

	switch key.String() {
	case "esc":
		p.visible = false
		return p, func() tea.Msg { return NamespacePickerCancelMsg{} }

	case "up", "k":
		if p.cursor > 0 {
			p.cursor--
		}
		p = p.ensureVisible(len(p.filtered()))

	case "down", "j":
		filtered := p.filtered()
		if p.cursor < len(filtered)-1 {
			p.cursor++
		}
		p = p.ensureVisible(len(filtered))

	case "backspace", "ctrl+h":
		if len(p.filter) > 0 {
			runes := []rune(p.filter)
			p.filter = string(runes[:len(runes)-1])
			p.cursor = 0
			p.scroll = 0
		}

	case "ctrl+d":
		filtered := p.filtered()
		p.cursor = p.clampCursor(filtered)
		if p.cursor < len(filtered) && filtered[p.cursor].saved {
			ns := filtered[p.cursor].name
			newEntries := p.entries[:0:0]
			for _, e := range p.entries {
				if e.name != ns {
					newEntries = append(newEntries, e)
				}
			}
			p.entries = newEntries
			updated := p.filtered()
			p.cursor = p.clampCursor(updated)
			p = p.ensureVisible(len(updated))
			return p, func() tea.Msg { return NamespaceRemovedMsg{Namespace: ns} }
		}

	case "enter":
		filtered := p.filtered()
		p.cursor = p.clampCursor(filtered)
		if len(filtered) > 0 {
			ns := filtered[p.cursor].name
			p.visible = false
			return p, func() tea.Msg { return NamespacePickedMsg{Namespace: ns} }
		}
		// No matches — enter confirm state before saving the new namespace.
		if ns := strings.TrimSpace(p.filter); ns != "" {
			p.pendingSave = ns
		}

	default:
		if len(key.Text) > 0 {
			p.filter += key.Text
			p.cursor = 0
			p.scroll = 0
		}
	}

	return p, nil
}

const (
	pickerWidth    = 52
	pickerMaxItems = 12
)

func (p NamespacePicker) View() string {
	if !p.visible {
		return ""
	}

	filtered := p.filtered()
	cursor := p.clampCursor(filtered)

	var sb strings.Builder

	title := appstyles.Primary.Bold(true).Render(" Namespace")
	sb.WriteString(title + "\n")
	sb.WriteString(appstyles.Muted.Render(strings.Repeat("─", pickerWidth-4)) + "\n")

	if len(p.entries) == 0 {
		sb.WriteString(appstyles.Muted.Render(" (no namespaces discovered — type to add)") + "\n")
	} else if len(filtered) == 0 {
		sb.WriteString(appstyles.Muted.Render(" no matches") + "\n")
	}

	start, end := p.window(filtered)

	for i := start; i < end; i++ {
		e := filtered[i]
		selected := i == cursor

		var prefix, tagStr string
		if selected {
			prefix = appstyles.Primary.Render("▶ ")
		} else {
			prefix = "  "
		}
		if e.saved {
			tagStr = appstyles.Muted.Render("[saved]")
		}

		// pickerWidth-4 is the box's inner text width (Width minus 2 border + 2
		// padding); the extra -1 keeps a one-column margin. Truncate the name to
		// what's left so the row is exactly one line — a wrapped name would shift
		// every row below it and break hit-testing.
		avail := pickerWidth - 4 - lipgloss.Width(prefix) - lipgloss.Width(tagStr) - 1
		disp := truncateName(e.name, avail)

		var nameStr string
		if selected {
			nameStr = appstyles.Primary.Bold(true).Render(disp)
		} else {
			nameStr = disp
		}

		gap := avail - lipgloss.Width(disp)
		if gap < 1 {
			gap = 1
		}
		sb.WriteString(prefix + nameStr + strings.Repeat(" ", gap) + tagStr + "\n")
	}

	sb.WriteString(appstyles.Muted.Render(strings.Repeat("─", pickerWidth-4)) + "\n")

	if p.pendingSave != "" {
		// Confirm state: user is about to save a new namespace entry.
		sb.WriteString(appstyles.Warning.Bold(true).Render(fmt.Sprintf(" Save %q?", p.pendingSave)) + "\n")
		sb.WriteString(appstyles.Muted.Render(" [enter] confirm  [esc] back"))
	} else {
		// Normal state: filter / add input + navigation hints.
		filterDisplay := p.filter
		if filterDisplay == "" {
			filterDisplay = appstyles.Muted.Render("type to filter or add new…")
		}
		sb.WriteString(" > " + filterDisplay + "▌\n")

		if len(filtered) > 0 {
			hint := " [↑↓] nav  [enter/click] switch"
			if cursor < len(filtered) && filtered[cursor].saved {
				hint += "  [ctrl+d] remove"
			}
			hint += "  [esc] cancel"
			sb.WriteString(appstyles.Muted.Render(hint))
		} else {
			sb.WriteString(appstyles.Muted.Render(" [enter] add & switch  [esc] cancel"))
		}
	}

	// Return just the box; the model's modalOverlay centers it in the terminal
	// (and boxOrigin mirrors that centering for click hit-testing).
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(appstyles.ColorPrimary).
		Padding(0, 1).
		Width(pickerWidth).
		Render(sb.String())
}

// boxOrigin returns the screen position of the rendered box's top-left after
// modalOverlay centers it in the terminal.
func (p NamespacePicker) boxOrigin() (int, int) {
	box := p.View()
	x := (p.termW - lipgloss.Width(box)) / 2
	y := (p.termH - lipgloss.Height(box)) / 2
	return max(0, x), max(0, y)
}

// rowAt maps a screen click to a filtered-entry index, or -1 when the click
// misses the list. The box interior runs: border, title, separator, then one
// row per visible item — so the first item sits at boxY+3.
func (p NamespacePicker) rowAt(x, y int) int {
	filtered := p.filtered()
	if len(filtered) == 0 {
		return -1
	}
	start, end := p.window(filtered)
	bx, by := p.boxOrigin()
	if x <= bx || x >= bx+pickerWidth-1 {
		return -1
	}
	row := y - (by + 3)
	if row < 0 || row >= end-start {
		return -1
	}
	return start + row
}
