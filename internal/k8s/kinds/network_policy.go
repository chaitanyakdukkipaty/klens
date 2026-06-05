package kinds

import (
	"context"
	"fmt"

	k8s "github.com/chaitanyak/klens/internal/k8s"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// networkPolicy preserves pre-migration behavior (no row builder). Fetch is
// wired so YAML view works through the new path.
type networkPolicy struct{}

func (networkPolicy) Meta() Meta {
	return Meta{
		Kind:       "NetworkPolicy",
		Group:      "Network",
		Plural:     "networkpolicies",
		Aliases:    []string{"netpol"},
		Namespaced: true,
		GVR:        networkingv1.SchemeGroupVersion.WithResource("networkpolicies"),
	}
}

func (networkPolicy) Columns() []k8s.Column {
	return []k8s.Column{
		{Header: "NAME", Width: 40, Flex: true},
		{Header: "POD-SELECTOR", Width: 30},
		{Header: "AGE", Width: 10},
	}
}

func (networkPolicy) List(Context) ([]Row, error) { return nil, nil }

func (networkPolicy) Fetch(ctx context.Context, c Context, ns, name string) (Object, error) {
	if c.Clientset == nil {
		return nil, fmt.Errorf("networkPolicy.Fetch: no Clientset")
	}
	return c.Clientset.NetworkingV1().NetworkPolicies(ns).Get(ctx, name, metav1.GetOptions{})
}

func init() { register(networkPolicy{}) }
