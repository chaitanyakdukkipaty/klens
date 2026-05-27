package kinds

import (
	"context"
	"fmt"

	k8s "github.com/chaitanyak/klens/internal/k8s"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// configMap implements Deleter + Applier.
type configMap struct{}

func (configMap) Meta() Meta {
	return Meta{
		Kind:       "ConfigMap",
		Plural:     "configmaps",
		Aliases:    []string{"cm"},
		Namespaced: true,
		GVR:        k8s.ConfigMapGVR,
	}
}

func (configMap) Columns() []k8s.Column {
	return []k8s.Column{
		{Header: "NAME", Width: 40, Flex: true},
		{Header: "DATA", Width: 8},
		{Header: "AGE", Width: 10},
	}
}

func (cm configMap) List(c Context) ([]Row, error) {
	cms, err := listTyped[*corev1.ConfigMap](c, cm.Meta().GVR, c.Namespace)
	if err != nil {
		return nil, err
	}
	rows := make([]Row, 0, len(cms))
	for _, m := range cms {
		rows = append(rows, Row{
			Name:      m.Name,
			Namespace: m.Namespace,
			Values:    []string{m.Name, fmt.Sprintf("%d", len(m.Data)), k8s.AgeString(m.CreationTimestamp)},
			Raw:       m,
		})
	}
	return rows, nil
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
