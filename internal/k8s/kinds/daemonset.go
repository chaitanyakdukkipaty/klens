package kinds

import (
	"context"
	"fmt"

	k8s "github.com/chaitanyak/klens/internal/k8s"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// daemonSet implements Deleter + Logger + Applier. No Scaler (DaemonSets are
// scheduled per-node, not by replicas).
type daemonSet struct{}

func (daemonSet) Meta() Meta {
	return Meta{
		Kind:       "DaemonSet",
		Plural:     "daemonsets",
		Aliases:    []string{"ds"},
		Namespaced: true,
		GVR:        k8s.DaemonSetGVR,
	}
}

func (daemonSet) Columns() []k8s.Column {
	return []k8s.Column{
		{Header: "NAME", Width: 40, Flex: true},
		{Header: "DESIRED", Width: 10},
		{Header: "READY", Width: 8},
		{Header: "UP-TO-DATE", Width: 12},
		{Header: "AGE", Width: 10},
	}
}

func (d daemonSet) List(c Context) ([]Row, error) {
	sets, err := listTyped[*appsv1.DaemonSet](c, d.Meta().GVR, c.Namespace)
	if err != nil {
		return nil, err
	}
	rows := make([]Row, 0, len(sets))
	for _, ds := range sets {
		rows = append(rows, Row{
			Name:      ds.Name,
			Namespace: ds.Namespace,
			Values: []string{
				ds.Name,
				fmt.Sprintf("%d", ds.Status.DesiredNumberScheduled),
				fmt.Sprintf("%d", ds.Status.NumberReady),
				fmt.Sprintf("%d", ds.Status.UpdatedNumberScheduled),
				k8s.AgeString(ds.CreationTimestamp),
			},
			Raw: ds,
		})
	}
	return rows, nil
}

func (daemonSet) Fetch(ctx context.Context, c Context, ns, name string) (Object, error) {
	if c.Clientset == nil {
		return nil, fmt.Errorf("daemonSet.Fetch: no Clientset")
	}
	return c.Clientset.AppsV1().DaemonSets(ns).Get(ctx, name, metav1.GetOptions{})
}

func (daemonSet) Delete(c Context, ns, name string) error {
	if c.Clientset == nil {
		return fmt.Errorf("daemonSet.Delete: no Clientset")
	}
	grace := int64(0)
	return c.Clientset.AppsV1().DaemonSets(ns).Delete(c.Ctx, name, metav1.DeleteOptions{GracePeriodSeconds: &grace})
}

func (daemonSet) Apply(c Context, ns, name string, body []byte) error {
	if c.Clientset == nil {
		return fmt.Errorf("daemonSet.Apply: no Clientset")
	}
	_, err := c.Clientset.AppsV1().DaemonSets(ns).Patch(
		c.Ctx, name, types.MergePatchType, body,
		metav1.PatchOptions{FieldManager: "klens"})
	return err
}

func (daemonSet) LogTargets(c Context, ns, name string) ([]LogTarget, error) {
	pods, err := listTyped[*corev1.Pod](c, k8s.PodGVR, ns)
	if err != nil {
		return nil, err
	}
	var out []LogTarget
	for _, p := range pods {
		for _, ref := range p.OwnerReferences {
			if ref.Kind == "DaemonSet" && ref.Name == name {
				out = append(out, LogTarget{Namespace: p.Namespace, Pod: p.Name})
				break
			}
		}
	}
	return out, nil
}

func init() { register(daemonSet{}) }
