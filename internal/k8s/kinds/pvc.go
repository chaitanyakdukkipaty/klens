package kinds

import (
	"context"
	"fmt"

	k8s "github.com/chaitanyak/klens/internal/k8s"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
		{Header: "NAME", Width: 40, Flex: true},
		{Header: "STATUS", Width: 12},
		{Header: "VOLUME", Width: 30},
		{Header: "CAPACITY", Width: 12},
		{Header: "AGE", Width: 10},
	}
}

func (pvc persistentVolumeClaim) List(c Context) ([]Row, error) {
	pvcs, err := listTyped[*corev1.PersistentVolumeClaim](c, pvc.Meta().GVR, c.Namespace)
	if err != nil {
		return nil, err
	}
	rows := make([]Row, 0, len(pvcs))
	for _, p := range pvcs {
		cap := ""
		if storage, ok := p.Status.Capacity[corev1.ResourceStorage]; ok {
			cap = storage.String()
		}
		phase := string(p.Status.Phase)
		rows = append(rows, Row{
			Name:      p.Name,
			Namespace: p.Namespace,
			Status:    phase,
			Values:    []string{p.Name, phase, p.Spec.VolumeName, cap, k8s.AgeString(p.CreationTimestamp)},
			Raw:       p,
		})
	}
	return rows, nil
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
