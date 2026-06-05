package app

import (
	"testing"

	k8sops "github.com/chaitanyak/klens/internal/k8s"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// rowFromEvent wraps a typed event in a ResourceRow as the informer pipeline
// would; only Name and Raw are meaningful to maybeFaultsFilter.
func rowFromEvent(e *corev1.Event) k8sops.ResourceRow {
	return k8sops.ResourceRow{Name: e.Name, Raw: e}
}

// rowsFor returns three rows: one Warning, one Normal, one Error string
// literal. Mirrors the truth-table on (event).IsFaultRow.
func rowsFor() []k8sops.ResourceRow {
	return []k8sops.ResourceRow{
		rowFromEvent(&corev1.Event{Type: "Warning"}),
		rowFromEvent(&corev1.Event{Type: "Normal"}),
		rowFromEvent(&corev1.Event{Type: "Error"}),
	}
}

// modelWithKind returns a Model wired to look like the user navigated to
// `kind` — the nav panel and the table controller both report it as active.
// The fields touched mirror what setKindAndSync would have set during a
// real nav change. Only the Model state involved in maybeFaultsFilter +
// isEventsTable is set; the rest stays at the zero value, which is the
// shape every other internal test in this package would also use.
func modelWithKind(kind string) Model {
	m := New(false)
	m.nav = m.nav.SetActiveKind(kind)
	m.tableCtrl = m.tableCtrl.SetKind(kind)
	m.mode = ModeTable
	return m
}

// TestMaybeFaultsFilterOff — the toggle is off, every row passes through
// unchanged. Sanity guard against accidentally always-on filtering after a
// kind switch that doesn't clear state.
func TestMaybeFaultsFilterOff(t *testing.T) {
	m := modelWithKind("Event")
	got := m.maybeFaultsFilter(rowsFor())
	if len(got) != 3 {
		t.Fatalf("toggle off: filtered to %d rows, want 3", len(got))
	}
}

// TestMaybeFaultsFilterOnKeepsWarningAndError — the spec's central
// invariant: faults-only retains Warning and Error rows, drops Normal.
func TestMaybeFaultsFilterOnKeepsWarningAndError(t *testing.T) {
	m := modelWithKind("Event")
	m.eventFaultsOnly = true
	got := m.maybeFaultsFilter(rowsFor())
	if len(got) != 2 {
		t.Fatalf("filtered to %d rows, want 2", len(got))
	}
	gotTypes := []string{got[0].Raw.(*corev1.Event).Type, got[1].Raw.(*corev1.Event).Type}
	want := map[string]bool{"Warning": true, "Error": true}
	for _, ty := range gotTypes {
		if !want[ty] {
			t.Errorf("unexpected row type %q in faults output", ty)
		}
	}
}

// TestMaybeFaultsFilterOffWhenKindNotEvent — the toggle is event-specific
// (no second kind opts in via FaultRowMarker today). Even with the flag on,
// non-Event tables pass through.
func TestMaybeFaultsFilterOffWhenKindNotEvent(t *testing.T) {
	m := modelWithKind("Pod")
	m.eventFaultsOnly = true
	got := m.maybeFaultsFilter(rowsFor())
	if len(got) != 3 {
		t.Fatalf("non-Event kind filtered to %d, want 3", len(got))
	}
}

// TestClearEventsViewStateResets — leaving the Event kind has to wipe
// every events-only flag and the wrap-column state on the table panel.
// Without this, the next time the user lands on Events they'd inherit
// stale state from the previous session.
func TestClearEventsViewStateResets(t *testing.T) {
	m := modelWithKind("Event")
	m.eventFaultsOnly = true
	m.eventWrapMessage = true
	m.tableCtrl = m.tableCtrl.SetWrapColumn(4)
	m.pendingJumpName = "foo"

	m.clearEventsViewState()
	if m.eventFaultsOnly {
		t.Error("eventFaultsOnly not cleared")
	}
	if m.eventWrapMessage {
		t.Error("eventWrapMessage not cleared")
	}
	if m.tableCtrl.WrapActive() {
		t.Error("table wrap not cleared")
	}
	if m.pendingJumpName != "" {
		t.Error("pendingJumpName not cleared")
	}
}

// TestEventsHelpAdvertisesNewKeys — the help footer for Events must show
// the three new bindings (ctrl+z, w, o) and must not list anything the
// kind doesn't support (ctrl+d, a). Anchors the spec's "what's
// advertised" decision.
func TestEventsHelpAdvertisesNewKeys(t *testing.T) {
	have := map[string]bool{}
	for _, h := range eventsHelp() {
		have[h.Key] = true
	}
	for _, want := range []string{"ctrl+z", "w", "o", "/"} {
		if !have[want] {
			t.Errorf("eventsHelp missing %q", want)
		}
	}
	// ctrl+d/a/e: no matching capability on Event. y/d/l/x/m: view-mode
	// actions live in the tab bar above the content, never in footers.
	for _, banned := range []string{"ctrl+d", "a", "e", "y", "d", "l", "x", "m"} {
		if have[banned] {
			t.Errorf("eventsHelp advertises %q (tab-bar action or unsupported capability)", banned)
		}
	}
}

// TestIsEventsTableGating — guards the cheap helper that every event-only
// keybinding checks. The wrong kind or the wrong mode must report false.
func TestIsEventsTableGating(t *testing.T) {
	cases := []struct {
		name string
		kind string
		mode ContentMode
		want bool
	}{
		{"Events table mode", "Event", ModeTable, true},
		{"Events YAML mode", "Event", ModeYAML, false},
		{"Pod table mode", "Pod", ModeTable, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := modelWithKind(c.kind)
			m.mode = c.mode
			if got := m.isEventsTable(); got != c.want {
				t.Errorf("isEventsTable = %v, want %v", got, c.want)
			}
		})
	}
}

