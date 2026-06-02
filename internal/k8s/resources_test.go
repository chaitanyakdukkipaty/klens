package k8s

import (
	"sort"
	"testing"
)

func TestRowLessNaturalOrder(t *testing.T) {
	rows := []ResourceRow{
		{Name: "pod-10"}, {Name: "pod-2"}, {Name: "pod-1"},
	}
	sort.Slice(rows, func(i, j int) bool { return RowLess(rows[i], rows[j]) })
	got := []string{rows[0].Name, rows[1].Name, rows[2].Name}
	want := []string{"pod-1", "pod-2", "pod-10"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("natural order = %v, want %v", got, want)
		}
	}
}

func TestRowLessNamespacePrimary(t *testing.T) {
	a := ResourceRow{Namespace: "alpha", Name: "z"}
	b := ResourceRow{Namespace: "beta", Name: "a"}
	if !RowLess(a, b) {
		t.Fatal("namespace should be the primary sort key")
	}
}

func TestRowLessTotalOrder(t *testing.T) {
	a := ResourceRow{Namespace: "ns", Name: "x"}
	b := ResourceRow{Namespace: "ns", Name: "x"}
	if RowLess(a, b) || RowLess(b, a) {
		t.Fatal("identical rows must compare equal (neither less)")
	}
}

func TestNaturalLessLeadingZeros(t *testing.T) {
	if !naturalLess("v007", "v8") {
		t.Fatal("leading zeros should not change numeric value: v007 < v8")
	}
}

func TestCellCompareNumber(t *testing.T) {
	if CellCompare(SortNumber, "9", "10") != -1 {
		t.Fatal("9 should sort before 10 numerically, not lexicographically")
	}
	if CellCompare(SortNumber, "1,000", "999") != 1 {
		t.Fatal("thousands separators should be ignored: 1000 > 999")
	}
}

func TestCellCompareTime(t *testing.T) {
	// Smaller duration = younger; younger sorts first ascending.
	if CellCompare(SortTime, "30s", "5m") != -1 {
		t.Fatal("30s should sort before 5m")
	}
	if CellCompare(SortTime, "2d", "47h") != 1 {
		t.Fatal("2d (48h) should sort after 47h")
	}
	if CellCompare(SortTime, "5d", "n/a") != -1 {
		t.Fatal("n/a should sort last")
	}
}

func TestCellCompareCapacity(t *testing.T) {
	if CellCompare(SortCapacity, "512Mi", "1Gi") != -1 {
		t.Fatal("512Mi should sort before 1Gi by byte value")
	}
	if CellCompare(SortCapacity, "1Gi", "1024Mi") != 0 {
		t.Fatal("1Gi and 1024Mi are equal by byte value")
	}
}

func TestCellCompareStripsANSI(t *testing.T) {
	styled := "\x1b[31m45\x1b[0m" // red "45"
	if CellCompare(SortNumber, styled, "9") != 1 {
		t.Fatal("ANSI styling must be stripped before numeric compare")
	}
}
