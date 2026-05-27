package kinds

import (
	"context"
	"encoding/json"
	"fmt"

	k8s "github.com/chaitanyak/klens/internal/k8s"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// deployment satisfies Scaler, Deleter, Logger, Applier, Topologer — the
// canonical workload kind.
type deployment struct{}

func (deployment) Meta() Meta {
	return Meta{
		Kind:       "Deployment",
		Plural:     "deployments",
		Aliases:    []string{"deploy", "dp"},
		Namespaced: true,
		GVR:        k8s.DeploymentGVR,
	}
}

func (deployment) Columns() []k8s.Column {
	return []k8s.Column{
		{Header: "NAME", Width: 40, Flex: true},
		{Header: "READY", Width: 10},
		{Header: "UP-TO-DATE", Width: 12},
		{Header: "AVAILABLE", Width: 12},
		{Header: "AGE", Width: 10},
	}
}

func (d deployment) List(c Context) ([]Row, error) {
	deps, err := listTyped[*appsv1.Deployment](c, d.Meta().GVR, c.Namespace)
	if err != nil {
		return nil, err
	}
	rows := make([]Row, 0, len(deps))
	for _, dp := range deps {
		desired := int32(0)
		if dp.Spec.Replicas != nil {
			desired = *dp.Spec.Replicas
		}
		status := "Pending"
		if dp.Status.AvailableReplicas == desired {
			status = "Running"
		}
		rows = append(rows, Row{
			Name:      dp.Name,
			Namespace: dp.Namespace,
			Status:    status,
			Values: []string{
				dp.Name,
				fmt.Sprintf("%d/%d", dp.Status.ReadyReplicas, desired),
				fmt.Sprintf("%d", dp.Status.UpdatedReplicas),
				fmt.Sprintf("%d", dp.Status.AvailableReplicas),
				k8s.AgeString(dp.CreationTimestamp),
			},
			Raw: dp,
		})
	}
	return rows, nil
}

func (deployment) Fetch(ctx context.Context, c Context, ns, name string) (Object, error) {
	if c.Clientset == nil {
		return nil, fmt.Errorf("deployment.Fetch: no Clientset")
	}
	return c.Clientset.AppsV1().Deployments(ns).Get(ctx, name, metav1.GetOptions{})
}

func (deployment) Delete(c Context, ns, name string) error {
	if c.Clientset == nil {
		return fmt.Errorf("deployment.Delete: no Clientset")
	}
	grace := int64(0)
	return c.Clientset.AppsV1().Deployments(ns).Delete(c.Ctx, name, metav1.DeleteOptions{
		GracePeriodSeconds: &grace,
	})
}

func (deployment) Apply(c Context, ns, name string, body []byte) error {
	if c.Clientset == nil {
		return fmt.Errorf("deployment.Apply: no Clientset")
	}
	_, err := c.Clientset.AppsV1().Deployments(ns).Patch(
		c.Ctx, name, types.MergePatchType, body,
		metav1.PatchOptions{FieldManager: "klens"})
	return err
}

func (deployment) Scale(c Context, ns, name string, replicas int32) error {
	if c.Clientset == nil {
		return fmt.Errorf("deployment.Scale: no Clientset")
	}
	body, _ := json.Marshal(map[string]any{"spec": map[string]any{"replicas": replicas}})
	_, err := c.Clientset.AppsV1().Deployments(ns).Patch(
		c.Ctx, name, types.MergePatchType, body, metav1.PatchOptions{})
	return err
}

func (deployment) CurrentReplicas(obj Object) int32 {
	if d, ok := obj.(*appsv1.Deployment); ok && d.Spec.Replicas != nil {
		return *d.Spec.Replicas
	}
	return 1
}

// LogTargets walks Deployment → ReplicaSet → Pod via owner refs and returns
// every backing pod. Mirrors k8s.ResolvePodNames("Deployment", ...) but uses
// the Lister so the kind owns its own log fan-out.
func (d deployment) LogTargets(c Context, ns, name string) ([]LogTarget, error) {
	deps, err := listTyped[*appsv1.Deployment](c, d.Meta().GVR, ns)
	if err != nil {
		return nil, err
	}
	var dep *appsv1.Deployment
	for _, x := range deps {
		if x.Name == name {
			dep = x
			break
		}
	}
	if dep == nil {
		return nil, fmt.Errorf("deployment %s/%s not in cache", ns, name)
	}
	rss, err := listTyped[*appsv1.ReplicaSet](c, k8s.ReplicaSetGVR, ns)
	if err != nil {
		return nil, err
	}
	pods, err := listTyped[*corev1.Pod](c, k8s.PodGVR, ns)
	if err != nil {
		return nil, err
	}
	var targets []LogTarget
	for _, rs := range rss {
		if !ownedByUID(rs.OwnerReferences, dep.UID) {
			continue
		}
		for _, p := range pods {
			if ownedByUID(p.OwnerReferences, rs.UID) {
				targets = append(targets, LogTarget{Namespace: p.Namespace, Pod: p.Name})
			}
		}
	}
	return targets, nil
}

func (d deployment) Topology(c Context, ns, name string) (*k8s.TreeNode, error) {
	deps, err := listTyped[*appsv1.Deployment](c, d.Meta().GVR, ns)
	if err != nil {
		return nil, err
	}
	var dep *appsv1.Deployment
	for _, x := range deps {
		if x.Name == name {
			dep = x
			break
		}
	}
	if dep == nil {
		return nil, nil
	}
	root := &k8s.TreeNode{Kind: "Deployment", Name: dep.Name, Status: deploymentTopologyStatus(dep)}
	rss, err := listTyped[*appsv1.ReplicaSet](c, k8s.ReplicaSetGVR, ns)
	if err != nil {
		return root, err
	}
	pods, err := listTyped[*corev1.Pod](c, k8s.PodGVR, ns)
	if err != nil {
		return root, err
	}
	for _, rs := range rss {
		if !ownedByUID(rs.OwnerReferences, dep.UID) {
			continue
		}
		rsNode := &k8s.TreeNode{Kind: "ReplicaSet", Name: rs.Name, Status: replicaSetTopologyStatus(rs)}
		for _, p := range pods {
			if ownedByUID(p.OwnerReferences, rs.UID) {
				rsNode.Children = append(rsNode.Children, podTreeNode(p))
			}
		}
		root.Children = append(root.Children, rsNode)
	}
	return root, nil
}

func init() { register(deployment{}) }
