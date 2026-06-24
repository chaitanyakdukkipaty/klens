package panels

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/chaitanyak/klens/internal/ui/styles"
)

// DockTab is one terminal session's render state in the dock tab bar.
// The panels package deliberately doesn't know about termsession — the app
// flattens its session registry into these for every render/hit-test.
type DockTab struct {
	Title  string
	Exited bool
}

// TerminalDock renders the embedded-terminal dock: a tab bar row over the
// active session's screen, inside a focus-aware border. Like TabBar it is a
// pure value rebuilt from current state for every render, so it can never go
// stale.
type TerminalDock struct {
	Width, Height int
	Tabs          []DockTab
	Active        int
	Focused       bool
	Maximized     bool
	// Body is the active session's pre-rendered emulator screen (ANSI).
	Body string
	// Scroll, when non-empty, is shown right-aligned in the tab bar to signal
	// that the active session is in scrollback (frozen) mode, e.g. "▲ 120/3000".
	Scroll string
}

const (
	dockTabMaxW   = 26  // cells per tab label, truncation bound
	dockCloseMark = "×" // per-tab close affordance
	dockMaxMark   = "⛶" // maximize toggle at the bar's right edge
)

// dockSeg is one rendered tab's text and horizontal extent, bar-local.
type dockSeg struct {
	idx    int
	text   string // plain (unstyled) cell text, includes close mark
	x      int    // start cell
	w      int    // cell width
	closeX int    // bar-local X of the close mark
}

// DockBodySize returns the emulator dimensions for a dock of outer size w×h:
// borders take 2 cols/rows and the tab bar one more row.
func DockBodySize(w, h int) (int, int) {
	return max(w-2, 0), max(h-3, 0)
}

// segments lays the tabs out left to right. Shared by View and TabAt so the
// rendered bar and the hit-test can never disagree (TabBar's invariant).
func (d TerminalDock) segments() []dockSeg {
	segs := make([]dockSeg, 0, len(d.Tabs))
	x := 1 // leading space
	for i, t := range d.Tabs {
		title := t.Title
		label := fmt.Sprintf("%d:%s", i+1, title)
		if lipgloss.Width(label) > dockTabMaxW {
			label = ansi.Truncate(label, dockTabMaxW-1, "…")
		}
		if t.Exited {
			label = "✕ " + label
		}
		text := label + " " + dockCloseMark
		w := lipgloss.Width(text)
		segs = append(segs, dockSeg{idx: i, text: text, x: x, w: w, closeX: x + w - 1})
		x += w + 3 // " │ " separator
	}
	return segs
}

// maxButtonX returns the bar-local X of the maximize toggle, or -1 when
// there's no room.
func (d TerminalDock) maxButtonX() int {
	segs := d.segments()
	if len(segs) == 0 {
		return -1
	}
	last := segs[len(segs)-1]
	bx := d.Width - 2 - 2 // border col + one-cell button + right margin
	if bx <= last.x+last.w {
		return -1
	}
	return bx
}

// TabAt resolves a tab-bar-local X (0 = first cell inside the left border)
// to the tab under it. close reports whether the hit was the × mark.
func (d TerminalDock) TabAt(x int) (idx int, close, ok bool) {
	for _, seg := range d.segments() {
		if x >= seg.x && x < seg.x+seg.w {
			return seg.idx, x == seg.closeX, true
		}
	}
	return 0, false, false
}

// MaxButtonAt reports whether a tab-bar-local X hits the maximize toggle.
func (d TerminalDock) MaxButtonAt(x int) bool {
	bx := d.maxButtonX()
	return bx != -1 && (x == bx || x == bx+1)
}

// View renders the dock at exactly Width×Height cells.
func (d TerminalDock) View() string {
	innerW, bodyH := DockBodySize(d.Width, d.Height)
	if innerW < 1 || bodyH < 0 {
		return ""
	}

	bar := d.renderTabBar(innerW)

	// Body: exact line count and width, so the dock band can never push the
	// status bar around. The emulator renders width-padded lines already;
	// truncation only matters during a transient resize mismatch.
	lines := strings.Split(d.Body, "\n")
	if len(lines) > bodyH {
		lines = lines[:bodyH]
	}
	for i, ln := range lines {
		lines[i] = ansi.Truncate(ln, innerW, "")
	}
	for len(lines) < bodyH {
		lines = append(lines, "")
	}

	content := bar
	if bodyH > 0 {
		content += "\n" + strings.Join(lines, "\n")
	}

	// lipgloss v2 Width/Height include the border frame (see nav_panel /
	// log_viewer, which pass their outer dims the same way).
	border := styles.NormalBorder
	if d.Focused {
		border = styles.FocusedBorder
	}
	return border.Width(d.Width).Height(d.Height).MaxHeight(d.Height).Render(content)
}

// renderTabBar builds the "1:pod × │ 2:pod ×" row, exactly innerW cells.
func (d TerminalDock) renderTabBar(innerW int) string {
	var b strings.Builder
	b.WriteString(" ")
	for i, seg := range d.segments() {
		if i > 0 {
			b.WriteString(" ")
			b.WriteString(styles.Muted.Render("│"))
			b.WriteString(" ")
		}
		b.WriteString(d.renderSeg(seg))
	}
	line := b.String()

	// Scroll indicator sits just left of the maximize toggle (or the right
	// edge when there's no toggle), flagging that the session is frozen in
	// scrollback. Styled with the focus color so it reads as an active mode.
	if d.Scroll != "" {
		label := styles.Primary.Render(d.Scroll)
		end := innerW - 1
		if bx := d.maxButtonX(); bx != -1 {
			end = bx - 2
		}
		start := end - lipgloss.Width(d.Scroll) + 1
		if pad := start - lipgloss.Width(line); pad > 0 {
			line += strings.Repeat(" ", pad) + label
		}
	}

	if bx := d.maxButtonX(); bx != -1 {
		pad := bx - lipgloss.Width(line)
		if pad > 0 {
			line += strings.Repeat(" ", pad)
			mark := dockMaxMark
			if d.Maximized {
				mark = "❐"
			}
			line += styles.Muted.Render(mark)
		}
	}
	return lipgloss.NewStyle().MaxWidth(innerW).Render(line)
}

// renderSeg styles one tab: active = primary bold; exited = muted regardless
// of activity (the ✕ prefix already flags it); close mark always muted.
func (d TerminalDock) renderSeg(seg dockSeg) string {
	t := d.Tabs[seg.idx]
	label := strings.TrimSuffix(seg.text, " "+dockCloseMark)
	var st lipgloss.Style
	switch {
	case t.Exited:
		st = styles.Muted
	case seg.idx == d.Active:
		st = styles.Primary.Bold(true)
	default:
		st = lipgloss.NewStyle().Foreground(styles.ColorBodyText)
	}
	if seg.idx == d.Active {
		st = st.Underline(true)
	}
	return st.Render(label) + " " + styles.Muted.Render(dockCloseMark)
}
