package panels

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/chaitanyak/klens/internal/ui/styles"
)

// TabID identifies one tab in the mode-tab strip. The app maps these to its
// ContentMode; the panels package deliberately doesn't know about modes.
type TabID int

const (
	TabNone TabID = iota
	TabTable
	TabYAML
	TabLogs
	TabXRay
	TabMetrics
	TabDescribe
	// TabFullscreen is the [⛶] button at the strip's right edge, not a tab.
	TabFullscreen
)

// tabDefs is the strip in display order. Key is the existing action key the
// tab visually reinforces (tabs add mouse access; keys stay the fast path).
var tabDefs = []struct {
	ID    TabID
	Label string
	Key   string
}{
	{TabTable, "Table", "esc"},
	{TabYAML, "YAML", "y"},
	{TabLogs, "Logs", "l"},
	{TabXRay, "X-Ray", "x"},
	{TabMetrics, "Metrics", "m"},
	{TabDescribe, "Describe", "d"},
}

// TabBar is the clickable mode-tab strip rendered above the content panel.
// It is a pure value: the root model fills the fields from current state for
// every render or hit-test, so the strip can never go stale (no Set/resize
// plumbing, no stored copy to forget to update).
type TabBar struct {
	Width  int
	Active TabID
	// Editing marks the YAML tab with an unsaved-edit badge (●) while the
	// editor is open.
	Editing bool
	// Enabled gates tabs per selected kind (capability-driven). A nil map
	// enables everything; TabTable is always enabled regardless.
	Enabled map[TabID]bool
	// CanFullscreen shows the [⛶] button (detail modes only — the table
	// never goes fullscreen).
	CanFullscreen bool
	// Hover is the tab currently under the mouse pointer (TabNone when
	// hover is inactive); render-only.
	Hover TabID
}

// tabSeg is one rendered tab's text and horizontal extent.
type tabSeg struct {
	id   TabID
	text string // plain (unstyled) cell text
	x    int    // start cell, bar-local
	w    int    // cell width
}

const (
	tabBarLeftPad = 1   // leading space before the first tab
	tabGap        = 2   // spaces between tabs
	fsButton      = "⛶" // fullscreen toggle, right-aligned
)

// enabled reports whether a tab is clickable under the current Enabled map.
func (t TabBar) enabled(id TabID) bool {
	if id == TabTable || t.Enabled == nil {
		return true
	}
	return t.Enabled[id]
}

// IsEnabled is the exported form of enabled, for the app's click dispatch.
func (t TabBar) IsEnabled(id TabID) bool { return t.enabled(id) }

// TabName returns the display label for a tab.
func TabName(id TabID) string {
	for _, def := range tabDefs {
		if def.ID == id {
			return def.Label
		}
	}
	return ""
}

// segments lays the tabs out left to right. Shared by View and TabAt so the
// rendered strip and the hit-test can never disagree.
func (t TabBar) segments() []tabSeg {
	segs := make([]tabSeg, 0, len(tabDefs))
	x := tabBarLeftPad
	for _, def := range tabDefs {
		label := def.Label
		if def.ID == TabYAML && t.Editing {
			label += " ●"
		}
		text := label + "·" + def.Key
		w := lipgloss.Width(text)
		segs = append(segs, tabSeg{id: def.ID, text: text, x: x, w: w})
		x += w + tabGap
	}
	return segs
}

// fsButtonX returns the bar-local X of the fullscreen button, or -1 when the
// button is absent (no room or not applicable).
func (t TabBar) fsButtonX() int {
	if !t.CanFullscreen {
		return -1
	}
	segs := t.segments()
	last := segs[len(segs)-1]
	bx := t.Width - 2 // one-cell button + one-cell right margin
	if bx <= last.x+last.w {
		return -1
	}
	return bx
}

// TabAt resolves a bar-local X to the tab under it.
func (t TabBar) TabAt(x int) (TabID, bool) {
	if bx := t.fsButtonX(); bx != -1 && (x == bx || x == bx+1) {
		return TabFullscreen, true
	}
	for _, seg := range t.segments() {
		if x >= seg.x && x < seg.x+seg.w {
			return seg.id, true
		}
	}
	return TabNone, false
}

// View renders the strip as a single row, exactly Width cells.
func (t TabBar) View() string {
	var b strings.Builder
	b.WriteString(strings.Repeat(" ", tabBarLeftPad))
	segs := t.segments()
	for i, seg := range segs {
		if i > 0 {
			b.WriteString(strings.Repeat(" ", tabGap))
		}
		b.WriteString(t.renderSeg(seg))
	}
	line := b.String()

	if bx := t.fsButtonX(); bx != -1 {
		pad := bx - lipgloss.Width(line)
		if pad > 0 {
			line += strings.Repeat(" ", pad)
			fs := styles.Muted.Render(fsButton)
			if t.Hover == TabFullscreen {
				fs = styles.Primary.Render(fsButton)
			}
			line += fs
		}
	}

	return lipgloss.NewStyle().Width(t.Width).MaxHeight(1).Render(line)
}

// renderSeg styles one tab: active = accent+bold+underline; hovered =
// accent; enabled = muted body text; disabled = faint.
func (t TabBar) renderSeg(seg tabSeg) string {
	// Label and key hint get separate weights, so split the text again.
	parts := strings.SplitN(seg.text, "·", 2)
	label, key := parts[0], parts[1]

	switch {
	case seg.id == t.Active:
		st := lipgloss.NewStyle().Foreground(styles.ColorPrimary).Bold(true)
		return st.Underline(true).Render(label) + st.Render("·"+key)
	case !t.enabled(seg.id):
		st := styles.Muted.Faint(true)
		return st.Render(label + "·" + key)
	case seg.id == t.Hover:
		st := lipgloss.NewStyle().Foreground(styles.ColorPrimary)
		return st.Render(label) + styles.Muted.Render("·"+key)
	default:
		return lipgloss.NewStyle().Foreground(styles.ColorBodyText).Render(label) +
			styles.Muted.Render("·"+key)
	}
}
