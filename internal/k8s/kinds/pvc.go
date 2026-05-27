package kinds

import (
	"context"
	"fmt"

	k8s "github.com/chaitanyak/klens/internal/k8s"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// persistentVolumeClaim implements Deleter (no Applier in the legacy
// RegisterAction set).
type persistentVolumeClaim struct{}

func (persistentVolumeClaim) Meta() Meta {
	return Meta{
		Kind:       "PersistentVolumeClaim",
		Plural:     "persistentvolumeclaims",
		Aliases:    []string{"pvc"},
		Namespaced: true,
		GVR:        k8s.PersistentVolumeClaimGVR,
	}
}

func (persistentVolumeClaim) Columns() []k8s.Column {
	return []k8s.Column{
		{Header: "NAME", Width: 40, Flex: true, Render: pvcName},
		{Header: "STATUS", Width: 12, Render: pvcStatus},
		{Header: "VOLUME", Width: 30, Render: pvcVolume},
		{Header: "CAPACITY", Width: 12, Render: pvcCapacity},
		{Header: "AGE", Width: 10, Render: pvcAge},
	}
}

func (pvc persistentVolumeClaim) List(c Context) ([]k8s.ResourceRow, error) {
	return listVia(pvc, c)
}

func (persistentVolumeClaim) RowStatus(o runtime.Object) string {
	return string(o.(*corev1.PersistentVolumeClaim).Status.Phase)
}

func pvcName(o runtime.Object, _ k8s.RowContext) string {
	return o.(*corev1.PersistentVolumeClaim).Name
}

func pvcStatus(o runtime.Object, _ k8s.RowContext) string {
	return string(o.(*corev1.PersistentVolumeClaim).Status.Phase)
}

func pvcVolume(o runtime.Object, _ k8s.RowContext) string {
	return o.(*corev1.PersistentVolumeClaim).Spec.VolumeName
}

func pvcCapacity(o runtime.Object, _ k8s.RowContext) string {
	p := o.(*corev1.PersistentVolumeClaim)
	if storage, ok := p.Status.Capacity[corev1.ResourceStorage]; ok {
		return storage.String()
	}
	return ""
}

func pvcAge(o runtime.Object, _ k8s.RowContext) string {
	return k8s.AgeString(o.(*corev1.PersistentVolumeClaim).CreationTimestamp)
}

func (persistentVolumeClaim) Fetch(ctx context.Context, c Context, ns, name string) (Object, error) {
	if c.Clientset == nil {
		return nil, fmt.Errorf("persistentVolumeClaim.Fetch: no Clientset")
	}
	return c.Clientset.CoreV1().PersistentVolumeClaims(ns).Get(ctx, name, metav1.GetOptions{})
}

func (persistentVolumeClaim) Delete(c Context, ns, name string) error {
	if c.Clientset == nil {
		return fmt.Errorf("persistentVolumeClaim.Delete: no Clientset")
	}
	grace := int64(0)
	return c.Clientset.CoreV1().PersistentVolumeClaims(ns).Delete(c.Ctx, name, metav1.DeleteOptions{GracePeriodSeconds: &grace})
}

func init() { register(persistentVolumeClaim{}) }
