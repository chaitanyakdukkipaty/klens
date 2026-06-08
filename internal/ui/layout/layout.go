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
	// dockH is the terminal dock band's height in rows (0 = no dock). The
	// dock sits between the middle section and the status bar, full width;
	// nav / tab bar / content give up that many rows.
	dockH int
}

const (
	navWidthPct   = 22 // percent of terminal width for nav
	headerHeight  = 1  // rows
	tabBarHeight  = 1  // rows — mode-tab strip above the content panel
	statusHeight  = 1  // rows
	borderPadding = 2  // lipgloss rounded border = 2 extra rows/cols

	dockHeightPct = 40 // percent of the middle band for the terminal dock
	minDockHeight = 8  // border (2) + tab bar (1) + a usable shell viewport
	minContentH   = 8  // rows the content panel keeps when the dock is open

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

// WithDockHeight sets the terminal dock band height (0 hides it).
func (l Layout) WithDockHeight(h int) Layout {
	l.dockH = max(0, h)
	return l
}

// middleHeight is the band between header and status bar.
func (l Layout) middleHeight() int {
	return max(1, l.termH-headerHeight-statusHeight)
}

// DockHeightFor computes the dock band height for the current terminal size:
// dockHeightPct of the middle band, clamped so the content keeps minContentH
// rows; the full middle band when maximized.
func (l Layout) DockHeightFor(maximized bool) int {
	middleH := l.middleHeight()
	if maximized {
		return middleH
	}
	h := middleH * dockHeightPct / 100
	if h < minDockHeight {
		h = minDockHeight
	}
	if h > middleH-minContentH {
		h = middleH - minContentH
	}
	return max(0, h)
}

// Dock is the terminal dock band: full width, directly above the status bar.
// Zero Rect when no dock is set.
func (l Layout) Dock() Rect {
	if l.dockH <= 0 {
		return Rect{}
	}
	return Rect{
		X:      0,
		Y:      max(headerHeight, l.termH-statusHeight-l.dockH),
		Width:  l.termW,
		Height: min(l.dockH, l.middleHeight()),
	}
}

func (l Layout) Nav() Rect {
	w := max(l.termW*navWidthPct/100, 18)
	h := max(1, l.termH-headerHeight-statusHeight-l.dockH)
	return Rect{X: 0, Y: headerHeight, Width: w, Height: h}
}

// TabBar is the one-row mode-tab strip sitting above the content panel,
// spanning the content column (the nav keeps the full side column).
func (l Layout) TabBar() Rect {
	navW := l.Nav().Width
	return Rect{X: navW, Y: headerHeight, Width: l.termW - navW, Height: tabBarHeight}
}

func (l Layout) Content() Rect {
	navW := l.Nav().Width
	w := l.termW - navW
	h := max(1, l.termH-headerHeight-tabBarHeight-statusHeight-l.dockH)
	return Rect{X: navW, Y: headerHeight + tabBarHeight, Width: w, Height: h}
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