// eventModelWith builds a model already on the Event kind with one event
// row whose Raw is the supplied *corev1.Event. The cursor lands on row 0
// so actionEventJumpToInvolvedObject's SelectedRow() returns it.
func eventModelWith(ev *corev1.Event) Model {
	m := modelWithKind("Event")
	rows := []k8sops.ResourceRow{{Name: "evt-1", Raw: ev}}
	m.tableCtrl = m.tableCtrl.WithRows(rows)
	return m
}

// TestActionEventJumpEmptyInvolvedObject — an event with no resolvable
// InvolvedObject must not move the user anywhere; the status bar
// communicates why.
func TestActionEventJumpEmptyInvolvedObject(t *testing.T) {
	ev := &corev1.Event{
		ObjectMeta:     metav1.ObjectMeta{Name: "evt-1"},
		InvolvedObject: corev1.ObjectReference{}, // empty
	}
	m := eventModelWith(ev)
	got, _ := m.actionEventJumpToInvolvedObject()
	if got.statusBar.Message() != "event has no involved object" {
		t.Errorf("status = %q, want 'event has no involved object'", got.statusBar.Message())
	}
	if got.nav.ActiveKind() != "Event" {
		t.Errorf("nav kind changed to %q on empty involved object", got.nav.ActiveKind())
	}
}

// TestActionEventJumpUnknownKind — the involved object's Kind isn't in our
// registry. The user stays put, but the message names what we couldn't
// resolve so they can act on it (file an issue, request the CRD).
func TestActionEventJumpUnknownKind(t *testing.T) {
	ev := &corev1.Event{
		ObjectMeta:     metav1.ObjectMeta{Name: "evt-1", Namespace: "default"},
		InvolvedObject: corev1.ObjectReference{Kind: "MadeUpKind", Name: "x"},
	}
	m := eventModelWith(ev)
	got, _ := m.actionEventJumpToInvolvedObject()
	if got.statusBar.Message() != "unknown kind: MadeUpKind" {
		t.Errorf("status = %q, want 'unknown kind: MadeUpKind'", got.statusBar.Message())
	}
	if got.nav.ActiveKind() != "Event" {
		t.Errorf("nav kind changed to %q on unknown kind", got.nav.ActiveKind())
	}
}

// TestActionEventJumpKnownKindSwitchesNav — a resolvable involved object
// switches the nav kind, sets focus to content, and queues the jump.
func TestActionEventJumpKnownKindSwitchesNav(t *testing.T) {
	ev := &corev1.Event{
		ObjectMeta:     metav1.ObjectMeta{Name: "evt-1", Namespace: "default"},
		InvolvedObject: corev1.ObjectReference{Kind: "Pod", Name: "target-pod", Namespace: "default"},
	}
	m := eventModelWith(ev)
	// Set the model's namespace to default so the namespace switch path is
	// skipped — the test focuses on the kind switch + pendingJump scheduling.
	m.namespace = "default"
	got, _ := m.actionEventJumpToInvolvedObject()
	if got.nav.ActiveKind() != "Pod" {
		t.Errorf("nav kind = %q, want Pod", got.nav.ActiveKind())
	}
	if got.pendingJumpName != "target-pod" {
		t.Errorf("pendingJumpName = %q, want target-pod", got.pendingJumpName)
	}
	if got.pendingJumpKind != "Pod" {
		t.Errorf("pendingJumpKind = %q, want Pod", got.pendingJumpKind)
	}
	if got.focus != FocusContent {
		t.Errorf("focus = %v, want FocusContent", got.focus)
	}
}
