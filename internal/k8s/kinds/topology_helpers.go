package kinds

import (
	"fmt"

	k8s "github.com/chaitanyak/klens/internal/k8s"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

// listTyped is the per-kind unwrapping helper every Topologer reaches for.
// It calls the Lister for `gvr` in `ns`, filters the slice to *T, and returns
// the typed slice — saving every caller from re-typing the cast loop.
func listTyped[T runtime.Object](c Context, gvr schema.GroupVersionResource, ns string) ([]T, error) {
	if c.Lister == nil {
		return nil, fmt.Errorf("listTyped[%T]: no Lister", *new(T))
	}
	objs, err := c.Lister.List(c.Ctx, gvr, ns)
	if err != nil {
		return nil, err
	}
	out := make([]T, 0, len(objs))
	for _, o := range objs {
		if t, ok := o.(T); ok {
			out = append(out, t)
		}
	}
	return out, nil
}

// ownedByUID reports whether any owner reference points at `uid`. Used by
// the Deployment → ReplicaSet → Pod walk and similar parent-child chains.
func ownedByUID(refs []metav1.OwnerReference, uid types.UID) bool {
	for _, ref := range refs {
		if ref.UID == uid {
			return true
		}
	}
	return false
}

// matchesLabelSelector is the trivial subset-match Service / Ingress use.
// Returns false if any selector key is missing from labels or has a
// different value.
func matchesLabelSelector(labels, selector map[string]string) bool {
	for k, v := range selector {
		if labels[k] != v {
			return false
		}
	}
	return true
}

// podTopologyStatus renders the status string used in the topology tree:
// "Terminating" if the pod is being deleted, otherwise the lifecycle phase.
func podTopologyStatus(p *corev1.Pod) string {
	if p.DeletionTimestamp != nil {
		return "Terminating"
	}
	return string(p.Status.Phase)
}

// deploymentTopologyStatus and replicaSetStatus are the two status strings
// the Deployment topology tree renders for its non-Pod nodes.
func deploymentTopologyStatus(d *appsv1.Deployment) string {
	if d.Spec.Replicas != nil && d.Status.AvailableReplicas == *d.Spec.Replicas {
		return "Running"
	}
	return "Pending"
}

func replicaSetTopologyStatus(rs *appsv1.ReplicaSet) string {
	desired := int32(0)
	if rs.Spec.Replicas != nil {
		desired = *rs.Spec.Replicas
	}
	if rs.Status.ReadyReplicas == desired {
		return "Ready"
	}
	return "Pending"
}

// podTreeNode wraps the leaf-pod rendering every Topologer needs.
func podTreeNode(p *corev1.Pod) *k8s.TreeNode {
	return &k8s.TreeNode{Kind: "Pod", Name: p.Name, Status: podTopologyStatus(p)}
}
