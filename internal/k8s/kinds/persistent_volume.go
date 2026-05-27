package kinds

import (
	"context"
	"fmt"
	"strings"

	k8s "github.com/chaitanyak/klens/internal/k8s"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// persistentVolume is cluster-scoped, supports YAML view + delete.
type persistentVolume struct{}

func (persistentVolume) Meta() Meta {
	return Meta{
		Kind:       "PersistentVolume",
		Plural:     "persistentvolumes",
		Aliases:    []string{"pv"},
		Namespaced: false,
		GVR:        k8s.PersistentVolumeGVR,
	}
}

func (persistentVolume) Columns() []k8s.Column {
	return []k8s.Column{
		{Header: "NAME", Width: 40, Flex: true},
		{Header: "CAPACITY", Width: 12},
		{Header: "ACCESS MODES", Width: 16},
		{Header: "STATUS", Width: 12},
		{Header: "AGE", Width: 10},
	}
}

func (pv persistentVolume) List(c Context) ([]Row, error) {
	if c.Lister == nil {
		return nil, fmt.Errorf("persistentVolume.List: no Lister")
	}
	objs, err := c.Lister.List(c.Ctx, pv.Meta().GVR, "")
	if err != nil {
		return nil, err
	}
	rows := make([]Row, 0, len(objs))
	for _, o := range objs {
		p, ok := o.(*corev1.PersistentVolume)
		if !ok {
			continue
		}
		cap := ""
		if storage, ok := p.Spec.Capacity[corev1.ResourceStorage]; ok {
			cap = storage.String()
		}
		modes := make([]string, 0, len(p.Spec.AccessModes))
		for _, m := range p.Spec.AccessModes {
			modes = append(modes, string(m))
		}
		phase := string(p.Status.Phase)
		rows = append(rows, Row{
			Name:   p.Name,
			Status: phase,
			Values: []string{p.Name, cap, strings.Join(modes, ","), phase, k8s.AgeString(p.CreationTimestamp)},
			Raw:    p,
		})
	}
	return rows, nil
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
