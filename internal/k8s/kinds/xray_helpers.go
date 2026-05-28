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

// listTyped is the per-kind unwrapping helper every XRayer reaches for.
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

// podXRayStatus renders the status string used in the xray tree:
// "Terminating" if the pod is being deleted, otherwise the lifecycle phase.
func podXRayStatus(p *corev1.Pod) string {
	if p.DeletionTimestamp != nil {
		return "Terminating"
	}
	return string(p.Status.Phase)
}

// deploymentXRayStatus and replicaSetXRayStatus are the status strings the
// Deployment xray tree renders for its non-Pod nodes.
func deploymentXRayStatus(d *appsv1.Deployment) string {
	if d.Spec.Replicas != nil && d.Status.AvailableReplicas == *d.Spec.Replicas {
		return "Running"
	}
	return "Pending"
}

func replicaSetXRayStatus(rs *appsv1.ReplicaSet) string {
	desired := int32(0)
	if rs.Spec.Replicas != nil {
		desired = *rs.Spec.Replicas
	}
	if rs.Status.ReadyReplicas == desired {
		return "Ready"
	}
	return "Pending"
}

// podTreeNode wraps the leaf-pod rendering every XRayer needs.
func podTreeNode(p *corev1.Pod) *k8s.TreeNode {
	return &k8s.TreeNode{Kind: "Pod", Name: p.Name, Status: podXRayStatus(p)}
}

// lookupNode returns a tree node for a referenced object. When the referent
// is not in the informer cache, Status is "missing" so the renderer can flag
// it with a dim-red badge — the canonical "why is image-pull failing?" trail.
func lookupNode(kind, name string, present bool) *k8s.TreeNode {
	n := &k8s.TreeNode{Kind: kind, Name: name}
	if !present {
		n.Status = "missing"
	}
	return n
}

// containerXRayNode builds the Container subtree: env / envFrom Secret and
// ConfigMap references, deduped by (kind, name) and marked "missing" when the
// referent is absent from the informer cache. variant is "init",
// "ephemeral", or "" for a regular container; it disambiguates the rendered
// name so init/main/ephemeral don't collide visually. status is the runtime
// state badge — empty when the container hasn't reported one yet.
func containerXRayNode(ctr corev1.Container, variant, status string, cms []*corev1.ConfigMap, secs []*corev1.Secret) *k8s.TreeNode {
	name := ctr.Name
	if variant != "" {
		name = fmt.Sprintf("%s (%s)", ctr.Name, variant)
	}
	node := &k8s.TreeNode{Kind: "Container", Name: name, Status: status}

	cmNames := configMapNames(cms)
	secNames := secretNames(secs)
	seen := make(map[string]bool, 8)
	add := func(kind, ref string, present bool) {
		key := kind + "/" + ref
		if seen[key] || ref == "" {
			return
		}
		seen[key] = true
		node.Children = append(node.Children, lookupNode(kind, ref, present))
	}

	for _, ef := range ctr.EnvFrom {
		if ef.ConfigMapRef != nil {
			add("ConfigMap", ef.ConfigMapRef.Name, cmNames[ef.ConfigMapRef.Name])
		}
		if ef.SecretRef != nil {
			add("Secret", ef.SecretRef.Name, secNames[ef.SecretRef.Name])
		}
	}
	for _, e := range ctr.Env {
		if e.ValueFrom == nil {
			continue
		}
		if e.ValueFrom.ConfigMapKeyRef != nil {
			add("ConfigMap", e.ValueFrom.ConfigMapKeyRef.Name, cmNames[e.ValueFrom.ConfigMapKeyRef.Name])
		}
		if e.ValueFrom.SecretKeyRef != nil {
			add("Secret", e.ValueFrom.SecretKeyRef.Name, secNames[e.ValueFrom.SecretKeyRef.Name])
		}
	}
	return node
}

