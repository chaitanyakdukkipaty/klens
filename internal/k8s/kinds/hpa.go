package kinds

import (
	"context"
	"fmt"

	k8s "github.com/chaitanyak/klens/internal/k8s"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// horizontalPodAutoscaler is namespaced, no informer. Migration preserves the
// "no rows" behavior and adds Fetch via the typed clientset.
type horizontalPodAutoscaler struct{}

func (horizontalPodAutoscaler) Meta() Meta {
	return Meta{
		Kind:       "HorizontalPodAutoscaler",
		Group:      "Config",
		Plural:     "horizontalpodautoscalers",
		Aliases:    []string{"hpa"},
		Namespaced: true,
		GVR:        autoscalingv1.SchemeGroupVersion.WithResource("horizontalpodautoscalers"),
	}
}

func (horizontalPodAutoscaler) Columns() []k8s.Column {
	return []k8s.Column{
		{Header: "NAME", Width: 40, Flex: true},
		{Header: "REFERENCE", Width: 30},
		{Header: "TARGETS", Width: 20},
		{Header: "MIN", Width: 6},
		{Header: "MAX", Width: 6},
		{Header: "AGE", Width: 10},
	}
}

func (horizontalPodAutoscaler) List(Context) ([]Row, error) { return nil, nil }

func (horizontalPodAutoscaler) Fetch(ctx context.Context, c Context, ns, name string) (Object, error) {
	if c.Clientset == nil {
		return nil, fmt.Errorf("horizontalPodAutoscaler.Fetch: no Clientset")
	}
	return c.Clientset.AutoscalingV1().HorizontalPodAutoscalers(ns).Get(ctx, name, metav1.GetOptions{})
}

func init() { register(horizontalPodAutoscaler{}) }
