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
