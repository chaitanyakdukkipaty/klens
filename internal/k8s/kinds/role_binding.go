package kinds

import (
	"context"
	"fmt"

	k8s "github.com/chaitanyak/klens/internal/k8s"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// roleBinding is namespaced, no informer. Migration preserves "no rows"; Fetch
// is wired so YAML view works.
type roleBinding struct{}

func (roleBinding) Meta() Meta {
	return Meta{
		Kind:       "RoleBinding",
		Group:      "Access",
		Plural:     "rolebindings",
		Aliases:    []string{"rb"},
		Namespaced: true,
		GVR:        rbacv1.SchemeGroupVersion.WithResource("rolebindings"),
	}
}

func (roleBinding) Columns() []k8s.Column {
	return []k8s.Column{
		{Header: "NAME", Width: 40, Flex: true},
		{Header: "ROLE", Width: 30},
		{Header: "AGE", Width: 10},
	}
}

func (roleBinding) List(Context) ([]Row, error) { return nil, nil }

func (roleBinding) Fetch(ctx context.Context, c Context, ns, name string) (Object, error) {
	if c.Clientset == nil {
		return nil, fmt.Errorf("roleBinding.Fetch: no Clientset")
	}
	return c.Clientset.RbacV1().RoleBindings(ns).Get(ctx, name, metav1.GetOptions{})
}

func init() { register(roleBinding{}) }
