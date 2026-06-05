package panels

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func keyMsg(s string) tea.KeyPressMsg {
	switch s {
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "left":
		return tea.KeyPressMsg{Code: tea.KeyLeft}
	case "right":
		return tea.KeyPressMsg{Code: tea.KeyRight}
	default:
		r := []rune(s)[0]
		return tea.KeyPressMsg{Code: r, Text: s}
	}
}

func TestNavStartsOnFirstKind(t *testing.T) {
	n := NewNavPanel(24, 40)
	if n.ActiveKind() != "Pod" {
		t.Fatalf("initial ActiveKind = %q, want Pod", n.ActiveKind())
	}
	if n.CursorOnGroup() {
		t.Fatal("initial cursor sits on a group header")
	}
	rows := n.rows()
	if !rows[0].isGroup || rows[0].group != "Workloads" {
		t.Fatalf("row 0 = %+v, want Workloads header", rows[0])
	}
}

func TestNavCursorLiveSwitchesOnKindRowsOnly(t *testing.T) {
	n := NewNavPanel(24, 40)
	// Pod → Deployment.
	n, _ = n.Update(keyMsg("down"))
	if n.ActiveKind() != "Deployment" {
		t.Fatalf("ActiveKind after down = %q, want Deployment", n.ActiveKind())
	}
	// Walk up: row 0 is the Workloads header — active kind must not change.
	n, _ = n.Update(keyMsg("up"))
	n, _ = n.Update(keyMsg("up"))
	if !n.CursorOnGroup() {
		t.Fatal("cursor should rest on the Workloads header")
	}
	if n.ActiveKind() != "Pod" {
		t.Fatalf("ActiveKind on header = %q, want Pod (unchanged)", n.ActiveKind())
	}
}

func TestNavCollapseExpand(t *testing.T) {
	n := NewNavPanel(24, 40)
	total := len(n.rows())
	// h on a kind row folds its group and lands on the header.
	n, _ = n.Update(keyMsg("h"))
	if !n.CursorOnGroup() {
		t.Fatal("cursor not on header after fold")
	}
	folded := len(n.rows())
	if folded >= total {
		t.Fatalf("fold did not shrink rows: %d >= %d", folded, total)
	}
	// l on the header expands again.
	n, _ = n.Update(keyMsg("l"))
	if got := len(n.rows()); got != total {
		t.Fatalf("expand: rows = %d, want %d", got, total)
	}
}

func TestNavClickTogglesGroupWithoutKind(t *testing.T) {
	n := NewNavPanel(24, 40)
	// innerY 1 is the first body row (Workloads header, no filter bar).
	n2, kind, toggled := n.HandleClickAt(1)
	if !toggled || kind != "" {
		t.Fatalf("header click = (kind=%q, toggled=%v), want (\"\", true)", kind, toggled)
	}
	if len(n2.rows()) >= len(n.rows()) {
		t.Fatal("header click did not collapse the group")
	}
	// Clicking the same header again expands.
	n3, _, toggled := n2.HandleClickAt(1)
	if !toggled || len(n3.rows()) != len(n.rows()) {
		t.Fatal("second header click did not expand the group")
	}
}

func TestNavClickSelectsKind(t *testing.T) {
	n := NewNavPanel(24, 40)
	// innerY 2 = first kind row (Pod) under the Workloads header.
	n2, kind, toggled := n.HandleClickAt(2)
	if toggled || kind != "Pod" {
		t.Fatalf("kind click = (kind=%q, toggled=%v), want (Pod, false)", kind, toggled)
	}
	if n2.ActiveKind() != "Pod" {
		t.Fatalf("ActiveKind = %q after click", n2.ActiveKind())
	}
}

func TestNavSetActiveKindExpandsGroup(t *testing.T) {
	n := NewNavPanel(24, 40)
	// Collapse everything first.
	for _, g := range groupOrder {
		n.collapsed[g] = true
	}
	n = n.SetActiveKind("Service")
	if n.ActiveKind() != "Service" {
		t.Fatalf("ActiveKind = %q, want Service", n.ActiveKind())
	}
	if n.collapsed["Network"] {
		t.Fatal("SetActiveKind left the Network group collapsed")
	}
	rows := n.rows()
	if rows[n.cursor].isGroup || rows[n.cursor].item.kind != "Service" {
		t.Fatalf("cursor not on Service row: %+v", rows[n.cursor])
	}
}

func TestNavFilterFlattens(t *testing.T) {
	n := NewNavPanel(24, 40)
	n, _ = n.Update(keyMsg("/"))
	for _, ch := range "dep" {
		n, _ = n.Update(tea.KeyPressMsg{Code: ch, Text: string(ch)})
	}
	for _, row := range n.rows() {
		if row.isGroup {
			t.Fatal("group header present while filtering")
		}
	}
	if n.ActiveKind() != "Deployment" {
		t.Fatalf("ActiveKind under filter = %q, want Deployment", n.ActiveKind())
	}
}

func TestNavScrollKeepsCursorVisible(t *testing.T) {
	n := NewNavPanel(24, 12) // tiny: ~9 visible body rows for 33 rows
	for i := 0; i < 25; i++ {
		n, _ = n.Update(keyMsg("down"))
	}
	vis := n.visibleBodyRows()
	if n.cursor < n.scroll || n.cursor >= n.scroll+vis {
		t.Fatalf("cursor %d outside window [%d,%d)", n.cursor, n.scroll, n.scroll+vis)
	}
	// And the view must not exceed the panel height.
	if got := strings.Count(n.View(), "\n") + 1; got > 12 {
		t.Fatalf("View renders %d lines, panel height is 12", got)
	}
}

func TestNavActiveCountRendered(t *testing.T) {
	n := NewNavPanel(28, 40).SetActiveCounts(14, false)
	if !strings.Contains(n.View(), "14") {
		t.Fatal("active kind count not rendered")
	}
}
