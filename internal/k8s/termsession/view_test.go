package termsession

import (
	"image/color"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// newTestSession builds a session backed only by an in-process emulator (no
// SPDY stream) and feeds it `lines` rows of "lineN" text so scrollback fills.
// The emulator is 10×3, so writing 8 lines leaves 6 in scrollback.
func newTestSession(t *testing.T, lines int) *Session {
	t.Helper()
	ch := make(chan tea.Msg, 16)
	s := New(nil, nil, "ns", "pod", "", 10, 3, ch)
	t.Cleanup(s.Close)
	for i := 0; i < lines; i++ {
		s.emu.WriteString("line" + string(rune('0'+i)) + "\r\n")
	}
	return s
}

func TestScrollByEntersAndExitsFollowing(t *testing.T) {
	s := newTestSession(t, 8) // sbLen=6, h=3
	if !s.Following() {
		t.Fatal("new session should follow the live bottom")
	}
	s.ScrollBy(2)
	if s.Following() {
		t.Fatal("ScrollBy(2) should leave following")
	}
	pos, total := s.ScrollInfo()
	if total != 9 { // sbLen(6) + h(3)
		t.Fatalf("total = %d, want 9", total)
	}
	if pos != 5 { // top global index 4, 1-based
		t.Fatalf("pos = %d, want 5", pos)
	}
	// Scrolling back down past the bottom re-pins to following.
	s.ScrollBy(-5)
	if !s.Following() {
		t.Fatal("scrolling past the bottom should re-pin following")
	}
}

func TestScrollHomeEnd(t *testing.T) {
	s := newTestSession(t, 8)
	s.ScrollHome()
	if s.Following() {
		t.Fatal("ScrollHome should leave following")
	}
	if pos, _ := s.ScrollInfo(); pos != 1 {
		t.Fatalf("ScrollHome pos = %d, want 1", pos)
	}
	s.ScrollEnd()
	if !s.Following() {
		t.Fatal("ScrollEnd should re-pin following")
	}
}

func TestRenderViewShowsScrolledContent(t *testing.T) {
	s := newTestSession(t, 8)
	// Live view shows the bottom (line6, line7); older lines are off-screen.
	live := s.RenderView(nil)
	if strings.Contains(live, "line4") {
		t.Fatalf("live view should not contain line4:\n%s", live)
	}
	s.ScrollBy(2) // top -> global 4: line4, line5, line6
	scrolled := s.RenderView(nil)
	for _, want := range []string{"line4", "line5", "line6"} {
		if !strings.Contains(scrolled, want) {
			t.Fatalf("scrolled view missing %q:\n%s", want, scrolled)
		}
	}
	if got := strings.Count(scrolled, "\n"); got != 2 { // 3 rows
		t.Fatalf("scrolled view has %d newlines, want 2:\n%s", got, scrolled)
	}
}

func TestSelectionTextAcrossScrollback(t *testing.T) {
	s := newTestSession(t, 8)
	s.ScrollBy(2) // viewport rows: line4, line5, line6
	s.SelectStart(0, 0)
	if !s.Selecting() {
		t.Fatal("SelectStart should mark the session selecting")
	}
	// Drag from start of row 0 to end of row 1 -> "line4\nline5".
	s.SelectExtend(4, 1)
	if !s.SelectEnd() {
		t.Fatal("a moved selection should be copyable")
	}
	if got, want := s.SelectionText(), "line4\nline5"; got != want {
		t.Fatalf("SelectionText = %q, want %q", got, want)
	}
}

func TestSelectionSingleLineRange(t *testing.T) {
	s := newTestSession(t, 8)
	s.ScrollBy(2)
	s.SelectStart(0, 0)
	s.SelectExtend(3, 0) // cols 0..3 of "line4" -> "line"
	s.SelectEnd()
	if got, want := s.SelectionText(), "line"; got != want {
		t.Fatalf("SelectionText = %q, want %q", got, want)
	}
}

func TestPlainClickDiscardsSelection(t *testing.T) {
	s := newTestSession(t, 8)
	s.SelectStart(2, 0)
	if s.SelectEnd() { // no Extend -> not moved -> discarded
		t.Fatal("a click that never moved should not be copyable")
	}
	if s.HasSelection() {
		t.Fatal("HasSelection should be false after a plain click")
	}
}

func TestResetViewClearsState(t *testing.T) {
	s := newTestSession(t, 8)
	s.ScrollBy(2)
	s.SelectStart(0, 0)
	s.SelectExtend(4, 0)
	s.SelectEnd()
	s.ResetView()
	if !s.Following() {
		t.Fatal("ResetView should re-pin following")
	}
	if s.HasSelection() {
		t.Fatal("ResetView should clear the selection")
	}
}

func TestRenderViewSelectionHighlight(t *testing.T) {
	s := newTestSession(t, 8)
	s.ScrollBy(2)
	s.SelectStart(0, 0)
	s.SelectExtend(4, 0)
	s.SelectEnd()
	red := color.RGBA{R: 0xff, A: 0xff}
	out := s.RenderView(red)
	// A non-following / selected render must emit SGR sequences for the
	// highlight (the fast path returns plain emulator output).
	if !strings.Contains(out, "\x1b[") {
		t.Fatalf("expected ANSI styling in highlighted render:\n%q", out)
	}
}

func TestOrderSelAndRange(t *testing.T) {
	// Reversed drag (head before anchor) should normalise.
	loG, loX, hiG, hiX := orderSel(selection{aG: 5, aX: 3, hG: 2, hX: 1})
	if loG != 2 || loX != 1 || hiG != 5 || hiX != 3 {
		t.Fatalf("orderSel = %d,%d,%d,%d", loG, loX, hiG, hiX)
	}
	// Middle line of a multi-line selection spans the full width.
	c0, c1, ok := selRange(3, 2, 4, 5, 1, 10)
	if !ok || c0 != 0 || c1 != 9 {
		t.Fatalf("middle selRange = %d,%d,%v", c0, c1, ok)
	}
	// A line outside the selection reports ok=false.
	if _, _, ok := selRange(9, 2, 0, 5, 0, 10); ok {
		t.Fatal("line outside selection should report ok=false")
	}
}
