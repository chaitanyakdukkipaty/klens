package klenstests

import (
	"regexp"
	"strings"
	"testing"

	k8sres "github.com/chaitanyak/klens/internal/k8s"
	_ "github.com/chaitanyak/klens/internal/k8s/kinds" // register Event descriptor
	"github.com/chaitanyak/klens/internal/ui/panels"
)

var ansiRE = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripANSI(s string) string { return ansiRE.ReplaceAllString(s, "") }

// makeWrappableEventRows fabricates Event rows with a very long MESSAGE so
// the wrap branch has something to wrap. Values are positional against the
// Event kind's columns: [LAST SEEN, TYPE, REASON, OBJECT, MESSAGE].
func makeWrappableEventRows() []k8sres.ResourceRow {
	long := "this is an exceptionally long event message that has to wrap across multiple terminal lines so the wrap test can observe the multi-line rendering kicking in"
	return []k8sres.ResourceRow{
		{
			Name:   "evt-1",
			Status: "Warning",
			Values: []string{"5m", "Warning", "BackOff", "Pod/foo", long},
		},
	}
}

// TestResourceTableWrapRendersMultipleLines — with wrap on, a row whose
// wrap-column value exceeds the column width has its message visible across
// continuation lines. Non-wrap renders truncate the cell to the column
// width, so the tail of the message ("ptionally", "wrap test") never
// appears. The panel always renders to its full height, so a newline
// count is not a useful discriminator — substring presence is.
func TestResourceTableWrapRendersMultipleLines(t *testing.T) {
	tbl := panels.NewResourceTable(80, 24).SetKind("Event").WithRows(makeWrappableEventRows())

	noWrap := stripANSI(tbl.View())
	wrapped := stripANSI(tbl.SetWrapColumn(4).View())

	if !strings.Contains(noWrap, "this is an") {
		t.Fatalf("non-wrap render missing first segment of row content; got:\n%s", noWrap)
	}
	if strings.Contains(noWrap, "ptionally") {
		t.Errorf("non-wrap render unexpectedly exposed tail of message; the cell should have truncated")
	}
	if !strings.Contains(wrapped, "ptionally") {
		t.Errorf("wrap render did not surface the message tail; expected continuation lines.\nwrapped output:\n%s", wrapped)
	}
}

// TestResourceTableWrapDisablesHScroll — wrap and horizontal scroll are
// mutually exclusive. The spec's reason: both modes operate on the same
// column, so allowing both would let the user reach an undefined state.
func TestResourceTableWrapDisablesHScroll(t *testing.T) {
	tbl := panels.NewResourceTable(80, 24).SetKind("Event").WithRows(makeWrappableEventRows())
	tbl = tbl.SetWrapColumn(4)
	if tbl.HasHScroll() {
		t.Error("HasHScroll returned true while wrap is on; the two modes must be mutually exclusive")
	}
}

// TestResourceTableClearWrapColumnRevertsToSingleLine — turning wrap off
// brings the row back to its truncated one-line rendering. This is the path
// the `w` toggle takes on second press. Same substring discriminator as
// TestResourceTableWrapRendersMultipleLines.
func TestResourceTableClearWrapColumnRevertsToSingleLine(t *testing.T) {
	tbl := panels.NewResourceTable(80, 24).SetKind("Event").WithRows(makeWrappableEventRows())
	cleared := stripANSI(tbl.SetWrapColumn(4).ClearWrapColumn().View())
	if strings.Contains(cleared, "ptionally") {
		t.Errorf("ClearWrapColumn left wrap continuation visible:\n%s", cleared)
	}
}

// TestResourceTableWrapResetsOnKindChange — SetKind to a different kind
// clears the wrap column setting along with cursor / filter / hScroll.
// Without this, a kind switch from Event to Pod would render Pod with
// wrap-column index 4, which is the wrong column on a Pod table.
func TestResourceTableWrapResetsOnKindChange(t *testing.T) {
	tbl := panels.NewResourceTable(80, 24).SetKind("Event").WithRows(makeWrappableEventRows()).SetWrapColumn(4)
	tbl = tbl.SetKind("Pod")
	if tbl.WrapActive() {
		t.Error("wrap stayed on after SetKind to a different kind")
	}
}
