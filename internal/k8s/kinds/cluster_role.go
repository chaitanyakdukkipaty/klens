package kinds

import (
	"context"
	"fmt"

	k8s "github.com/chaitanyak/klens/internal/k8s"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// clusterRole is cluster-scoped, list-only (no informer today). Migration
// preserves "no rows" behavior and adds Fetch via the typed clientset.
type clusterRole struct{}

func (clusterRole) Meta() Meta {
	return Meta{
		Kind:       "ClusterRole",
		Group:      "Access",
		Plural:     "clusterroles",
		Aliases:    []string{"cr"},
		Namespaced: false,
		GVR:        schema.GroupVersionResource{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "clusterroles"},
	}
}

func (clusterRole) Columns() []k8s.Column {
	return []k8s.Column{
		{Header: "NAME", Width: 40, Flex: true},
		{Header: "AGE", Width: 10},
	}
}

func (clusterRole) List(Context) ([]Row, error) { return nil, nil }

func (clusterRole) Fetch(ctx context.Context, c Context, _, name string) (Object, error) {
	if c.Clientset == nil {
		return nil, fmt.Errorf("clusterRole.Fetch: no Clientset")
	}
	return c.Clientset.RbacV1().ClusterRoles().Get(ctx, name, metav1.GetOptions{})
}

func init() { register(clusterRole{}) }
