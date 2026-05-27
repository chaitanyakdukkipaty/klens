package k8s

import (
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// TreeNode is a node in the resource topology tree. Each Kind that
// implements Topologer (internal/k8s/kinds) builds a tree of these.
type TreeNode struct {
	Kind     string
	Name     string
	Status   string
	Children []*TreeNode
}

// ResolvePodNames returns the pod names that back the named resource.
// For Pod it returns the name directly. For Deployment it walks the
// Deployment → ReplicaSet → Pod ownership chain. For StatefulSet,
// DaemonSet, and Job it matches pod owner refs directly. For ReplicaSet
// it resolves via UID to avoid name collisions after recreation.
//
// Used by the multi-pod log streaming path (model.go::actionLogs).
func ResolvePodNames(kind, name, namespace string, wf *WatcherFactory) []string {
	switch kind {
	case "Pod":
		return []string{name}

	case "Deployment":
		deps := ListAs[*appsv1.Deployment](wf, "Deployment", namespace)
		var depUID types.UID
		for _, d := range deps {
			if d.Name == name {
				depUID = d.UID
				break
			}
		}
		if depUID == "" {
			return nil
		}
		pods := ListAs[*corev1.Pod](wf, "Pod", namespace)
		var podNames []string
		for _, rs := range ListAs[*appsv1.ReplicaSet](wf, "ReplicaSet", namespace) {
			if !ownedBy(rs.OwnerReferences, depUID) {
				continue
			}
			for _, pod := range pods {
				if ownedBy(pod.OwnerReferences, rs.UID) {
					podNames = append(podNames, pod.Name)
				}
			}
		}
		return podNames

	case "StatefulSet", "DaemonSet", "Job":
		var names []string
		for _, pod := range ListAs[*corev1.Pod](wf, "Pod", namespace) {
			for _, ref := range pod.OwnerReferences {
				if ref.Kind == kind && ref.Name == name {
					names = append(names, pod.Name)
					break
				}
			}
		}
		return names

	case "ReplicaSet":
		var rsUID types.UID
		for _, rs := range ListAs[*appsv1.ReplicaSet](wf, "ReplicaSet", namespace) {
			if rs.Name == name {
				rsUID = rs.UID
				break
			}
		}
		if rsUID == "" {
			return nil
		}
		var names []string
		for _, pod := range ListAs[*corev1.Pod](wf, "Pod", namespace) {
			if ownedBy(pod.OwnerReferences, rsUID) {
				names = append(names, pod.Name)
			}
		}
		return names
	}
	return nil
}

func ownedBy(refs []metav1.OwnerReference, uid types.UID) bool {
	for _, ref := range refs {
		if ref.UID == uid {
			return true
		}
	}
	return false
}
