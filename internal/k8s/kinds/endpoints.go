package kinds

import (
	"context"
	"fmt"

	k8s "github.com/chaitanyak/klens/internal/k8s"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// endpoints preserves the pre-migration behavior: no row builder existed, so
// List returns nil. Fetch is wired via the typed clientset; previously the
// kind had no Fetch handler at all.
type endpoints struct{}

func (endpoints) Meta() Meta {
	return Meta{
		Kind:       "Endpoints",
		Group:      "Network",
		Plural:     "endpoints",
		Aliases:    []string{"ep"},
		Namespaced: true,
		GVR:        corev1.SchemeGroupVersion.WithResource("endpoints"),
	}
}

func (endpoints) Columns() []k8s.Column {
	return []k8s.Column{
		{Header: "NAME", Width: 40, Flex: true},
		{Header: "ENDPOINTS", Width: 40},
		{Header: "AGE", Width: 10},
	}
}

func (endpoints) List(Context) ([]Row, error) { return nil, nil }

func (endpoints) Fetch(ctx context.Context, c Context, ns, name string) (Object, error) {
	if c.Clientset == nil {
		return nil, fmt.Errorf("endpoints.Fetch: no Clientset")
	}
	return c.Clientset.CoreV1().Endpoints(ns).Get(ctx, name, metav1.GetOptions{})
}

func init() { register(endpoints{}) }
