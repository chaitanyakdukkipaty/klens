package kinds

import (
	"context"
	"fmt"

	k8s "github.com/chaitanyak/klens/internal/k8s"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// namespace is the first migrated kind — cluster-scoped, only YAML view and
// Delete. Validates the package shape end-to-end before Pod exercises every
// capability interface.
type namespace struct{}

func (namespace) Meta() Meta {
	return Meta{
		Kind:       "Namespace",
		Plural:     "namespaces",
		Aliases:    []string{"ns"},
		Namespaced: false,
		GVR:        k8s.NamespaceGVR,
	}
}

func (namespace) Columns() []k8s.Column {
	return []k8s.Column{
		{Header: "NAME", Width: 40, Flex: true},
		{Header: "STATUS", Width: 14},
		{Header: "AGE", Width: 10},
	}
}

func (n namespace) List(c Context) ([]Row, error) {
	if c.Lister == nil {
		return nil, fmt.Errorf("namespace.List: no Lister")
	}
	objs, err := c.Lister.List(c.Ctx, n.Meta().GVR, "")
	if err != nil {
		return nil, err
	}
	rows := make([]Row, 0, len(objs))
	for _, o := range objs {
		ns, ok := o.(*corev1.Namespace)
		if !ok {
			continue
		}
		status := string(ns.Status.Phase)
		if ns.DeletionTimestamp != nil {
			status = "Terminating"
		}
		rows = append(rows, Row{
			Name:   ns.Name,
			Status: status,
			Values: []string{ns.Name, status, k8s.AgeString(ns.CreationTimestamp)},
			Raw:    ns,
		})
	}
	return rows, nil
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
