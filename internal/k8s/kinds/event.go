package kinds

import (
	"context"
	"fmt"
	"strconv"
	"time"

	k8s "github.com/chaitanyak/klens/internal/k8s"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// event is namespaced. The MESSAGE column is horizontally scrollable; the
// resource table reads Column.Scrollable to enable left/right navigation.
// SortByTime is set so the table renders events newest-first.
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
		{Header: "LAST SEEN", Width: 12, Render: eventLastSeen},
		{Header: "COUNT", Width: 6, Render: eventCount},
		{Header: "AGE", Width: 10, Render: eventAge},
		{Header: "TYPE", Width: 10, Render: eventType},
		{Header: "REASON", Width: 20, Render: eventReason},
		{Header: "OBJECT", Width: 30, Render: eventObject},
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

// RowSortByTime renders the table newest-first by LastTimestamp.
func (event) RowSortByTime(o runtime.Object) time.Time {
	return o.(*corev1.Event).LastTimestamp.Time
}

func eventLastSeen(o runtime.Object, _ k8s.RowContext) string {
	return k8s.AgeString(o.(*corev1.Event).LastTimestamp)
}

func eventCount(o runtime.Object, _ k8s.RowContext) string {
	return strconv.Itoa(int(o.(*corev1.Event).Count))
}

func eventAge(o runtime.Object, _ k8s.RowContext) string {
	return k8s.AgeString(o.(*corev1.Event).FirstTimestamp)
}

func eventType(o runtime.Object, _ k8s.RowContext) string { return o.(*corev1.Event).Type }

func eventReason(o runtime.Object, _ k8s.RowContext) string { return o.(*corev1.Event).Reason }

func eventObject(o runtime.Object, _ k8s.RowContext) string {
	return o.(*corev1.Event).InvolvedObject.Name
}

func eventMessage(o runtime.Object, _ k8s.RowContext) string { return o.(*corev1.Event).Message }

func (event) Fetch(ctx context.Context, c Context, ns, name string) (Object, error) {
	if c.Clientset == nil {
		return nil, fmt.Errorf("event.Fetch: no Clientset")
	}
	return c.Clientset.CoreV1().Events(ns).Get(ctx, name, metav1.GetOptions{})
}

func init() { register(event{}) }
