package panels

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

func dockFixture() TerminalDock {
	return TerminalDock{
		Width:  80,
		Height: 12,
		Tabs: []DockTab{
			{Title: "api-7f9c"},
			{Title: "worker-x4", Exited: true},
			{Title: "a-very-long-pod-name-that-overflows-the-tab"},
		},
		Active: 0,
		Body:   "line1\nline2",
	}
}

func TestDockBodySize(t *testing.T) {
	w, h := DockBodySize(80, 12)
	if w != 78 || h != 9 { // border 2 cols; border 2 + tab bar 1 rows
		t.Fatalf("DockBodySize = %d×%d, want 78×9", w, h)
	}
	if w, h := DockBodySize(1, 2); w != 0 || h != 0 {
		t.Fatalf("degenerate DockBodySize = %d×%d, want 0×0", w, h)
	}
}

func TestDockViewExactDimensions(t *testing.T) {
	d := dockFixture()
	out := d.View()
	lines := strings.Split(out, "\n")
	if len(lines) != d.Height {
		t.Fatalf("View has %d lines, want %d", len(lines), d.Height)
	}
	for i, ln := range lines {
		if w := lipgloss.Width(ln); w != d.Width {
			t.Fatalf("line %d width = %d, want %d", i, w, d.Width)
		}
	}
}

// The scroll indicator appears in the tab bar without breaking exact-width
// rendering (it must fit between the tabs and the maximize toggle).
func TestDockScrollIndicator(t *testing.T) {
	d := dockFixture()
	d.Scroll = "▲ 120/3000"
	out := d.View()
	if !strings.Contains(out, "120/3000") {
		t.Fatalf("scroll indicator missing from view:\n%s", out)
	}
	for i, ln := range strings.Split(out, "\n") {
		if w := lipgloss.Width(ln); w != d.Width {
			t.Fatalf("line %d width = %d, want %d", i, w, d.Width)
		}
	}
}

// Render and hit-test must agree: every segment's reported extent resolves
// back to its own tab, and the close mark cell reports close=true.
func TestDockTabAtMatchesSegments(t *testing.T) {
	d := dockFixture()
	for _, seg := range d.segments() {
		idx, isClose, ok := d.TabAt(seg.x)
		if !ok || idx != seg.idx || isClose {
			t.Fatalf("TabAt(start=%d) = (%d,%v,%v), want (%d,false,true)", seg.x, idx, isClose, ok, seg.idx)
		}
		idx, isClose, ok = d.TabAt(seg.closeX)
		if !ok || idx != seg.idx || !isClose {
			t.Fatalf("TabAt(close=%d) = (%d,%v,%v), want (%d,true,true)", seg.closeX, idx, isClose, ok, seg.idx)
		}
	}
	if _, _, ok := d.TabAt(0); ok {
		t.Fatal("TabAt(0) hit a tab, want miss (leading pad)")
	}
}

func TestDockTabTruncation(t *testing.T) {
	d := dockFixture()
	segs := d.segments()
	if got := segs[2].w; got > dockTabMaxW+2 { // label cap + " ×"
		t.Fatalf("overlong tab width = %d, want ≤ %d", got, dockTabMaxW+2)
	}
}

func TestDockMaxButton(t *testing.T) {
	d := dockFixture()
	bx := d.maxButtonX()
	if bx == -1 {
		t.Fatal("maxButtonX = -1, want a button at width 80")
	}
	if !d.MaxButtonAt(bx) {
		t.Fatalf("MaxButtonAt(%d) = false, want true", bx)
	}
	if d.MaxButtonAt(bx - 2) {
		t.Fatalf("MaxButtonAt(%d) = true, want false", bx-2)
	}
}
