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

// statefulSet implements Scaler + Deleter + Logger + Applier.
type statefulSet struct{}

func (statefulSet) Meta() Meta {
	return Meta{
		Kind:       "StatefulSet",
		Plural:     "statefulsets",
		Aliases:    []string{"sts"},
		Namespaced: true,
		GVR:        k8s.StatefulSetGVR,
	}
}

func (statefulSet) Columns() []k8s.Column {
	return []k8s.Column{
		{Header: "NAME", Width: 40, Flex: true, Render: statefulSetName},
		{Header: "READY", Width: 10, Render: statefulSetReady},
		{Header: "AGE", Width: 10, Render: statefulSetAge},
	}
}

func (s statefulSet) List(c Context) ([]k8s.ResourceRow, error) { return listVia(s, c) }

func statefulSetName(o runtime.Object, _ k8s.RowContext) string {
	return o.(*appsv1.StatefulSet).Name
}

func statefulSetReady(o runtime.Object, _ k8s.RowContext) string {
	s := o.(*appsv1.StatefulSet)
	desired := int32(0)
	if s.Spec.Replicas != nil {
		desired = *s.Spec.Replicas
	}
	return fmt.Sprintf("%d/%d", s.Status.ReadyReplicas, desired)
}

func statefulSetAge(o runtime.Object, _ k8s.RowContext) string {
	return k8s.AgeString(o.(*appsv1.StatefulSet).CreationTimestamp)
}

func (statefulSet) Fetch(ctx context.Context, c Context, ns, name string) (Object, error) {
	if c.Clientset == nil {
		return nil, fmt.Errorf("statefulSet.Fetch: no Clientset")
	}
	return c.Clientset.AppsV1().StatefulSets(ns).Get(ctx, name, metav1.GetOptions{})
}

func (statefulSet) Delete(c Context, ns, name string) error {
	if c.Clientset == nil {
		return fmt.Errorf("statefulSet.Delete: no Clientset")
	}
	grace := int64(0)
	return c.Clientset.AppsV1().StatefulSets(ns).Delete(c.Ctx, name, metav1.DeleteOptions{GracePeriodSeconds: &grace})
}

func (statefulSet) Apply(c Context, ns, name string, body []byte) error {
	if c.Clientset == nil {
		return fmt.Errorf("statefulSet.Apply: no Clientset")
	}
	_, err := c.Clientset.AppsV1().StatefulSets(ns).Patch(
		c.Ctx, name, types.MergePatchType, body,
		metav1.PatchOptions{FieldManager: "klens"})
	return err
}

func (statefulSet) Scale(c Context, ns, name string, replicas int32) error {
	if c.Clientset == nil {
		return fmt.Errorf("statefulSet.Scale: no Clientset")
	}
	body, _ := json.Marshal(map[string]any{"spec": map[string]any{"replicas": replicas}})
	_, err := c.Clientset.AppsV1().StatefulSets(ns).Patch(
		c.Ctx, name, types.MergePatchType, body, metav1.PatchOptions{})
	return err
}

func (statefulSet) CurrentReplicas(obj Object) int32 {
	if s, ok := obj.(*appsv1.StatefulSet); ok && s.Spec.Replicas != nil {
		return *s.Spec.Replicas
	}
	return 1
}

// LogTargets matches pods whose owner reference points at this StatefulSet
// by Kind+Name (StatefulSet doesn't use a controller indirection like
// Deployment's ReplicaSet).
func (statefulSet) LogTargets(c Context, ns, name string) ([]LogTarget, error) {
	pods, err := listTyped[*corev1.Pod](c, k8s.PodGVR, ns)
	if err != nil {
		return nil, err
	}
	var out []LogTarget
	for _, p := range pods {
		for _, ref := range p.OwnerReferences {
			if ref.Kind == "StatefulSet" && ref.Name == name {
				out = append(out, LogTarget{Namespace: p.Namespace, Pod: p.Name})
				break
			}
		}
	}
	return out, nil
}

func init() { register(statefulSet{}) }
