package kinds

import (
	"context"
	"fmt"
	"strings"
	"time"

	k8s "github.com/chaitanyak/klens/internal/k8s"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// event is namespaced. Column shape mirrors `kubectl get events`: LAST SEEN
// folds COUNT and FIRST SEEN into one cell ("5m (xN over 1h)"); OBJECT
// renders as Kind/Name; TYPE is colored via RowStatus plus the
// Warning/Normal entries in styles.statusStyleMap. The MESSAGE column is
// horizontally scrollable. User-controlled cells (Type, Reason, Message,
// InvolvedObject.{Kind,Name}) flow through safeCell to neutralize terminal
// escape sequences. SortByTime is set so the table renders newest-first.
type event struct{}

func (event) Meta() Meta {
	return Meta{
		Kind:       "Event",
		Plural:     "events",
		Aliases:    []string{"ev"},
		Namespaced: true,
		GVR:        k8s.EventGVR,
	}
}

func (event) Columns() []k8s.Column {
	return []k8s.Column{
		{Header: "LAST SEEN", Width: 22, Render: eventLastSeen},
		{Header: "TYPE", Width: 10, Render: eventType},
		{Header: "REASON", Width: 22, Render: eventReason},
		{Header: "OBJECT", Width: 32, Render: eventObject},
		{Header: "MESSAGE", Width: 40, Flex: true, Scrollable: true, Render: eventMessage},
	}
}

func (e event) List(c Context) ([]k8s.ResourceRow, error) { return listVia(e, c) }

// RowName overrides the default metadata-name with the involved object's
// name so multi-select and filter operate on what the user sees in the
// OBJECT column.
func (event) RowName(o runtime.Object) string { return o.(*corev1.Event).InvolvedObject.Name }

// RowStatus drives the colored TYPE column via the Status color key.
func (event) RowStatus(o runtime.Object) string { return o.(*corev1.Event).Type }

// RowSortByTime renders the table newest-first using the same precedence as
// lastSeenAge so a Series-driven event sorts on its most recent observation.
func (event) RowSortByTime(o runtime.Object) time.Time {
	return lastSeenTime(o.(*corev1.Event))
}

// safeCell neutralizes terminal control bytes that could escape the table
// cell and corrupt the screen. Mirrors cli-runtime's terminalEscaper;
// inlined so we don't take a direct dep on k8s.io/cli-runtime/pkg/printers.
var safeCell = strings.NewReplacer("\x1b", "^[", "\r", "\\r")

// lastSeenTime returns the most recent observation timestamp using the
// precedence kubectl uses: Series.LastObservedTime → EventTime →
// LastTimestamp → CreationTimestamp.
func lastSeenTime(e *corev1.Event) time.Time {
	if e.Series != nil && !e.Series.LastObservedTime.IsZero() {
		return e.Series.LastObservedTime.Time
	}
	if !e.EventTime.IsZero() {
		return e.EventTime.Time
	}
	if !e.LastTimestamp.IsZero() {
		return e.LastTimestamp.Time
	}
	return e.CreationTimestamp.Time
}

// firstSeenTime returns the original observation timestamp.
// Precedence: EventTime → FirstTimestamp → CreationTimestamp.
func firstSeenTime(e *corev1.Event) time.Time {
	if !e.EventTime.IsZero() {
		return e.EventTime.Time
	}
	if !e.FirstTimestamp.IsZero() {
		return e.FirstTimestamp.Time
	}
	return e.CreationTimestamp.Time
}

func lastSeenAge(e *corev1.Event) string {
	return k8s.AgeString(metav1.Time{Time: lastSeenTime(e)})
}

func firstSeenAge(e *corev1.Event) string {
	return k8s.AgeString(metav1.Time{Time: firstSeenTime(e)})
}

func eventLastSeen(o runtime.Object, _ k8s.RowContext) string {
	e := o.(*corev1.Event)
	switch {
	case e.Series != nil && e.Series.Count > 1:
		return fmt.Sprintf("%s (x%d over %s)",
			k8s.AgeString(metav1.Time{Time: e.Series.LastObservedTime.Time}),
			e.Series.Count, firstSeenAge(e))
	case e.Count > 1:
		return fmt.Sprintf("%s (x%d over %s)", lastSeenAge(e), e.Count, firstSeenAge(e))
	default:
		return lastSeenAge(e)
	}
}

func eventType(o runtime.Object, _ k8s.RowContext) string {
	return safeCell.Replace(o.(*corev1.Event).Type)
}

func eventReason(o runtime.Object, _ k8s.RowContext) string {
	return safeCell.Replace(o.(*corev1.Event).Reason)
}

func eventObject(o runtime.Object, _ k8s.RowContext) string {
	e := o.(*corev1.Event)
	kind := safeCell.Replace(e.InvolvedObject.Kind)
	name := safeCell.Replace(e.InvolvedObject.Name)
	if kind == "" {
		return name
	}
	return kind + "/" + name
}

func eventMessage(o runtime.Object, _ k8s.RowContext) string {
	return safeCell.Replace(o.(*corev1.Event).Message)
}

func (event) Fetch(ctx context.Context, c Context, ns, name string) (Object, error) {
	if c.Clientset == nil {
		return nil, fmt.Errorf("event.Fetch: no Clientset")
	}
	return c.Clientset.CoreV1().Events(ns).Get(ctx, name, metav1.GetOptions{})
}

func init() { register(event{}) }
