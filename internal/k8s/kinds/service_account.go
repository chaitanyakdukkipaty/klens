package kinds

import (
	"context"
	"fmt"

	k8s "github.com/chaitanyak/klens/internal/k8s"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// serviceAccount preserves the pre-migration behavior: Fetch is wired so the
// YAML view works, but List returns nil because no row builder existed.
type serviceAccount struct{}

func (serviceAccount) Meta() Meta {
	return Meta{
		Kind:       "ServiceAccount",
		Plural:     "serviceaccounts",
		Aliases:    []string{"sa"},
		Namespaced: true,
		GVR:        corev1.SchemeGroupVersion.WithResource("serviceaccounts"),
	}
}

func (serviceAccount) Columns() []k8s.Column {
	return []k8s.Column{
		{Header: "NAME", Width: 40, Flex: true},
		{Header: "SECRETS", Width: 10},
		{Header: "AGE", Width: 10},
	}
}

func (serviceAccount) List(Context) ([]Row, error) { return nil, nil }

func (serviceAccount) Fetch(ctx context.Context, c Context, ns, name string) (Object, error) {
	if c.Clientset == nil {
		return nil, fmt.Errorf("serviceAccount.Fetch: no Clientset")
	}
	return c.Clientset.CoreV1().ServiceAccounts(ns).Get(ctx, name, metav1.GetOptions{})
}

func init() { register(serviceAccount{}) }
