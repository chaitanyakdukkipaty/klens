package widgets

import (
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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
	visible bool
	entries []nsEntry
	cursor  int
	filter  string
}

func NewNamespacePicker() NamespacePicker { return NamespacePicker{} }

// Show opens the picker with cluster-discovered namespaces and persisted ones.
// clusterNs may be empty when the user lacks cluster-wide namespace listing.
func (p NamespacePicker) Show(clusterNs, savedNs []string) NamespacePicker {
	p.visible = true
	p.cursor = 0
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
	key, ok := msg.(tea.KeyMsg)
	if !ok {
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

	case "down", "j":
		filtered := p.filtered()
		if p.cursor < len(filtered)-1 {
			p.cursor++
		}

	case "backspace", "ctrl+h":
		if len(p.filter) > 0 {
			runes := []rune(p.filter)
			p.filter = string(runes[:len(runes)-1])
			p.cursor = 0
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
		// No matches — the typed text is a new namespace to add.
		if ns := strings.TrimSpace(p.filter); ns != "" {
			p.visible = false
			return p, func() tea.Msg { return NamespacePickedMsg{Namespace: ns, Save: true} }
		}

	default:
		if key.Type == tea.KeyRunes {
			p.filter += string(key.Runes)
			p.cursor = 0
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
	sb.WriteString(appstyles.Muted.Render(strings.Repeat("─", pickerWidth-2)) + "\n")

	if len(p.entries) == 0 {
		sb.WriteString(appstyles.Muted.Render(" (no namespaces discovered — type to add)") + "\n")
	} else if len(filtered) == 0 {
		sb.WriteString(appstyles.Muted.Render(" no matches") + "\n")
	}

	start := 0
	if len(filtered) > pickerMaxItems && cursor >= pickerMaxItems {
		start = cursor - pickerMaxItems + 1
	}
	end := start + pickerMaxItems
	if end > len(filtered) {
		end = len(filtered)
	}

	for i := start; i < end; i++ {
		e := filtered[i]
		selected := i == cursor

		var prefix, nameStr, tagStr string
		if selected {
			prefix = appstyles.Primary.Render("▶ ")
			nameStr = appstyles.Primary.Bold(true).Render(e.name)
		} else {
			prefix = "  "
			nameStr = e.name
		}
		if e.saved {
			tagStr = appstyles.Muted.Render("[saved]")
		}

		gap := pickerWidth - 2 -
			lipgloss.Width(prefix) -
			lipgloss.Width(e.name) -
			lipgloss.Width(tagStr) - 1
		if gap < 1 {
			gap = 1
		}
		sb.WriteString(prefix + nameStr + strings.Repeat(" ", gap) + tagStr + "\n")
	}

	sb.WriteString(appstyles.Muted.Render(strings.Repeat("─", pickerWidth-2)) + "\n")

	// Filter / add input
	filterDisplay := p.filter
	if filterDisplay == "" {
		filterDisplay = appstyles.Muted.Render("type to filter or add new…")
	}
	sb.WriteString(" > " + filterDisplay + "▌\n")

	// Hint
	if len(filtered) > 0 {
		hint := " [↑↓] nav  [enter] switch"
		if cursor < len(filtered) && filtered[cursor].saved {
			hint += "  [ctrl+d] remove saved"
		}
		hint += "  [esc] cancel"
		sb.WriteString(appstyles.Muted.Render(hint))
	} else {
		sb.WriteString(appstyles.Muted.Render(" [enter] add & switch  [esc] cancel"))
	}

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#00ADD8")).
		Padding(0, 1).
		Width(pickerWidth).
		Render(sb.String())

	return lipgloss.Place(pickerWidth+8, pickerMaxItems+10,
		lipgloss.Center, lipgloss.Center, box,
		lipgloss.WithWhitespaceBackground(lipgloss.Color("#0D0D1A")))
}
