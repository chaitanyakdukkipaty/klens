package termsession

// Scrollback browsing and text selection for an embedded session. This is the
// view layer over the emulator: it never touches the SPDY stream, only the
// in-process screen + scrollback buffer.
//
// Coordinate system: a "global" line index G runs from the oldest scrollback
// line (G=0) through the live screen. With sbLen scrollback lines and a screen
// of height h, G in [0, sbLen) addresses scrollback line G and G in
// [sbLen, sbLen+h) addresses live screen row G-sbLen. Scrollback only grows at
// the end (front lines drop only once the 10k cap overflows), so a G that
// points into scrollback is stable across new output — that is what lets a
// frozen viewport stay anchored while the shell keeps printing.
//
// A session starts "following": pinned to the live bottom, identical to the
// pre-scrollback behaviour. Scrolling up clears following and freezes scrollTop
// at a global index; reaching the bottom (or esc) re-pins it.

import (
	"image/color"
	"strings"

	uv "github.com/charmbracelet/ultraviolet"
)

// selection is a 2-D text selection in global coordinates. Endpoints are stored
// as-dragged (anchor → head); ordering happens at read time.
type selection struct {
	active bool // mouse button held; selection in progress
	moved  bool // head moved away from the anchor since Begin
	set    bool // a completed (copyable / highlightable) selection exists
	aG, aX int  // anchor: global line, column
	hG, hX int  // head: global line, column
}

// AltScreen reports whether the remote app has switched to the alternate
// screen (vim, less, htop, …). Wheel events forward to the app there instead
// of driving local scrollback, which the alt screen doesn't have.
func (s *Session) AltScreen() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.emu.IsAltScreen()
}

// dimsLocked returns screen width, screen height and scrollback length.
func (s *Session) dimsLocked() (w, h, sbLen int) {
	b := s.emu.Bounds()
	return b.Dx(), b.Dy(), s.emu.ScrollbackLen()
}

// topLocked is the global index of the top visible row for the current state.
func (s *Session) topLocked(sbLen int) int {
	if s.following {
		return sbLen
	}
	return clampi(s.scrollTop, 0, sbLen)
}

// Following reports whether the viewport is pinned to the live bottom.
func (s *Session) Following() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.following
}

// ScrollBy moves the viewport by delta lines (delta>0 scrolls up, toward older
// output). Crossing the live bottom re-pins to following.
func (s *Session) ScrollBy(delta int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, _, sbLen := s.dimsLocked()
	cur := s.topLocked(sbLen) - delta
	if cur >= sbLen {
		s.following = true
		s.scrollTop = sbLen
		return
	}
	if cur < 0 {
		cur = 0
	}
	s.following = false
	s.scrollTop = cur
}

// ScrollHome jumps to the oldest scrollback line.
func (s *Session) ScrollHome() {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, _, sbLen := s.dimsLocked()
	if sbLen == 0 {
		s.following = true
		return
	}
	s.following = false
	s.scrollTop = 0
}

// ScrollEnd re-pins to the live bottom.
func (s *Session) ScrollEnd() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.following = true
}

// ScrollInfo reports the viewport top, total line count and screen height for
// the scroll indicator. position is the 1-based line of the top visible row.
func (s *Session) ScrollInfo() (position, total int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, h, sbLen := s.dimsLocked()
	top := s.topLocked(sbLen)
	return top + 1, sbLen + h
}

// bodyRow clamps a viewport row into [0,h) and returns its global index.
func (s *Session) globalAt(row, h, sbLen int) int {
	if row < 0 {
		row = 0
	}
	if row >= h {
		row = h - 1
	}
	return s.topLocked(sbLen) + row
}

// SelectStart begins a selection anchored at the given body-local cell. It does
// not change the follow state — a plain click (no drag) leaves the live view
// untouched; only wheel/keys enter scroll mode.
func (s *Session) SelectStart(col, row int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	w, h, sbLen := s.dimsLocked()
	g := s.globalAt(row, h, sbLen)
	col = clampi(col, 0, max(w-1, 0))
	s.sel = selection{active: true, aG: g, aX: col, hG: g, hX: col}
}

// SelectExtend moves the selection head to the given body-local cell. Returns
// whether the head moved (so the caller can mark the drag as real).
func (s *Session) SelectExtend(col, row int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.sel.active {
		return false
	}
	w, h, sbLen := s.dimsLocked()
	g := s.globalAt(row, h, sbLen)
	col = clampi(col, 0, max(w-1, 0))
	if g == s.sel.hG && col == s.sel.hX {
		return false
	}
	s.sel.moved = true
	s.sel.hG = g
	s.sel.hX = col
	return true
}

// SelectEnd finishes the drag. A selection that never moved is discarded (a
// plain click clears, like the other panels); a real drag is retained for
// rendering and copy. Returns whether a copyable selection exists.
func (s *Session) SelectEnd() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sel.active = false
	s.sel.set = s.sel.moved
	return s.sel.set
}

