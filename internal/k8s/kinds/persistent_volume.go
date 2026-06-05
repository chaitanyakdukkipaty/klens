package kinds

import (
	"context"
	"fmt"
	"strings"

	k8s "github.com/chaitanyak/klens/internal/k8s"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// persistentVolume is cluster-scoped, supports YAML view + delete.
type persistentVolume struct{}

func (persistentVolume) Meta() Meta {
	return Meta{
		Kind:       "PersistentVolume",
		Group:      "Storage",
		Plural:     "persistentvolumes",
		Aliases:    []string{"pv"},
		Namespaced: false,
		GVR:        k8s.PersistentVolumeGVR,
	}
}

func (persistentVolume) Columns() []k8s.Column {
	return []k8s.Column{
		{Header: "NAME", Width: 40, Flex: true, Render: pvName},
		{Header: "CAPACITY", Width: 12, Render: pvCapacity},
		{Header: "ACCESS MODES", Width: 16, Render: pvAccessModes},
		{Header: "STATUS", Width: 12, Render: pvStatusCell},
		{Header: "AGE", Width: 10, Render: pvAge},
	}
}

func (pv persistentVolume) List(c Context) ([]k8s.ResourceRow, error) { return listVia(pv, c) }

func (persistentVolume) RowStatus(o runtime.Object) string {
	return string(o.(*corev1.PersistentVolume).Status.Phase)
}

func pvName(o runtime.Object, _ k8s.RowContext) string { return o.(*corev1.PersistentVolume).Name }

func pvCapacity(o runtime.Object, _ k8s.RowContext) string {
	p := o.(*corev1.PersistentVolume)
	if storage, ok := p.Spec.Capacity[corev1.ResourceStorage]; ok {
		return storage.String()
	}
	return ""
}

func pvAccessModes(o runtime.Object, _ k8s.RowContext) string {
	p := o.(*corev1.PersistentVolume)
	modes := make([]string, 0, len(p.Spec.AccessModes))
	for _, m := range p.Spec.AccessModes {
		modes = append(modes, string(m))
	}
	return strings.Join(modes, ",")
}

func pvStatusCell(o runtime.Object, _ k8s.RowContext) string {
	return string(o.(*corev1.PersistentVolume).Status.Phase)
}

func pvAge(o runtime.Object, _ k8s.RowContext) string {
	return k8s.AgeString(o.(*corev1.PersistentVolume).CreationTimestamp)
}

func (persistentVolume) Fetch(ctx context.Context, c Context, _, name string) (Object, error) {
	if c.Clientset == nil {
		return nil, fmt.Errorf("persistentVolume.Fetch: no Clientset")
	}
	return c.Clientset.CoreV1().PersistentVolumes().Get(ctx, name, metav1.GetOptions{})
}

func (persistentVolume) Delete(c Context, _, name string) error {
	if c.Clientset == nil {
		return fmt.Errorf("persistentVolume.Delete: no Clientset")
	}
	grace := int64(0)
	return c.Clientset.CoreV1().PersistentVolumes().Delete(c.Ctx, name, metav1.DeleteOptions{
		GracePeriodSeconds: &grace,
	})
}

func init() { register(persistentVolume{}) }
