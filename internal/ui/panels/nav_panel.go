package panels

import (
	"fmt"
	"strings"

	"github.com/atotto/clipboard"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	k8sres "github.com/chaitanyak/klens/internal/k8s"
	"github.com/chaitanyak/klens/internal/ui/styles"
)

// navTitleBase, navCursorBase, navBodyBase are pre-built without Width so that
// per-render calls only incur one copy (`.Width(w)`) instead of a full style chain.
var (
	navTitleBase  = lipgloss.NewStyle().Foreground(styles.ColorPrimary).Bold(true).PaddingLeft(1)
	navCursorBase = lipgloss.NewStyle().Foreground(styles.ColorPrimary).Bold(true)
	navBodyBase   = lipgloss.NewStyle().Foreground(styles.ColorBodyText)
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
	case tea.PasteMsg:
		if n.filterOn {
			clean := strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ").Replace(msg.Content)
			n.filterInput += clean
			n.applyNavFilter()
		}
		return n, nil
	case tea.MouseWheelMsg:
		switch msg.Button {
		case tea.MouseWheelUp:
			if len(n.filtered) > 0 {
				n.cursor = (n.cursor - 1 + len(n.filtered)) % len(n.filtered)
			}
		case tea.MouseWheelDown:
			if len(n.filtered) > 0 {
				n.cursor = (n.cursor + 1) % len(n.filtered)
			}
		}
		return n, nil
	case tea.KeyPressMsg:
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
			case "ctrl+v":
				if text, err := clipboard.ReadAll(); err == nil && text != "" {
					clean := strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ").Replace(text)
					n.filterInput += clean
					n.applyNavFilter()
				}
			default:
				if len(msg.Text) > 0 {
					n.filterInput += msg.Text
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
		case "g":
			n.cursor = 0
		case "G":
			if len(n.filtered) > 0 {
				n.cursor = len(n.filtered) - 1
			}
		case "/":
			n.filterOn = true
			n.filterInput = n.filter
		case "esc":
			n.filter = ""
			n.filterInput = ""
			n.applyNavFilter()
		}
	}
	return n, nil
}

// HandleClickAt moves the cursor to the item at panel-inner-Y.
// Returns the new active kind ("" if click was on title / filter / empty area).
func (n NavPanel) HandleClickAt(innerY int) (NavPanel, string) {
	firstRowY := 1 // title at line 0
	if n.filterOn || n.filter != "" {
		firstRowY = 2 // + filter bar
	}
	idx := innerY - firstRowY
	if idx < 0 || idx >= len(n.filtered) {
		return n, ""
	}
	n.cursor = idx
	return n, n.filtered[idx].kind
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

	innerW := max(1, n.width-2)
	innerH := max(1, n.height-2)

	countInfo := ""
	if n.filter != "" {
		countInfo = styles.Muted.Render(fmt.Sprintf(" %d/%d", len(n.filtered), len(n.items)))
	}
	title := navTitleBase.Width(innerW).Render("Resources") + countInfo

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
			row = navCursorBase.Width(innerW).Render(" ▶ " + label)
		} else {
			row = navBodyBase.Width(innerW).Render("   " + label)
		}
		rows = append(rows, row)
	}

	content := strings.Join(rows, "\n")
	return border.Width(max(1, n.width)).Height(max(1, n.height)).Render(content)
}
