package panels

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	k8sres "github.com/chaitanyak/klens/internal/k8s"
	"github.com/chaitanyak/klens/internal/ui/styles"
)

// NavPanel is the left-side resource type navigator.
type NavPanel struct {
	width       int
	height      int
	items       []navItem
	cursor      int
	focused     bool
	filter      string
	filterOn    bool
	filterInput string
	filtered    []navItem
}

type navItem struct {
	kind    string
	display string
}

func NewNavPanel(w, h int) NavPanel {
	items := make([]navItem, 0, len(k8sres.Registry))
	for _, r := range k8sres.Registry {
		items = append(items, navItem{kind: r.Kind, display: r.Kind})
	}
	p := NavPanel{width: w, height: h, items: items}
	p.filtered = p.items
	return p
}

func (n NavPanel) SetSize(w, h int) NavPanel { n.width = w; n.height = h; return n }
func (n NavPanel) SetFocused(f bool) NavPanel { n.focused = f; return n }
func (n NavPanel) FilterActive() bool        { return n.filterOn }
func (n NavPanel) ActiveKind() string {
	if len(n.filtered) == 0 {
		return ""
	}
	return n.filtered[n.cursor].kind
}

// SetActiveKind moves the cursor to the first item matching kind (case-insensitive).
// Resets any active filter so the item is visible.
func (n NavPanel) SetActiveKind(kind string) NavPanel {
	n.filter = ""
	n.filtered = n.items
	for i, item := range n.items {
		if strings.EqualFold(item.kind, kind) {
			n.cursor = i
			return n
		}
	}
	return n
}

func (n NavPanel) Update(msg tea.Msg) (NavPanel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if n.filterOn {
			switch msg.String() {
			case "enter":
				n.filterOn = false
				n.filter = n.filterInput
				n.applyNavFilter()
			case "esc":
				n.filterOn = false
				n.filterInput = ""
				n.filter = ""
				n.applyNavFilter()
			case "backspace":
				if len(n.filterInput) > 0 {
					n.filterInput = n.filterInput[:len(n.filterInput)-1]
					n.applyNavFilter()
				}
			default:
				if len(msg.String()) == 1 {
					n.filterInput += msg.String()
					n.applyNavFilter()
				}
			}
			return n, nil
		}
		switch msg.String() {
		case "up", "k":
			if len(n.filtered) > 0 {
				n.cursor = (n.cursor - 1 + len(n.filtered)) % len(n.filtered)
			}
		case "down", "j":
			if len(n.filtered) > 0 {
				n.cursor = (n.cursor + 1) % len(n.filtered)
			}
		case "/":
			n.filterOn = true
			n.filterInput = n.filter
		}
	}
	return n, nil
}

func (n *NavPanel) applyNavFilter() {
	if n.filterInput == "" {
		n.filtered = n.items
		return
	}
	low := strings.ToLower(n.filterInput)
	filtered := make([]navItem, 0, len(n.items))
	for _, item := range n.items {
		if strings.Contains(strings.ToLower(item.kind), low) {
			filtered = append(filtered, item)
		}
	}
	n.filtered = filtered
	if n.cursor >= len(n.filtered) {
		n.cursor = max(0, len(n.filtered)-1)
	}
}

func (n NavPanel) View() string {
	border := styles.NormalBorder
	if n.focused {
		border = styles.FocusedBorder
	}

	innerW := n.width - 2
	innerH := n.height - 2

	countInfo := ""
	if n.filter != "" {
		countInfo = styles.Muted.Render(fmt.Sprintf(" %d/%d", len(n.filtered), len(n.items)))
	}
	// PaddingLeft is included in Width (unlike MarginLeft), so the title
	// renders to exactly innerW chars — no overflow into the border.
	title := lipgloss.NewStyle().
		Foreground(styles.Primary.GetForeground()).
		Bold(true).
		PaddingLeft(1).
		Width(innerW).
		Render("Resources") + countInfo

	var rows []string
	rows = append(rows, title)

	filterBar := ""
	if n.filterOn {
		filterBar = styles.Primary.Render("filter: ") + n.filterInput + styles.Muted.Render("█")
	} else if n.filter != "" {
		filterBar = styles.Primary.Render("filter: ") + styles.Warning.Render(n.filter) + styles.Muted.Render("  (/ to change, esc to clear)")
	}
	if filterBar != "" {
		rows = append(rows, filterBar)
		innerH--
	}

	// prefix is 3 chars ("   " or " ▶ "); leave room for it when truncating.
	const prefix = 3
	maxLabel := innerW - prefix
	if maxLabel < 1 {
		maxLabel = 1
	}

	for i, item := range n.filtered {
		if i >= innerH-1 {
			break
		}
		label := item.display
		if len(label) > maxLabel {
			label = label[:maxLabel-1] + "…"
		}

		var row string
		if i == n.cursor {
			row = lipgloss.NewStyle().
				Background(lipgloss.Color("#1E3A5F")).
				Foreground(styles.Primary.GetForeground()).
				Bold(true).
				Width(innerW).
				Render(" ▶ " + label)
		} else {
			row = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#CCCCCC")).
				Width(innerW).
				Render("   " + label)
		}
		rows = append(rows, row)
	}

	content := strings.Join(rows, "\n")
	return border.Width(n.width - 2).Height(n.height - 2).Render(content)
}
