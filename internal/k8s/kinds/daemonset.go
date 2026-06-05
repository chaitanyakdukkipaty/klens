package kinds

import (
	"context"
	"fmt"

	k8s "github.com/chaitanyak/klens/internal/k8s"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
)

// daemonSet implements Deleter + Logger + Applier + XRayer. No Scaler
// (DaemonSets are scheduled per-node, not by replicas).
type daemonSet struct{}

func (daemonSet) Meta() Meta {
	return Meta{
		Kind:       "DaemonSet",
		Group:      "Workloads",
		Plural:     "daemonsets",
		Aliases:    []string{"ds"},
		Namespaced: true,
		GVR:        k8s.DaemonSetGVR,
	}
}

func (daemonSet) Columns() []k8s.Column {
	return []k8s.Column{
		{Header: "NAME", Width: 40, Flex: true, Render: daemonSetName},
		{Header: "DESIRED", Width: 10, Render: daemonSetDesired},
		{Header: "READY", Width: 8, Render: daemonSetReady},
		{Header: "UP-TO-DATE", Width: 12, Render: daemonSetUpToDate},
		{Header: "AGE", Width: 10, Render: daemonSetAge},
	}
}

func (d daemonSet) List(c Context) ([]k8s.ResourceRow, error) { return listVia(d, c) }

func daemonSetName(o runtime.Object, _ k8s.RowContext) string { return o.(*appsv1.DaemonSet).Name }

func daemonSetDesired(o runtime.Object, _ k8s.RowContext) string {
	return fmt.Sprintf("%d", o.(*appsv1.DaemonSet).Status.DesiredNumberScheduled)
}

func daemonSetReady(o runtime.Object, _ k8s.RowContext) string {
	return fmt.Sprintf("%d", o.(*appsv1.DaemonSet).Status.NumberReady)
}

func daemonSetUpToDate(o runtime.Object, _ k8s.RowContext) string {
	return fmt.Sprintf("%d", o.(*appsv1.DaemonSet).Status.UpdatedNumberScheduled)
}

func daemonSetAge(o runtime.Object, _ k8s.RowContext) string {
	return k8s.AgeString(o.(*appsv1.DaemonSet).CreationTimestamp)
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

// XRay walks pods owned by this DaemonSet via owner reference UID.
func (d daemonSet) XRay(c Context, ns, name string) (*k8s.TreeNode, error) {
	sets, err := listTyped[*appsv1.DaemonSet](c, d.Meta().GVR, ns)
	if err != nil {
		return nil, err
	}
	var ds *appsv1.DaemonSet
	for _, x := range sets {
		if x.Name == name {
			ds = x
			break
		}
	}
	if ds == nil {
		return nil, nil
	}
	root := &k8s.TreeNode{Kind: "DaemonSet", Name: ds.Name}
	pods, err := listTyped[*corev1.Pod](c, k8s.PodGVR, ns)
	if err != nil {
		return root, err
	}
	for _, p := range pods {
		if ownedByUID(p.OwnerReferences, ds.UID) {
			root.Children = append(root.Children, podTreeNode(p))
		}
	}
	return root, nil
}

func init() { register(daemonSet{}) }
