package kinds

import (
	"context"
	"fmt"

	k8s "github.com/chaitanyak/klens/internal/k8s"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// namespace is the first migrated kind — cluster-scoped, only YAML view and
// Delete. Validates the package shape end-to-end before Pod exercises every
// capability interface.
type namespace struct{}

func (namespace) Meta() Meta {
	return Meta{
		Kind:       "Namespace",
		Group:      "Cluster",
		Plural:     "namespaces",
		Aliases:    []string{"ns"},
		Namespaced: false,
		GVR:        k8s.NamespaceGVR,
	}
}

func (namespace) Columns() []k8s.Column {
	return []k8s.Column{
		{Header: "NAME", Width: 40, Flex: true, Render: namespaceName},
		{Header: "STATUS", Width: 14, Render: namespaceStatus},
		{Header: "AGE", Width: 10, Render: namespaceAge},
	}
}

func (n namespace) List(c Context) ([]k8s.ResourceRow, error) { return listVia(n, c) }

// RowStatus drives the colored STATUS column. Mirrors the cell text returned
// by namespaceStatus so the row's color key matches the rendered cell.
func (namespace) RowStatus(o runtime.Object) string { return namespaceStatus(o, k8s.RowContext{}) }

func namespaceName(o runtime.Object, _ k8s.RowContext) string {
	return o.(*corev1.Namespace).Name
}

func namespaceStatus(o runtime.Object, _ k8s.RowContext) string {
	ns := o.(*corev1.Namespace)
	if ns.DeletionTimestamp != nil {
		return "Terminating"
	}
	return string(ns.Status.Phase)
}

func namespaceAge(o runtime.Object, _ k8s.RowContext) string {
	return k8s.AgeString(o.(*corev1.Namespace).CreationTimestamp)
}

func (namespace) Fetch(ctx context.Context, c Context, ns, name string) (Object, error) {
	if c.Clientset == nil {
		return nil, fmt.Errorf("namespace.Fetch: no Clientset")
	}
	return c.Clientset.CoreV1().Namespaces().Get(ctx, name, metav1.GetOptions{})
}

func (namespace) Delete(c Context, _, name string) error {
	if c.Clientset == nil {
		return fmt.Errorf("namespace.Delete: no Clientset")
	}
	grace := int64(0)
	return c.Clientset.CoreV1().Namespaces().Delete(c.Ctx, name, metav1.DeleteOptions{
		GracePeriodSeconds: &grace,
	})
}

func init() { register(namespace{}) }
