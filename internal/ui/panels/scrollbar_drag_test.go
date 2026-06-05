package panels

import (
	"fmt"
	"testing"

	k8sres "github.com/chaitanyak/klens/internal/k8s"
)

// tableWithRows builds a Pod table sized so ~17 rows are visible out of n.
func tableWithRows(n int) ResourceTable {
	t := NewResourceTable(80, 22).SetKind("Pod")
	rows := make([]k8sres.ResourceRow, n)
	for i := range rows {
		rows[i] = k8sres.ResourceRow{Name: fmt.Sprintf("pod-%03d", i), Status: "Running"}
	}
	return t.WithRows(rows)
}

func TestScrollbarGeometryMirrorsView(t *testing.T) {
	tb := tableWithRows(100)
	sbX, topY, height, thumbPos, thumbSize, ok := tb.scrollbarGeometry()
	if !ok {
		t.Fatal("no scrollbar geometry for 100 rows")
	}
	// 80 wide → inner 78, data 77, scrollbar at outer X 78.
	if sbX != 78 {
		t.Errorf("sbX = %d, want 78", sbX)
	}
	if topY != 2 {
		t.Errorf("topY = %d, want 2 (title+header, no filter)", topY)
	}
	if height != tb.visibleRowCount() {
		t.Errorf("height = %d, want %d", height, tb.visibleRowCount())
	}
	if thumbPos != 0 {
		t.Errorf("thumbPos = %d at offset 0", thumbPos)
	}
	if thumbSize < 1 || thumbSize >= height {
		t.Errorf("thumbSize = %d out of range (height %d)", thumbSize, height)
	}
}

func TestScrollbarThumbDragScrolls(t *testing.T) {
	tb := tableWithRows(100)
	sbX, topY, height, _, thumbSize, _ := tb.scrollbarGeometry()

	// Grab the thumb at its top cell.
	tb, hit := tb.HandleScrollbarDown(sbX, topY)
	if !hit || !tb.ScrollbarDragging() {
		t.Fatal("thumb grab not registered")
	}
	// Drag to the bottom of the track → cursor at the last row.
	tb = tb.HandleScrollbarDrag(topY + height - thumbSize)
	if tb.cursor != 99 {
		t.Errorf("cursor after drag to bottom = %d, want 99", tb.cursor)
	}
	// Drag back to the top → first window (cursor at its bottom row).
	tb = tb.HandleScrollbarDrag(topY)
	if got := tb.scrollStart(); got != 0 {
		t.Errorf("scrollStart after drag to top = %d, want 0", got)
	}
	tb, was := tb.HandleScrollbarUp()
	if !was || tb.ScrollbarDragging() {
		t.Error("release did not end the drag")
	}
}

func TestScrollbarTrackClickJumps(t *testing.T) {
	tb := tableWithRows(100)
	sbX, topY, height, _, _, _ := tb.scrollbarGeometry()
	// Click the last track cell → window jumps near the end.
	tb, hit := tb.HandleScrollbarDown(sbX, topY+height-1)
	if !hit {
		t.Fatal("track click not claimed")
	}
	if tb.scrollStart() < 50 {
		t.Errorf("scrollStart after bottom track click = %d, want near end", tb.scrollStart())
	}
}

func TestScrollbarMissesDataColumns(t *testing.T) {
	tb := tableWithRows(100)
	_, topY, _, _, _, _ := tb.scrollbarGeometry()
	if _, hit := tb.HandleScrollbarDown(10, topY); hit {
		t.Error("click on data column claimed by scrollbar")
	}
}

func TestScrollbarNoDragWhenAllRowsFit(t *testing.T) {
	tb := tableWithRows(5)
	sbX, topY, _, _, _, ok := tb.scrollbarGeometry()
	if !ok {
		t.Fatal("geometry absent for short table")
	}
	tb, hit := tb.HandleScrollbarDown(sbX, topY)
	if !hit {
		t.Error("scrollbar click not claimed (should claim to block drag-select)")
	}
	if tb.ScrollbarDragging() {
		t.Error("drag started with a full-track thumb")
	}
}
