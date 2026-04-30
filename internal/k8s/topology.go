package k8s

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TreeNode is a node in the resource topology tree.
type TreeNode struct {
	Kind     string
	Name     string
	Status   string
	Children []*TreeNode
}

// BuildDeploymentTopology builds a Deployment → ReplicaSet → Pod tree.
func BuildDeploymentTopology(deploy *appsv1.Deployment, wf *WatcherFactory) *TreeNode {
	root := &TreeNode{
		Kind:   "Deployment",
		Name:   deploy.Name,
		Status: deployTopologyStatus(deploy),
	}

	rss := wf.ListReplicaSets(deploy.Namespace)
	for _, rs := range rss {
		if !ownedBy(rs.OwnerReferences, deploy.UID) {
			continue
		}
		rsNode := &TreeNode{
			Kind:   "ReplicaSet",
			Name:   rs.Name,
			Status: rsStatus(rs),
		}
		pods := wf.ListPods(deploy.Namespace)
		for _, pod := range pods {
			if !ownedBy(pod.OwnerReferences, rs.UID) {
				continue
			}
			rsNode.Children = append(rsNode.Children, &TreeNode{
				Kind:   "Pod",
				Name:   pod.Name,
				Status: podTopologyStatus(pod),
			})
		}
		root.Children = append(root.Children, rsNode)
	}
	return root
}

// BuildServiceTopology builds a Service → Endpoints → Pod tree.
func BuildServiceTopology(svc *corev1.Service, wf *WatcherFactory) *TreeNode {
	root := &TreeNode{
		Kind:   "Service",
		Name:   svc.Name,
		Status: string(svc.Spec.Type),
	}

	// Find pods matching the service selector
	if svc.Spec.Selector == nil {
		return root
	}
	pods := wf.ListPods(svc.Namespace)
	for _, pod := range pods {
		if matchesSelector(pod.Labels, svc.Spec.Selector) {
			root.Children = append(root.Children, &TreeNode{
				Kind:   "Pod",
				Name:   pod.Name,
				Status: podTopologyStatus(pod),
			})
		}
	}
	return root
}

// BuildIngressTopology builds an Ingress → Rule → Route → Service → Pod tree.
func BuildIngressTopology(ing *networkingv1.Ingress, wf *WatcherFactory) *TreeNode {
	totalRoutes := 0
	for _, r := range ing.Spec.Rules {
		if r.HTTP != nil {
			totalRoutes += len(r.HTTP.Paths)
		}
	}
	root := &TreeNode{
		Kind:   "Ingress",
		Name:   ing.Name,
		Status: fmt.Sprintf("%d routes", totalRoutes),
	}

	svcs := wf.ListServices(ing.Namespace)
	pods := wf.ListPods(ing.Namespace)

	for _, rule := range ing.Spec.Rules {
		host := rule.Host
		if host == "" {
			host = "*"
		}
		ruleNode := &TreeNode{Kind: "Rule", Name: host}
		if rule.HTTP == nil {
			root.Children = append(root.Children, ruleNode)
			continue
		}
		for _, path := range rule.HTTP.Paths {
			svcBackend := path.Backend.Service
			if svcBackend == nil {
				continue
			}
			routeNode := &TreeNode{
				Kind: "Route",
				Name: fmt.Sprintf("%s → %s:%d", path.Path, svcBackend.Name, svcBackend.Port.Number),
			}
			for _, svc := range svcs {
				if svc.Name != svcBackend.Name {
					continue
				}
				svcNode := &TreeNode{
					Kind:   "Service",
					Name:   svc.Name,
					Status: string(svc.Spec.Type),
				}
				if svc.Spec.Selector != nil {
					for _, pod := range pods {
						if matchesSelector(pod.Labels, svc.Spec.Selector) {
							svcNode.Children = append(svcNode.Children, &TreeNode{
								Kind:   "Pod",
								Name:   pod.Name,
								Status: podTopologyStatus(pod),
							})
						}
					}
				}
				routeNode.Children = append(routeNode.Children, svcNode)
				break
			}
			ruleNode.Children = append(ruleNode.Children, routeNode)
		}
		root.Children = append(root.Children, ruleNode)
	}
	return root
}

func ownedBy(refs []metav1.OwnerReference, uid interface{}) bool {
	for _, ref := range refs {
		if string(ref.UID) == fmt.Sprint(uid) {
			return true
		}
	}
	return false
}

func matchesSelector(labels, selector map[string]string) bool {
	for k, v := range selector {
		if labels[k] != v {
			return false
		}
	}
	return true
}

func deployTopologyStatus(d *appsv1.Deployment) string {
	if d.Status.AvailableReplicas == *d.Spec.Replicas {
		return "Running"
	}
	return "Pending"
}

func rsStatus(rs *appsv1.ReplicaSet) string {
	desired := int32(0)
	if rs.Spec.Replicas != nil {
		desired = *rs.Spec.Replicas
	}
	if rs.Status.ReadyReplicas == desired {
		return "Ready"
	}
	return "Pending"
}

func podTopologyStatus(pod *corev1.Pod) string {
	if pod.DeletionTimestamp != nil {
		return "Terminating"
	}
	return string(pod.Status.Phase)
}

// ResolvePodNames returns the pod names that back the named resource.
// For Pod it returns the name directly. For Deployment it walks the
// Deployment → ReplicaSet → Pod ownership chain. For StatefulSet,
// DaemonSet, and Job it matches pod owner refs directly. For ReplicaSet
// it resolves via UID to avoid name collisions after recreation.
func ResolvePodNames(kind, name, namespace string, wf *WatcherFactory) []string {
	switch kind {
	case "Pod":
		return []string{name}

	case "Deployment":
		deps := wf.ListDeployments(namespace)
		var depUID interface{}
		for _, d := range deps {
			if d.Name == name {
				depUID = d.UID
				break
			}
		}
		if depUID == nil {
			return nil
		}
		var podNames []string
		for _, rs := range wf.ListReplicaSets(namespace) {
			if !ownedBy(rs.OwnerReferences, depUID) {
				continue
			}
			for _, pod := range wf.ListPods(namespace) {
				if ownedBy(pod.OwnerReferences, rs.UID) {
					podNames = append(podNames, pod.Name)
				}
			}
		}
		return podNames

	case "StatefulSet", "DaemonSet", "Job":
		var names []string
		for _, pod := range wf.ListPods(namespace) {
			for _, ref := range pod.OwnerReferences {
				if ref.Kind == kind && ref.Name == name {
					names = append(names, pod.Name)
					break
				}
			}
		}
		return names

	case "ReplicaSet":
		var rsUID interface{}
		for _, rs := range wf.ListReplicaSets(namespace) {
			if rs.Name == name {
				rsUID = rs.UID
				break
			}
		}
		if rsUID == nil {
			return nil
		}
		var names []string
		for _, pod := range wf.ListPods(namespace) {
			if ownedBy(pod.OwnerReferences, rsUID) {
				names = append(names, pod.Name)
			}
		}
		return names
	}
	return nil
}

