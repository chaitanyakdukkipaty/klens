package kinds

import (
	"context"
	"fmt"
	"strconv"

	k8s "github.com/chaitanyak/klens/internal/k8s"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
		{Header: "LAST SEEN", Width: 12},
		{Header: "COUNT", Width: 6},
		{Header: "AGE", Width: 10},
		{Header: "TYPE", Width: 10},
		{Header: "REASON", Width: 20},
		{Header: "OBJECT", Width: 30},
		{Header: "MESSAGE", Width: 40, Flex: true, Scrollable: true},
	}
}

func (e event) List(c Context) ([]Row, error) {
	if c.Lister == nil {
		return nil, fmt.Errorf("event.List: no Lister")
	}
	objs, err := c.Lister.List(c.Ctx, e.Meta().GVR, c.Namespace)
	if err != nil {
		return nil, err
	}
	rows := make([]Row, 0, len(objs))
	for _, o := range objs {
		ev, ok := o.(*corev1.Event)
		if !ok {
			continue
		}
		rows = append(rows, Row{
			Name:       ev.InvolvedObject.Name,
			Namespace:  ev.Namespace,
			Status:     ev.Type,
			SortByTime: ev.LastTimestamp.Time,
			Values: []string{
				k8s.AgeString(ev.LastTimestamp),
				strconv.Itoa(int(ev.Count)),
				k8s.AgeString(ev.FirstTimestamp),
				ev.Type,
				ev.Reason,
				ev.InvolvedObject.Name,
				ev.Message,
			},
			Raw: ev,
		})
	}
	return rows, nil
}

func (event) Fetch(ctx context.Context, c Context, ns, name string) (Object, error) {
	if c.Clientset == nil {
		return nil, fmt.Errorf("event.Fetch: no Clientset")
	}
	return c.Clientset.CoreV1().Events(ns).Get(ctx, name, metav1.GetOptions{})
}

func init() { register(event{}) }
