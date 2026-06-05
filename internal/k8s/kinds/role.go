package kinds

import (
	"context"
	"fmt"

	k8s "github.com/chaitanyak/klens/internal/k8s"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// role is namespaced and currently has no informer. Migration preserves the
// "no rows" behavior; Fetch is wired so YAML view works.
type role struct{}

func (role) Meta() Meta {
	return Meta{
		Kind:       "Role",
		Group:      "Access",
		Plural:     "roles",
		Aliases:    []string{"role"},
		Namespaced: true,
		GVR:        rbacv1.SchemeGroupVersion.WithResource("roles"),
	}
}

func (role) Columns() []k8s.Column {
	return []k8s.Column{
		{Header: "NAME", Width: 40, Flex: true},
		{Header: "AGE", Width: 10},
	}
}

func (role) List(Context) ([]Row, error) { return nil, nil }

func (role) Fetch(ctx context.Context, c Context, ns, name string) (Object, error) {
	if c.Clientset == nil {
		return nil, fmt.Errorf("role.Fetch: no Clientset")
	}
	return c.Clientset.RbacV1().Roles(ns).Get(ctx, name, metav1.GetOptions{})
}

func init() { register(role{}) }
