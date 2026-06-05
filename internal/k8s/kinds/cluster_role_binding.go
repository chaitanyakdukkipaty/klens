package kinds

import (
	"context"
	"fmt"

	k8s "github.com/chaitanyak/klens/internal/k8s"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// clusterRoleBinding is cluster-scoped, list-only (no informer today).
// Migration preserves "no rows" behavior and adds Fetch via the typed
// clientset.
type clusterRoleBinding struct{}

func (clusterRoleBinding) Meta() Meta {
	return Meta{
		Kind:       "ClusterRoleBinding",
		Group:      "Access",
		Plural:     "clusterrolebindings",
		Aliases:    []string{"crb"},
		Namespaced: false,
		GVR:        schema.GroupVersionResource{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "clusterrolebindings"},
	}
}

func (clusterRoleBinding) Columns() []k8s.Column {
	return []k8s.Column{
		{Header: "NAME", Width: 40, Flex: true},
		{Header: "ROLE", Width: 30},
		{Header: "AGE", Width: 10},
	}
}

func (clusterRoleBinding) List(Context) ([]Row, error) { return nil, nil }

func (clusterRoleBinding) Fetch(ctx context.Context, c Context, _, name string) (Object, error) {
	if c.Clientset == nil {
		return nil, fmt.Errorf("clusterRoleBinding.Fetch: no Clientset")
	}
	return c.Clientset.RbacV1().ClusterRoleBindings().Get(ctx, name, metav1.GetOptions{})
}

func init() { register(clusterRoleBinding{}) }
