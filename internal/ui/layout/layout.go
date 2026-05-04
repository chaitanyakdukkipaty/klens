package layout

import "github.com/charmbracelet/lipgloss"

// PanelID identifies layout panels.
type PanelID int

const (
	PanelHeader PanelID = iota
	PanelNav
	PanelContent
	PanelStatus
)

// Dimensions holds computed width/height for a panel.
type Dimensions struct {
	Width  int
	Height int
}

// Layout computes panel dimensions from the terminal size.
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

func (l Layout) Nav() Dimensions {
	w := max(l.termW*navWidthPct/100, 18)
	h := max(1, l.termH-headerHeight-statusHeight)
	return Dimensions{Width: w, Height: h}
}

func (l Layout) Content() Dimensions {
	navW := l.Nav().Width
	w := l.termW - navW
	h := max(1, l.termH-headerHeight-statusHeight)
	return Dimensions{Width: w, Height: h}
}

// TooSmall reports whether the terminal is below the minimum usable size.
func (l Layout) TooSmall() bool {
	return l.termW < MinTermWidth || l.termH < MinTermHeight
}

// TermSize returns the current terminal dimensions.
func (l Layout) TermSize() (w, h int) {
	return l.termW, l.termH
}

func (l Layout) Header() Dimensions {
	return Dimensions{Width: l.termW, Height: headerHeight}
}

func (l Layout) Status() Dimensions {
	return Dimensions{Width: l.termW, Height: statusHeight}
}

// InnerSize returns the usable inner dimensions of a bordered panel.
func InnerSize(d Dimensions) (w, h int) {
	return max(d.Width-borderPadding, 0), max(d.Height-borderPadding, 0)
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