// containerStateStatus returns a badge-friendly status for a container's
// runtime state. Mirrors what kubectl describe surfaces: the terminated
// reason (e.g. "Completed", "Error"), the waiting reason (e.g.
// "CrashLoopBackOff"), "Running" while running, or empty when no status
// has been reported yet.
func containerStateStatus(s corev1.ContainerStatus) string {
	switch {
	case s.State.Running != nil:
		return "Running"
	case s.State.Terminated != nil:
		if r := s.State.Terminated.Reason; r != "" {
			return r
		}
		return "Terminated"
	case s.State.Waiting != nil:
		if r := s.State.Waiting.Reason; r != "" {
			return r
		}
		return "Waiting"
	}
	return ""
}

// containerStatusByName indexes a ContainerStatus slice by container name so
// the XRay builder can attach runtime state to each Container node without
// an O(N×M) inner scan.
func containerStatusByName(statuses []corev1.ContainerStatus) map[string]string {
	out := make(map[string]string, len(statuses))
	for _, s := range statuses {
		out[s.Name] = containerStateStatus(s)
	}
	return out
}

func configMapNames(cms []*corev1.ConfigMap) map[string]bool {
	out := make(map[string]bool, len(cms))
	for _, cm := range cms {
		out[cm.Name] = true
	}
	return out
}

func secretNames(secs []*corev1.Secret) map[string]bool {
	out := make(map[string]bool, len(secs))
	for _, s := range secs {
		out[s.Name] = true
	}
	return out
}

func pvcNames(pvcs []*corev1.PersistentVolumeClaim) map[string]bool {
	out := make(map[string]bool, len(pvcs))
	for _, p := range pvcs {
		out[p.Name] = true
	}
	return out
}

func serviceAccountNames(sas []*corev1.ServiceAccount) map[string]bool {
	out := make(map[string]bool, len(sas))
	for _, sa := range sas {
		out[sa.Name] = true
	}
	return out
}

// buildVolumeXRayNode walks pp.Spec.Volumes, emitting one ConfigMap /
// Secret / PersistentVolumeClaim child per source. Projected sources expand
// into one entry per inner source. EmptyDir / HostPath / etc. are skipped —
// there's no resource to point at. Returns nil when no volume references
// exist so the caller can omit the empty "Volume" parent.
func buildVolumeXRayNode(pp *corev1.Pod, cms []*corev1.ConfigMap, secs []*corev1.Secret, pvcs []*corev1.PersistentVolumeClaim) *k8s.TreeNode {
	cmNames := configMapNames(cms)
	secNames := secretNames(secs)
	pvcN := pvcNames(pvcs)

	parent := &k8s.TreeNode{Kind: "Volume"}
	seen := make(map[string]bool, len(pp.Spec.Volumes))
	add := func(kind, ref string, present bool) {
		key := kind + "/" + ref
		if seen[key] || ref == "" {
			return
		}
		seen[key] = true
		parent.Children = append(parent.Children, lookupNode(kind, ref, present))
	}
	for _, v := range pp.Spec.Volumes {
		switch {
		case v.ConfigMap != nil:
			add("ConfigMap", v.ConfigMap.Name, cmNames[v.ConfigMap.Name])
		case v.Secret != nil:
			add("Secret", v.Secret.SecretName, secNames[v.Secret.SecretName])
		case v.PersistentVolumeClaim != nil:
			add("PersistentVolumeClaim", v.PersistentVolumeClaim.ClaimName, pvcN[v.PersistentVolumeClaim.ClaimName])
		case v.Projected != nil:
			for _, src := range v.Projected.Sources {
				if src.ConfigMap != nil {
					add("ConfigMap", src.ConfigMap.Name, cmNames[src.ConfigMap.Name])
				}
				if src.Secret != nil {
					add("Secret", src.Secret.Name, secNames[src.Secret.Name])
				}
			}
		}
	}
	if len(parent.Children) == 0 {
		return nil
	}
	return parent
}
