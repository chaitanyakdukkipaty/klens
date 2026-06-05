package panels

import (
	"testing"

	"charm.land/lipgloss/v2"
)

func testHeader() Header {
	return NewHeader(100).
		SetCluster("prod-cluster").
		SetNamespace("default").
		SetVersion("v1.29.3")
}

func TestHeaderChipAtMatchesSegments(t *testing.T) {
	h := testHeader()
	found := map[HeaderChip]bool{}
	for _, seg := range h.segments() {
		if seg.chip == ChipNone {
			continue
		}
		found[seg.chip] = true
		for _, x := range []int{seg.x, seg.x + lipgloss.Width(seg.text) - 1} {
			chip, ok := h.ChipAt(x)
			if !ok || chip != seg.chip {
				t.Errorf("ChipAt(%d) = (%v,%v), want (%v,true)", x, chip, ok, seg.chip)
			}
		}
	}
	for _, want := range []HeaderChip{ChipCluster, ChipNamespace, ChipMenu} {
		if !found[want] {
			t.Errorf("chip %v missing from segments at width 100", want)
		}
	}
}

func TestHeaderChipAtMissesInertText(t *testing.T) {
	h := testHeader()
	if chip, ok := h.ChipAt(0); ok {
		t.Errorf("ChipAt(0) = %v inside left padding", chip)
	}
	// The gap between cluster and namespace chips is inert.
	segs := h.segments()
	cluster := segs[0]
	gapX := cluster.x + lipgloss.Width(cluster.text)
	if chip, ok := h.ChipAt(gapX); ok {
		t.Errorf("ChipAt(%d) = %v inside the chip gap", gapX, chip)
	}
}

func TestHeaderNarrowDropsRightSide(t *testing.T) {
	h := testHeader().SetWidth(30)
	for _, seg := range h.segments() {
		if seg.chip == ChipMenu {
			t.Fatal("menu chip present at width 30; right side should collapse")
		}
	}
	// Cluster/namespace chips must survive.
	if _, ok := h.ChipAt(1); !ok {
		t.Error("cluster chip missing at narrow width")
	}
}

func TestHeaderViewSingleRow(t *testing.T) {
	for _, w := range []int{40, 80, 120} {
		v := testHeader().SetWidth(w).SetReadOnly(true).View()
		if got := lipgloss.Height(v); got != 1 {
			t.Errorf("width %d: View height = %d, want 1", w, got)
		}
	}
}
