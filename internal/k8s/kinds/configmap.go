package kinds

import (
	"context"
	"fmt"

	k8s "github.com/chaitanyak/klens/internal/k8s"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
)

// configMap implements Deleter + Applier.
type configMap struct{}

func (configMap) Meta() Meta {
	return Meta{
		Kind:       "ConfigMap",
		Group:      "Config",
		Plural:     "configmaps",
		Aliases:    []string{"cm"},
		Namespaced: true,
		GVR:        k8s.ConfigMapGVR,
	}
}

func (configMap) Columns() []k8s.Column {
	return []k8s.Column{
		{Header: "NAME", Width: 40, Flex: true, Render: configMapName},
		{Header: "DATA", Width: 8, Render: configMapData},
		{Header: "AGE", Width: 10, Render: configMapAge},
	}
}

func (cm configMap) List(c Context) ([]k8s.ResourceRow, error) { return listVia(cm, c) }

func configMapName(o runtime.Object, _ k8s.RowContext) string { return o.(*corev1.ConfigMap).Name }

func configMapData(o runtime.Object, _ k8s.RowContext) string {
	return fmt.Sprintf("%d", len(o.(*corev1.ConfigMap).Data))
}

func configMapAge(o runtime.Object, _ k8s.RowContext) string {
	return k8s.AgeString(o.(*corev1.ConfigMap).CreationTimestamp)
}

func (configMap) Fetch(ctx context.Context, c Context, ns, name string) (Object, error) {
	if c.Clientset == nil {
		return nil, fmt.Errorf("configMap.Fetch: no Clientset")
	}
	return c.Clientset.CoreV1().ConfigMaps(ns).Get(ctx, name, metav1.GetOptions{})
}

func (configMap) Delete(c Context, ns, name string) error {
	if c.Clientset == nil {
		return fmt.Errorf("configMap.Delete: no Clientset")
	}
	grace := int64(0)
	return c.Clientset.CoreV1().ConfigMaps(ns).Delete(c.Ctx, name, metav1.DeleteOptions{GracePeriodSeconds: &grace})
}

func (configMap) Apply(c Context, ns, name string, body []byte) error {
	if c.Clientset == nil {
		return fmt.Errorf("configMap.Apply: no Clientset")
	}
	_, err := c.Clientset.CoreV1().ConfigMaps(ns).Patch(
		c.Ctx, name, types.MergePatchType, body,
		metav1.PatchOptions{FieldManager: "klens"})
	return err
}

func init() { register(configMap{}) }
