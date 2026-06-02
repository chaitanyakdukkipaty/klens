package panels

import (
	"testing"

	k8sres "github.com/chaitanyak/klens/internal/k8s"
)

// resolvableKind returns a registered kind whose descriptor has at least two
// columns, so the column-mapping tests have boundaries to probe. Skips the
// test if the registry somehow has no such kind (it always does in practice).
func resolvableKind(t *testing.T) (string, k8sres.ResourceDescriptor) {
	t.Helper()
	for _, name := range []string{"Pod", "Deployment", "Service", "Node"} {
		if desc, ok := k8sres.Resolve(name); ok && len(desc.Columns) >= 2 {
			return name, desc
		}
	}
	t.Skip("no registered kind with >=2 columns")
	return "", k8sres.ResourceDescriptor{}
}

func TestColumnAtXMapsHeaderClicksToColumns(t *testing.T) {
	kind, desc := resolvableKind(t)
	const width = 120
	tbl := NewResourceTable(width, 30).SetKind(kind)

	innerW := max(1, width-2)
	dataW := max(1, innerW-1)
	colWidths := computeColWidths(desc.Columns, dataW)

	// Walk each column and confirm its first and last rendered cell map back
	// to the column index. acc tracks the start cell (post-offset) of column i.
	acc := 0
	for i, w := range colWidths {
		if w <= 0 {
			acc += w + 1
			continue
		}
		first := headerColTextOffset + acc
		last := headerColTextOffset + acc + w - 1
		if got, ok := tbl.columnAtX(first); !ok || got != i {
			t.Errorf("col %d first cell x=%d → (%d, %v), want (%d, true)", i, first, got, ok, i)
		}
		if got, ok := tbl.columnAtX(last); !ok || got != i {
			t.Errorf("col %d last cell x=%d → (%d, %v), want (%d, true)", i, last, got, ok, i)
		}
		acc += w + 1
	}
}

func TestColumnAtXRejectsBorderAndPadding(t *testing.T) {
	kind, _ := resolvableKind(t)
	tbl := NewResourceTable(120, 30).SetKind(kind)

	// innerX 0 is the panel's left border, 1 is TableHeader's left padding —
	// neither maps to a column.
	for _, x := range []int{0, 1, -3} {
		if _, ok := tbl.columnAtX(x); ok {
			t.Errorf("columnAtX(%d) = ok, want rejected (border/padding)", x)
		}
	}
}

func TestColumnAtXRejectsPastLastColumn(t *testing.T) {
	kind, _ := resolvableKind(t)
	tbl := NewResourceTable(120, 30).SetKind(kind)

	// Far past the rightmost column (into the reserved scrollbar / empty area).
	if _, ok := tbl.columnAtX(10_000); ok {
		t.Error("columnAtX past last column = ok, want rejected")
	}
}

func TestHandleHeaderClickTogglesDirection(t *testing.T) {
	kind, _ := resolvableKind(t)
	tbl := NewResourceTable(120, 30).SetKind(kind)
	headerY := tbl.headerRowInnerY()

	// Click squarely on the first column's first cell.
	x := headerColTextOffset
	tbl, ok := tbl.HandleHeaderClickAt(x, headerY)
	if !ok {
		t.Fatalf("HandleHeaderClickAt(%d,%d) = not handled, want handled", x, headerY)
	}
	if tbl.sortColIdx != 0 || !tbl.sortAsc {
		t.Fatalf("after first click: sortColIdx=%d asc=%v, want 0 asc=true", tbl.sortColIdx, tbl.sortAsc)
	}
	// Second click on the same column flips direction to descending.
	tbl, ok = tbl.HandleHeaderClickAt(x, headerY)
	if !ok || tbl.sortColIdx != 0 || tbl.sortAsc {
		t.Fatalf("after second click: ok=%v sortColIdx=%d asc=%v, want ok=true 0 desc", ok, tbl.sortColIdx, tbl.sortAsc)
	}
}

func TestHandleHeaderClickIgnoresNonHeaderRow(t *testing.T) {
	kind, _ := resolvableKind(t)
	tbl := NewResourceTable(120, 30).SetKind(kind)

	// A click on a data row (one below the header) must not be treated as a
	// sort — it falls through to row selection in the caller.
	if _, ok := tbl.HandleHeaderClickAt(headerColTextOffset, tbl.headerRowInnerY()+1); ok {
		t.Error("HandleHeaderClickAt on data row = handled, want ignored")
	}
}
