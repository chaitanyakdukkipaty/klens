package panels

import (
	"testing"

	"charm.land/lipgloss/v2"
)

func testBar() TabBar {
	return TabBar{
		Width:  80,
		Active: TabTable,
		Enabled: map[TabID]bool{
			TabYAML:     true,
			TabDescribe: true,
			TabLogs:     true,
			// XRay / Metrics absent → disabled
		},
		CanFullscreen: false,
	}
}

func TestTabAtHitsEverySegment(t *testing.T) {
	tb := testBar()
	for _, seg := range tb.segments() {
		for _, x := range []int{seg.x, seg.x + seg.w - 1} {
			id, ok := tb.TabAt(x)
			if !ok || id != seg.id {
				t.Errorf("TabAt(%d) = (%v,%v), want (%v,true)", x, id, ok, seg.id)
			}
		}
		// Gap after the segment must not hit it.
		if id, ok := tb.TabAt(seg.x + seg.w); ok && id == seg.id {
			t.Errorf("TabAt(%d) still hits %v inside the gap", seg.x+seg.w, seg.id)
		}
	}
	if _, ok := tb.TabAt(0); ok {
		t.Error("TabAt(0) hit a tab inside the left pad")
	}
}

func TestFullscreenButton(t *testing.T) {
	tb := testBar()
	tb.CanFullscreen = true
	bx := tb.fsButtonX()
	if bx == -1 {
		t.Fatal("fsButtonX = -1 with CanFullscreen and ample width")
	}
	if id, ok := tb.TabAt(bx); !ok || id != TabFullscreen {
		t.Errorf("TabAt(fsButtonX) = (%v,%v), want (TabFullscreen,true)", id, ok)
	}
	tb.CanFullscreen = false
	if tb.fsButtonX() != -1 {
		t.Error("fsButtonX present with CanFullscreen=false")
	}
	// Too narrow to fit the button after the tabs → hidden.
	tb.CanFullscreen = true
	tb.Width = 30
	if tb.fsButtonX() != -1 {
		t.Error("fsButtonX present when strip overflows the width")
	}
}

func TestEnabledGating(t *testing.T) {
	tb := testBar()
	if !tb.IsEnabled(TabTable) {
		t.Error("TabTable must always be enabled")
	}
	if tb.IsEnabled(TabMetrics) {
		t.Error("TabMetrics enabled despite absent capability")
	}
	if !tb.IsEnabled(TabLogs) {
		t.Error("TabLogs disabled despite capability")
	}
	tb.Enabled = nil
	if !tb.IsEnabled(TabMetrics) {
		t.Error("nil Enabled map must enable everything")
	}
}

func TestEditingBadgeWidensYAMLTab(t *testing.T) {
	plain := testBar()
	editing := testBar()
	editing.Editing = true
	var pw, ew int
	for _, s := range plain.segments() {
		if s.id == TabYAML {
			pw = s.w
		}
	}
	for _, s := range editing.segments() {
		if s.id == TabYAML {
			ew = s.w
		}
	}
	if ew <= pw {
		t.Errorf("editing badge did not widen YAML tab: %d <= %d", ew, pw)
	}
}

func TestViewIsSingleRowAtWidth(t *testing.T) {
	tb := testBar()
	tb.CanFullscreen = true
	v := tb.View()
	if h := lipgloss.Height(v); h != 1 {
		t.Errorf("View height = %d, want 1", h)
	}
	if w := lipgloss.Width(v); w != tb.Width {
		t.Errorf("View width = %d, want %d", w, tb.Width)
	}
}
