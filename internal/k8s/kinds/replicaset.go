package kinds

import (
	"context"
	"encoding/json"
	"fmt"

	k8s "github.com/chaitanyak/klens/internal/k8s"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
)

// replicaSet implements Scaler + Deleter + Logger + XRayer. Apply is
// intentionally not implemented — RS pods are owned by their parent
// Deployment, so merge-patch through klens would race the controller.
type replicaSet struct{}

func (replicaSet) Meta() Meta {
	return Meta{
		Kind:       "ReplicaSet",
		Group:      "Workloads",
		Plural:     "replicasets",
		Aliases:    []string{"rs"},
		Namespaced: true,
		GVR:        k8s.ReplicaSetGVR,
	}
}

func (replicaSet) Columns() []k8s.Column {
	return []k8s.Column{
		{Header: "NAME", Width: 40, Flex: true, Render: replicaSetName},
		{Header: "DESIRED", Width: 10, Render: replicaSetDesired},
		{Header: "CURRENT", Width: 10, Render: replicaSetCurrent},
		{Header: "READY", Width: 8, Render: replicaSetReady},
		{Header: "AGE", Width: 10, Render: replicaSetAge},
	}
}

func (r replicaSet) List(c Context) ([]k8s.ResourceRow, error) { return listVia(r, c) }

func replicaSetName(o runtime.Object, _ k8s.RowContext) string { return o.(*appsv1.ReplicaSet).Name }

func replicaSetDesired(o runtime.Object, _ k8s.RowContext) string {
	s := o.(*appsv1.ReplicaSet)
	desired := int32(0)
	if s.Spec.Replicas != nil {
		desired = *s.Spec.Replicas
	}
	return fmt.Sprintf("%d", desired)
}

func replicaSetCurrent(o runtime.Object, _ k8s.RowContext) string {
	return fmt.Sprintf("%d", o.(*appsv1.ReplicaSet).Status.Replicas)
}

func replicaSetReady(o runtime.Object, _ k8s.RowContext) string {
	return fmt.Sprintf("%d", o.(*appsv1.ReplicaSet).Status.ReadyReplicas)
}

func replicaSetAge(o runtime.Object, _ k8s.RowContext) string {
	return k8s.AgeString(o.(*appsv1.ReplicaSet).CreationTimestamp)
}

func (replicaSet) Fetch(ctx context.Context, c Context, ns, name string) (Object, error) {
	if c.Clientset == nil {
		return nil, fmt.Errorf("replicaSet.Fetch: no Clientset")
	}
	return c.Clientset.AppsV1().ReplicaSets(ns).Get(ctx, name, metav1.GetOptions{})
}

func (replicaSet) Delete(c Context, ns, name string) error {
	if c.Clientset == nil {
		return fmt.Errorf("replicaSet.Delete: no Clientset")
	}
	grace := int64(0)
	return c.Clientset.AppsV1().ReplicaSets(ns).Delete(c.Ctx, name, metav1.DeleteOptions{GracePeriodSeconds: &grace})
}

func (replicaSet) Scale(c Context, ns, name string, replicas int32) error {
	if c.Clientset == nil {
		return fmt.Errorf("replicaSet.Scale: no Clientset")
	}
	body, _ := json.Marshal(map[string]any{"spec": map[string]any{"replicas": replicas}})
	_, err := c.Clientset.AppsV1().ReplicaSets(ns).Patch(
		c.Ctx, name, types.MergePatchType, body, metav1.PatchOptions{})
	return err
}

func (replicaSet) CurrentReplicas(obj Object) int32 {
	if s, ok := obj.(*appsv1.ReplicaSet); ok && s.Spec.Replicas != nil {
		return *s.Spec.Replicas
	}
	return 1
}

// LogTargets resolves the pods owned (via UID) by this ReplicaSet. The
// k8s.ResolvePodNames code resolves via UID rather than Name to avoid
// collisions after RS recreation; the migrated method preserves that.
func (r replicaSet) LogTargets(c Context, ns, name string) ([]LogTarget, error) {
	sets, err := listTyped[*appsv1.ReplicaSet](c, r.Meta().GVR, ns)
	if err != nil {
		return nil, err
	}
	var rsUID types.UID
	for _, s := range sets {
		if s.Name == name {
			rsUID = s.UID
			break
		}
	}
	if rsUID == "" {
		return nil, fmt.Errorf("replicaset %s/%s not in cache", ns, name)
	}
	pods, err := listTyped[*corev1.Pod](c, k8s.PodGVR, ns)
	if err != nil {
		return nil, err
	}
	var out []LogTarget
	for _, p := range pods {
		if ownedByUID(p.OwnerReferences, rsUID) {
			out = append(out, LogTarget{Namespace: p.Namespace, Pod: p.Name})
		}
	}
	return out, nil
}

// XRay walks pods owned by this ReplicaSet via owner reference UID.
func (r replicaSet) XRay(c Context, ns, name string) (*k8s.TreeNode, error) {
	sets, err := listTyped[*appsv1.ReplicaSet](c, r.Meta().GVR, ns)
	if err != nil {
		return nil, err
	}
	var rs *appsv1.ReplicaSet
	for _, s := range sets {
		if s.Name == name {
			rs = s
			break
		}
	}
	if rs == nil {
		return nil, nil
	}
	root := &k8s.TreeNode{Kind: "ReplicaSet", Name: rs.Name, Status: replicaSetXRayStatus(rs)}
	pods, err := listTyped[*corev1.Pod](c, k8s.PodGVR, ns)
	if err != nil {
		return root, err
	}
	for _, p := range pods {
		if ownedByUID(p.OwnerReferences, rs.UID) {
			root.Children = append(root.Children, podTreeNode(p))
		}
	}
	return root, nil
}

func init() { register(replicaSet{}) }
