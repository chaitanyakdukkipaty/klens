package kinds

import (
	"strings"
	"testing"
	"time"

	k8s "github.com/chaitanyak/klens/internal/k8s"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestEventLastSeen covers all three rendering branches: single occurrence
// (no count, no series), Count > 1, and Series.Count > 1. Each is reduced
// to its visible string so any regression in the kubectl-style template
// surfaces here instead of through manual cluster spelunking.
func TestEventLastSeen(t *testing.T) {
	now := time.Now()
	tenMinAgo := metav1.Time{Time: now.Add(-10 * time.Minute)}
	oneHourAgo := metav1.Time{Time: now.Add(-1 * time.Hour)}

	cases := []struct {
		name string
		ev   *corev1.Event
		want string
	}{
		{
			name: "single occurrence",
			ev:   &corev1.Event{LastTimestamp: tenMinAgo, FirstTimestamp: tenMinAgo, Count: 1},
			want: "10m",
		},
		{
			name: "count > 1",
			ev: &corev1.Event{
				LastTimestamp:  tenMinAgo,
				FirstTimestamp: oneHourAgo,
				Count:          3,
			},
			want: "10m (x3 over 1h)",
		},
		{
			name: "series count > 1",
			ev: &corev1.Event{
				FirstTimestamp: oneHourAgo,
				Series: &corev1.EventSeries{
					Count:            5,
					LastObservedTime: metav1.MicroTime{Time: tenMinAgo.Time},
				},
			},
			want: "10m (x5 over 1h)",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := eventLastSeen(c.ev, k8s.RowContext{})
			if got != c.want {
				t.Errorf("eventLastSeen = %q, want %q", got, c.want)
			}
		})
	}
}

// TestEventObject pins the Kind/Name formatting and the empty-Kind fallback.
func TestEventObject(t *testing.T) {
	cases := []struct {
		name string
		ev   *corev1.Event
		want string
	}{
		{"kind and name", &corev1.Event{InvolvedObject: corev1.ObjectReference{Kind: "Pod", Name: "foo"}}, "Pod/foo"},
		{"empty kind", &corev1.Event{InvolvedObject: corev1.ObjectReference{Kind: "", Name: "foo"}}, "foo"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := eventObject(c.ev, k8s.RowContext{})
			if got != c.want {
				t.Errorf("eventObject = %q, want %q", got, c.want)
			}
		})
	}
}

// TestEventCellTerminalEscaping guards the CVE-2021-25743-style hardening:
// any user-controlled cell must have no raw \x1b or \r left in the output.
func TestEventCellTerminalEscaping(t *testing.T) {
	ev := &corev1.Event{
		Type:           "Warn\x1bing",
		Reason:         "Bad\x1bReason",
		Message:        "alert\x1b[31mred\r\n",
		InvolvedObject: corev1.ObjectReference{Kind: "Pod\x1b", Name: "foo\r"},
	}
	cells := map[string]string{
		"Type":    eventType(ev, k8s.RowContext{}),
		"Reason":  eventReason(ev, k8s.RowContext{}),
		"Message": eventMessage(ev, k8s.RowContext{}),
		"Object":  eventObject(ev, k8s.RowContext{}),
	}
	for name, got := range cells {
		if strings.ContainsAny(got, "\x1b\r") {
			t.Errorf("%s cell contains raw control byte: %q", name, got)
		}
	}
}

