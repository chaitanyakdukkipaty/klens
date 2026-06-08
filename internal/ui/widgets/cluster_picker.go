package widgets

import (
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	appstyles "github.com/chaitanyak/klens/internal/ui/styles"
)

// ClusterPickedMsg is sent when the user selects a context.
type ClusterPickedMsg struct{ Context string }

// ClusterPickerCancelMsg is sent when the user cancels without selecting.
type ClusterPickerCancelMsg struct{}

// ClusterPicker is a modal overlay for switching kubeconfig contexts.
type ClusterPicker struct {
	visible  bool
	contexts []string
	active   string
	cursor   int
	scroll   int // index of the first visible row (viewport top)
	filter   string
	termW    int
	termH    int
}

func NewClusterPicker() ClusterPicker { return ClusterPicker{} }

// SetSize records the terminal dimensions so click/hover hit-testing can
// locate the centered box (modalOverlay centers View() in termW×termH).
func (p ClusterPicker) SetSize(w, h int) ClusterPicker {
	p.termW = w
	p.termH = h
	return p
}

// Show opens the picker with the provided context list and marks the active one.
func (p ClusterPicker) Show(contexts []string, active string) ClusterPicker {
	p.visible = true
	p.filter = ""
	p.scroll = 0
	sorted := append([]string(nil), contexts...)
	sort.Strings(sorted)
	p.contexts = sorted
	p.active = active
	// position cursor on the currently active context
	p.cursor = 0
	for i, c := range sorted {
		if c == active {
			p.cursor = i
			break
		}
	}
	// Scroll the active context into view when it sits past the first page.
	p = p.ensureVisible(len(sorted))
	return p
}

func (p ClusterPicker) Hide() ClusterPicker { p.visible = false; return p }
func (p ClusterPicker) IsVisible() bool     { return p.visible }

func (p ClusterPicker) filtered() []string {
	if p.filter == "" {
		return p.contexts
	}
	var out []string
	for _, c := range p.contexts {
		if strings.Contains(strings.ToLower(c), strings.ToLower(p.filter)) {
			out = append(out, c)
		}
	}
	return out
}

