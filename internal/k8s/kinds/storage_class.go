package kinds

import (
	"context"
	"fmt"

	k8s "github.com/chaitanyak/klens/internal/k8s"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// storageClass is cluster-scoped and currently has no informer; List returns
// no rows (mirrors the pre-migration behavior). Fetch is wired so the YAML
// view works when a row is selected through other paths.
type storageClass struct{}

func (storageClass) Meta() Meta {
	return Meta{
		Kind:       "StorageClass",
		Plural:     "storageclasses",
		Aliases:    []string{"sc"},
		Namespaced: false,
		GVR:        schema.GroupVersionResource{Group: "storage.k8s.io", Version: "v1", Resource: "storageclasses"},
	}
}

func (storageClass) Columns() []k8s.Column {
	return []k8s.Column{
		{Header: "NAME", Width: 40, Flex: true},
		{Header: "PROVISIONER", Width: 30},
		{Header: "AGE", Width: 10},
	}
}

func (storageClass) List(Context) ([]Row, error) { return nil, nil }

func (storageClass) Fetch(ctx context.Context, c Context, _, name string) (Object, error) {
	if c.Clientset == nil {
		return nil, fmt.Errorf("storageClass.Fetch: no Clientset")
	}
	return c.Clientset.StorageV1().StorageClasses().Get(ctx, name, metav1.GetOptions{})
}

func init() { register(storageClass{}) }