// TestEventIsFaultRow covers all four meaningful Type values the events
// table can produce: Warning + the Error string literal are faults; Normal
// and the empty string are not. Hardcoded against the spec so any future
// "be lenient about Error" change is visible.
func TestEventIsFaultRow(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"Warning", true},
		{"Error", true},
		{"Normal", false},
		{"", false},
	}
	for _, c := range cases {
		ev := &corev1.Event{Type: c.in}
		if got := (event{}).IsFaultRow(ev); got != c.want {
			t.Errorf("IsFaultRow(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

// TestEventInvolvedObject pins the three branches: a fully-populated
// reference resolves; an empty-Kind reference returns ok=false; a reference
// missing its own namespace inherits the event's namespace.
func TestEventInvolvedObject(t *testing.T) {
	t.Run("kind and namespace present", func(t *testing.T) {
		ev := &corev1.Event{
			ObjectMeta:     metav1.ObjectMeta{Namespace: "ns-event"},
			InvolvedObject: corev1.ObjectReference{Kind: "Pod", Name: "foo", Namespace: "ns-pod", APIVersion: "v1"},
		}
		gvk, ns, name, ok := (event{}).InvolvedObject(ev)
		if !ok {
			t.Fatal("InvolvedObject ok=false on fully-populated reference")
		}
		if gvk.Kind != "Pod" || gvk.Version != "v1" || gvk.Group != "" {
			t.Errorf("gvk = %v, want {Group:\"\", Version:\"v1\", Kind:\"Pod\"}", gvk)
		}
		if ns != "ns-pod" {
			t.Errorf("namespace = %q, want ns-pod (reference's own value)", ns)
		}
		if name != "foo" {
			t.Errorf("name = %q, want foo", name)
		}
	})
	t.Run("empty kind returns ok=false", func(t *testing.T) {
		ev := &corev1.Event{InvolvedObject: corev1.ObjectReference{Name: "foo"}}
		if _, _, _, ok := (event{}).InvolvedObject(ev); ok {
			t.Error("InvolvedObject ok=true on empty-kind reference")
		}
	})
	t.Run("namespace inherits from event when reference is namespace-less", func(t *testing.T) {
		ev := &corev1.Event{
			ObjectMeta:     metav1.ObjectMeta{Namespace: "ns-event"},
			InvolvedObject: corev1.ObjectReference{Kind: "Pod", Name: "foo"},
		}
		_, ns, _, ok := (event{}).InvolvedObject(ev)
		if !ok {
			t.Fatal("InvolvedObject ok=false")
		}
		if ns != "ns-event" {
			t.Errorf("namespace = %q, want ns-event (inherited from event)", ns)
		}
	})
	t.Run("grouped APIVersion parses correctly", func(t *testing.T) {
		ev := &corev1.Event{InvolvedObject: corev1.ObjectReference{Kind: "Deployment", Name: "x", APIVersion: "apps/v1"}}
		gvk, _, _, ok := (event{}).InvolvedObject(ev)
		if !ok {
			t.Fatal("InvolvedObject ok=false")
		}
		if gvk.Group != "apps" || gvk.Version != "v1" || gvk.Kind != "Deployment" {
			t.Errorf("gvk = %v, want apps/v1/Deployment", gvk)
		}
	})
}

// TestEventRowSortByTime pins the lastSeenTime precedence chain. Series
// wins over EventTime wins over LastTimestamp.
func TestEventRowSortByTime(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		ev   *corev1.Event
		want time.Time
	}{
		{
			name: "LastTimestamp only",
			ev:   &corev1.Event{LastTimestamp: metav1.Time{Time: t0}},
			want: t0,
		},
		{
			name: "EventTime preferred over LastTimestamp",
			ev: &corev1.Event{
				LastTimestamp: metav1.Time{Time: t0.Add(-time.Hour)},
				EventTime:     metav1.MicroTime{Time: t0},
			},
			want: t0,
		},
		{
			name: "Series.LastObservedTime preferred over all",
			ev: &corev1.Event{
				LastTimestamp: metav1.Time{Time: t0.Add(-2 * time.Hour)},
				EventTime:     metav1.MicroTime{Time: t0.Add(-time.Hour)},
				Series:        &corev1.EventSeries{LastObservedTime: metav1.MicroTime{Time: t0}},
			},
			want: t0,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := event{}.RowSortByTime(c.ev)
			if !got.Equal(c.want) {
				t.Errorf("RowSortByTime = %v, want %v", got, c.want)
			}
		})
	}
}

// TestEventDoesNotImplementRowNamer is a regression guard. An earlier version
// of event{} overrode RowName to return InvolvedObject.Name, which broke
// YAML / Describe (the row's Name became the involved object's name, so
// events.Get(ns, name) returned 404). If a future change re-adds RowName on
// event, this test fails loudly instead of waiting for a user bug report.
func TestEventDoesNotImplementRowNamer(t *testing.T) {
	if _, ok := any(event{}).(RowNamer); ok {
		t.Fatal("event must not implement RowNamer — row.Name is used to fetch the resource for YAML/describe and must equal the event's metadata Name, not InvolvedObject.Name")
	}
}