// clampScroll keeps the viewport top within [0, n-clusterPickerMaxItems].
func (p ClusterPicker) clampScroll(n int) int {
	maxStart := n - clusterPickerMaxItems
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

// window returns the [start,end) slice of filtered contexts currently shown.
// The viewport top is explicit state (p.scroll), not derived from the cursor —
// so hovering a visible row never re-scrolls the list. Shared by View and the
// click hit-test so pixels and click targets can't drift.
func (p ClusterPicker) window(filtered []string) (start, end int) {
	start = p.clampScroll(len(filtered))
	end = start + clusterPickerMaxItems
	if end > len(filtered) {
		end = len(filtered)
	}
	return start, end
}

// ensureVisible scrolls the viewport the minimum amount to keep the cursor in
// view. Called after cursor moves via keyboard or wheel — not on hover, where
// the cursor is set to an already-visible row.
func (p ClusterPicker) ensureVisible(n int) ClusterPicker {
	p.scroll = p.clampScroll(n)
	if p.cursor < p.scroll {
		p.scroll = p.cursor
	} else if p.cursor >= p.scroll+clusterPickerMaxItems {
		p.scroll = p.cursor - clusterPickerMaxItems + 1
	}
	p.scroll = p.clampScroll(n)
	return p
}

// truncateName shortens s to at most maxW display columns so each picker row
// stays exactly one line — a wrapped name would shift every row below it and
// break click/hover hit-testing, which assumes one screen row per item.
//
// It ellipsizes the MIDDLE, keeping both the head and the tail, because long
// names like EKS ARNs (arn:aws:eks:<region>:<acct>:cluster/<name>) share a
// long common prefix and differ in the region (middle) and cluster name
// (end) — tail-only truncation would render them indistinguishable. The tail
// gets the extra column on odd splits since the distinguishing name trails.
func truncateName(s string, maxW int) string {
	if maxW <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= maxW {
		return s
	}
	if maxW == 1 {
		return "…"
	}
	r := []rune(s)
	keep := maxW - 1 // one column for the ellipsis
	head := keep / 2
	tail := keep - head
	return string(r[:head]) + "…" + string(r[len(r)-tail:])
}

func clamp(v, max int) int {
	if v < 0 {
		return 0
	}
	if max == 0 {
		return 0
	}
	if v >= max {
		return max - 1
	}
	return v
}

func (p ClusterPicker) Update(msg tea.Msg) (ClusterPicker, tea.Cmd) {
	if !p.visible {
		return p, nil
	}

	switch mm := msg.(type) {
	case tea.MouseWheelMsg:
		// Wheel scrolls the context list (same cursor moves as ↑↓).
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
		return p, nil

	case tea.MouseMotionMsg:
		// Hover highlights the row under the pointer.
		if idx := p.rowAt(mm.Mouse().X, mm.Mouse().Y); idx >= 0 {
			p.cursor = idx
		}
		return p, nil

	case tea.MouseClickMsg:
		if mm.Button != tea.MouseLeft {
			return p, nil
		}
		idx := p.rowAt(mm.Mouse().X, mm.Mouse().Y)
		if idx < 0 {
			// Click outside the list dismisses the picker.
			p.visible = false
			return p, func() tea.Msg { return ClusterPickerCancelMsg{} }
		}
		ctx := p.filtered()[idx]
		p.visible = false
		return p, func() tea.Msg { return ClusterPickedMsg{Context: ctx} }
	}

	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return p, nil
	}

	switch key.String() {
	case "esc":
		p.visible = false
		return p, func() tea.Msg { return ClusterPickerCancelMsg{} }

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

	case "enter":
		filtered := p.filtered()
		p.cursor = clamp(p.cursor, len(filtered))
		if len(filtered) > 0 {
			ctx := filtered[p.cursor]
			p.visible = false
			return p, func() tea.Msg { return ClusterPickedMsg{Context: ctx} }
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
	clusterPickerWidth    = 56
	clusterPickerMaxItems = 12
)

func (p ClusterPicker) View() string {
	if !p.visible {
		return ""
	}

	filtered := p.filtered()
	cursor := clamp(p.cursor, len(filtered))

	var sb strings.Builder

	title := appstyles.Primary.Bold(true).Render(" Cluster Context")
	sb.WriteString(title + "\n")
	sb.WriteString(appstyles.Muted.Render(strings.Repeat("─", clusterPickerWidth-4)) + "\n")

	if len(filtered) == 0 {
		sb.WriteString(appstyles.Muted.Render(" no matches") + "\n")
	}

	start, end := p.window(filtered)

	for i := start; i < end; i++ {
		c := filtered[i]
		selected := i == cursor
		isActive := c == p.active

		var prefix, tagStr string
		if selected {
			prefix = appstyles.Primary.Render("▶ ")
		} else {
			prefix = "  "
		}
		if isActive {
			tagStr = appstyles.Muted.Render("[active]")
		}

		// clusterPickerWidth-4 is the box's inner text width (Width minus 2
		// border + 2 padding); the extra -1 keeps a one-column margin. Truncate
		// the name to what's left so the row is exactly one line — a wrapped
		// name would shift every row below it and break hit-testing.
		avail := clusterPickerWidth - 4 - lipgloss.Width(prefix) - lipgloss.Width(tagStr) - 1
		disp := truncateName(c, avail)

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

	sb.WriteString(appstyles.Muted.Render(strings.Repeat("─", clusterPickerWidth-4)) + "\n")

	filterDisplay := p.filter
	if filterDisplay == "" {
		filterDisplay = appstyles.Muted.Render("type to filter…")
	}
	sb.WriteString(" > " + filterDisplay + "▌\n")
	sb.WriteString(appstyles.Muted.Render(" [↑↓] nav  [enter/click] switch  [esc] cancel"))

	// Return just the box; the model's modalOverlay centers it in the terminal
	// (and boxOrigin mirrors that centering for click hit-testing).
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(appstyles.ColorPrimary).
		Padding(0, 1).
		Width(clusterPickerWidth).
		Render(sb.String())
}

// boxOrigin returns the screen position of the rendered box's top-left after
// modalOverlay centers it in the terminal.
func (p ClusterPicker) boxOrigin() (int, int) {
	box := p.View()
	x := (p.termW - lipgloss.Width(box)) / 2
	y := (p.termH - lipgloss.Height(box)) / 2
	return max(0, x), max(0, y)
}

// rowAt maps a screen click to a filtered-context index, or -1 when the click
// misses the list. The box interior runs: border, title, separator, then one
// row per visible item — so the first item sits at boxY+3.
func (p ClusterPicker) rowAt(x, y int) int {
	filtered := p.filtered()
	if len(filtered) == 0 {
		return -1
	}
	start, end := p.window(filtered)
	bx, by := p.boxOrigin()
	if x <= bx || x >= bx+clusterPickerWidth-1 {
		return -1
	}
	row := y - (by + 3)
	if row < 0 || row >= end-start {
		return -1
	}
	return start + row
}
