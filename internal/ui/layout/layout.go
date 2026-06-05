package layout

import "charm.land/lipgloss/v2"

// PanelID identifies layout panels.
type PanelID int

const (
	PanelHeader PanelID = iota
	PanelNav
	PanelContent
	PanelStatus
)

// Rect is a positioned panel rectangle in absolute screen coordinates.
// X/Y are the top-left cell; Width/Height the panel's full outer size
// (borders included). Rect is the unit of mouse hit-testing: every
// clickable surface resolves a screen point to a panel via Contains and
// then works in panel-local coordinates via Local.
type Rect struct {
	X      int
	Y      int
	Width  int
	Height int
}

// Contains reports whether the screen point (x, y) falls inside r.
func (r Rect) Contains(x, y int) bool {
	return r.Width > 0 && r.Height > 0 &&
		x >= r.X && x < r.X+r.Width &&
		y >= r.Y && y < r.Y+r.Height
}

// Local translates the screen point (x, y) to coordinates relative to r's
// top-left corner. Callers should check Contains first when the point must
// be inside; drag handlers intentionally call Local on outside points to
// drive edge auto-scroll.
func (r Rect) Local(x, y int) (int, int) {
	return x - r.X, y - r.Y
}

// Layout computes positioned panel rectangles from the terminal size.
type Layout struct {
	termW int
	termH int
}

const (
	navWidthPct   = 22 // percent of terminal width for nav
	headerHeight  = 1  // rows
	statusHeight  = 1  // rows
	borderPadding = 2  // lipgloss rounded border = 2 extra rows/cols

	MinTermWidth  = 60 // minimum usable terminal width
	MinTermHeight = 18 // minimum usable terminal height
)

func New(w, h int) Layout {
	return Layout{termW: w, termH: h}
}

func (l Layout) Update(w, h int) Layout {
	l.termW = w
	l.termH = h
	return l
}

func (l Layout) Nav() Rect {
	w := max(l.termW*navWidthPct/100, 18)
	h := max(1, l.termH-headerHeight-statusHeight)
	return Rect{X: 0, Y: headerHeight, Width: w, Height: h}
}

func (l Layout) Content() Rect {
	navW := l.Nav().Width
	w := l.termW - navW
	h := max(1, l.termH-headerHeight-statusHeight)
	return Rect{X: navW, Y: headerHeight, Width: w, Height: h}
}

// Fullscreen is the content rectangle when fullscreen mode hides all chrome:
// the entire terminal.
func (l Layout) Fullscreen() Rect {
	return Rect{X: 0, Y: 0, Width: l.termW, Height: l.termH}
}

// TooSmall reports whether the terminal is below the minimum usable size.
func (l Layout) TooSmall() bool {
	return l.termW < MinTermWidth || l.termH < MinTermHeight
}

// TermSize returns the current terminal dimensions.
func (l Layout) TermSize() (w, h int) {
	return l.termW, l.termH
}

func (l Layout) Header() Rect {
	return Rect{X: 0, Y: 0, Width: l.termW, Height: headerHeight}
}

func (l Layout) Status() Rect {
	return Rect{X: 0, Y: max(0, l.termH-statusHeight), Width: l.termW, Height: statusHeight}
}

// InnerSize returns the usable inner dimensions of a bordered panel.
func InnerSize(r Rect) (w, h int) {
	return max(r.Width-borderPadding, 0), max(r.Height-borderPadding, 0)
}

// JoinPanels combines left nav and right content side by side.
func JoinPanels(nav, content string) string {
	return lipgloss.JoinHorizontal(lipgloss.Top, nav, content)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
