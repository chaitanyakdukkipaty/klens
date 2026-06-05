package panels

import "testing"

// Hover and keyboard drive one cursor: SetCursorVisible moves the highlight
// without re-anchoring the scroll window.
func TestSetCursorVisibleKeepsWindowStill(t *testing.T) {
	tb := tableWithRows(100)
	// Scroll deep via the scrollbar so the window sits at [40, 40+vis).
	sbX, topY, height, _, thumbSize, _ := tb.scrollbarGeometry()
	_ = sbX
	tb, _ = tb.HandleScrollbarDown(sbX, topY+ (height-thumbSize)/2)
	tb, _ = tb.HandleScrollbarUp()
	start := tb.scrollStart()
	if start == 0 {
		t.Fatal("setup: window did not scroll")
	}
	// Hover a row in the middle of the window: cursor moves, window doesn't.
	mid := start + tb.visibleRowCount()/2
	tb = tb.SetCursorVisible(mid)
	if tb.cursor != mid {
		t.Errorf("cursor = %d, want %d", tb.cursor, mid)
	}
	if got := tb.scrollStart(); got != start {
		t.Errorf("window moved on hover: scrollStart %d → %d", start, got)
	}
	// Keyboard from there still scrolls when crossing the window edge.
	for i := 0; i < tb.visibleRowCount(); i++ {
		tb, _ = tb.Update(keyMsg("down"))
	}
	if tb.cursor < tb.scrollStart() || tb.cursor >= tb.scrollStart()+tb.visibleRowCount() {
		t.Errorf("cursor %d left window [%d,%d)", tb.cursor, tb.scrollStart(), tb.scrollStart()+tb.visibleRowCount())
	}
}

// Out-of-range hover indices are ignored.
func TestSetCursorVisibleBounds(t *testing.T) {
	tb := tableWithRows(5)
	before := tb.cursor
	tb = tb.SetCursorVisible(-1)
	tb = tb.SetCursorVisible(99)
	if tb.cursor != before {
		t.Errorf("cursor moved on out-of-range hover: %d", tb.cursor)
	}
}