// HasSelection reports whether a completed selection is available to copy.
func (s *Session) HasSelection() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sel.set
}

// Selecting reports whether a drag selection is currently in progress.
func (s *Session) Selecting() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sel.active
}

// ClearSelection drops any selection (active or completed).
func (s *Session) ClearSelection() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sel = selection{}
}

// ResetView returns to the live bottom and clears any selection — the esc /
// "resume" action out of scroll mode.
func (s *Session) ResetView() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.following = true
	s.sel = selection{}
}

// orderSel returns the selection endpoints ordered by (line, column).
func orderSel(sel selection) (loG, loX, hiG, hiX int) {
	loG, loX, hiG, hiX = sel.aG, sel.aX, sel.hG, sel.hX
	if hiG < loG || (hiG == loG && hiX < loX) {
		loG, loX, hiG, hiX = hiG, hiX, loG, loX
	}
	return
}

// selRange returns the inclusive column span [c0,c1] selected on global line g,
// or ok=false when g is outside the selection. w is the screen width.
func selRange(g, loG, loX, hiG, hiX, w int) (c0, c1 int, ok bool) {
	if g < loG || g > hiG {
		return 0, 0, false
	}
	switch {
	case loG == hiG:
		c0, c1 = loX, hiX
		if c0 > c1 {
			c0, c1 = c1, c0
		}
	case g == loG:
		c0, c1 = loX, w-1
	case g == hiG:
		c0, c1 = 0, hiX
	default:
		c0, c1 = 0, w-1
	}
	return clampi(c0, 0, max(w-1, 0)), clampi(c1, 0, max(w-1, 0)), true
}

// lineAtLocked materialises global line g as a width-w line of cloned cells,
// padded with empty cells. Out-of-range lines come back all-empty.
func (s *Session) lineAtLocked(g, w, h, sbLen int) uv.Line {
	line := make(uv.Line, w)
	for i := range line {
		line[i] = uv.EmptyCell
	}
	switch {
	case g < 0:
		return line
	case g < sbLen:
		src := s.emu.Scrollback().Line(g)
		for x := 0; x < w && x < len(src); x++ {
			line[x] = src[x]
		}
	default:
		y := g - sbLen
		if y < 0 || y >= h {
			return line
		}
		for x := 0; x < w; x++ {
			if c := s.emu.CellAt(x, y); c != nil {
				line[x] = *c
			}
		}
	}
	return line
}

// RenderView snapshots the visible window. When following the live bottom with
// no selection it falls back to the emulator's fast path (unchanged behaviour);
// otherwise it renders the windowed view with the selection highlighted using
// selBg as the background.
func (s *Session) RenderView(selBg color.Color) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	w, h, sbLen := s.dimsLocked()
	if w < 1 || h < 1 {
		return ""
	}
	hasSel := s.sel.active || s.sel.set
	if s.following && !hasSel {
		return s.emu.Render()
	}
	loG, loX, hiG, hiX := orderSel(s.sel)
	top := s.topLocked(sbLen)
	lines := make([]string, 0, h)
	for r := 0; r < h; r++ {
		line := s.lineAtLocked(top+r, w, h, sbLen)
		if hasSel {
			if c0, c1, ok := selRange(top+r, loG, loX, hiG, hiX, w); ok {
				applySelBg(line, c0, c1, selBg)
			}
		}
		lines = append(lines, line.Render())
	}
	return strings.Join(lines, "\n")
}

// SelectionText returns the selected text, one line per global row, trailing
// spaces trimmed. Empty when no selection exists.
func (s *Session) SelectionText() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.sel.set && !s.sel.active {
		return ""
	}
	w, h, sbLen := s.dimsLocked()
	loG, loX, hiG, hiX := orderSel(s.sel)
	var b strings.Builder
	for g := loG; g <= hiG; g++ {
		c0, c1, ok := selRange(g, loG, loX, hiG, hiX, w)
		if !ok {
			continue
		}
		line := s.lineAtLocked(g, w, h, sbLen)
		b.WriteString(strings.TrimRight(lineText(line, c0, c1), " "))
		if g < hiG {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// lineText extracts the plain content of cells [c0,c1] inclusive.
func lineText(line uv.Line, c0, c1 int) string {
	var b strings.Builder
	for x := c0; x <= c1 && x < len(line); x++ {
		c := &line[x]
		if c.IsZero() {
			continue
		}
		if c.Content == "" {
			b.WriteByte(' ')
			continue
		}
		b.WriteString(c.Content)
	}
	return b.String()
}

// applySelBg paints cells [c0,c1] inclusive with the selection background,
// materialising empty cells as styled spaces so the highlight is visible.
func applySelBg(line uv.Line, c0, c1 int, bg color.Color) {
	for x := c0; x <= c1 && x < len(line); x++ {
		c := &line[x]
		if c.IsZero() || c.Content == "" {
			c.Content = " "
			c.Width = 1
		}
		c.Style.Bg = bg
	}
}

func clampi(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
