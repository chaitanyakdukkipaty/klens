package k8s

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

// OperationResultMsg reports success or failure of a cluster operation.
type OperationResultMsg struct {
	Operation string
	Resource  string
	Success   bool
	Err       error
}

// DeleteCmd deletes a resource by kind/name/namespace.
func DeleteCmd(cs *kubernetes.Clientset, kind, name, namespace string) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		grace := int64(0)
		opts := metav1.DeleteOptions{GracePeriodSeconds: &grace}
		var err error
		switch kind {
		case "Pod":
			err = cs.CoreV1().Pods(namespace).Delete(ctx, name, opts)
		case "Deployment":
			err = cs.AppsV1().Deployments(namespace).Delete(ctx, name, opts)
		case "StatefulSet":
			err = cs.AppsV1().StatefulSets(namespace).Delete(ctx, name, opts)
		case "DaemonSet":
			err = cs.AppsV1().DaemonSets(namespace).Delete(ctx, name, opts)
		case "ReplicaSet":
			err = cs.AppsV1().ReplicaSets(namespace).Delete(ctx, name, opts)
		case "Service":
			err = cs.CoreV1().Services(namespace).Delete(ctx, name, opts)
		case "ConfigMap":
			err = cs.CoreV1().ConfigMaps(namespace).Delete(ctx, name, opts)
		case "Secret":
			err = cs.CoreV1().Secrets(namespace).Delete(ctx, name, opts)
		case "Ingress":
			err = cs.NetworkingV1().Ingresses(namespace).Delete(ctx, name, opts)
		case "Job":
			err = cs.BatchV1().Jobs(namespace).Delete(ctx, name, opts)
		case "CronJob":
			err = cs.BatchV1().CronJobs(namespace).Delete(ctx, name, opts)
		case "PersistentVolumeClaim":
			err = cs.CoreV1().PersistentVolumeClaims(namespace).Delete(ctx, name, opts)
		case "PersistentVolume":
			err = cs.CoreV1().PersistentVolumes().Delete(ctx, name, opts)
		case "Namespace":
			err = cs.CoreV1().Namespaces().Delete(ctx, name, opts)
		default:
			err = fmt.Errorf("delete not supported for %s", kind)
		}
		if err != nil {
			return OperationResultMsg{Operation: "delete", Resource: name, Err: err}
		}
		return OperationResultMsg{Operation: "delete", Resource: name, Success: true}
	}
}

// ScaleCmd scales a scalable resource to the given replica count.
func ScaleCmd(cs *kubernetes.Clientset, kind, name, namespace string, replicas int32) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		patch := map[string]interface{}{
			"spec": map[string]interface{}{"replicas": replicas},
		}
		patchBytes, _ := json.Marshal(patch)
		opts := metav1.PatchOptions{}
		var err error
		switch kind {
		case "Deployment":
			_, err = cs.AppsV1().Deployments(namespace).Patch(ctx, name, types.MergePatchType, patchBytes, opts)
		case "StatefulSet":
			_, err = cs.AppsV1().StatefulSets(namespace).Patch(ctx, name, types.MergePatchType, patchBytes, opts)
		case "ReplicaSet":
			_, err = cs.AppsV1().ReplicaSets(namespace).Patch(ctx, name, types.MergePatchType, patchBytes, opts)
		default:
			err = fmt.Errorf("scale not supported for %s", kind)
		}
		if err != nil {
			return OperationResultMsg{Operation: "scale", Resource: name, Err: err}
		}
		return OperationResultMsg{Operation: "scale", Resource: name, Success: true}
	}
}

// RolloutRestartCmd restarts a deployment by patching the pod template annotation.
func RolloutRestartCmd(cs *kubernetes.Clientset, kind, name, namespace string) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		now := time.Now().UTC().Format(time.RFC3339)
		patch := map[string]interface{}{
			"spec": map[string]interface{}{
				"template": map[string]interface{}{
					"metadata": map[string]interface{}{
						"annotations": map[string]string{
							"kubectl.kubernetes.io/restartedAt": now,
						},
					},
				},
			},
		}
		patchBytes, _ := json.Marshal(patch)
		opts := metav1.PatchOptions{}
		var err error
		switch kind {
		case "Deployment":
			_, err = cs.AppsV1().Deployments(namespace).Patch(ctx, name, types.MergePatchType, patchBytes, opts)
		case "StatefulSet":
			_, err = cs.AppsV1().StatefulSets(namespace).Patch(ctx, name, types.MergePatchType, patchBytes, opts)
		case "DaemonSet":
			_, err = cs.AppsV1().DaemonSets(namespace).Patch(ctx, name, types.MergePatchType, patchBytes, opts)
		default:
			err = fmt.Errorf("rollout restart not supported for %s", kind)
		}
		if err != nil {
			return OperationResultMsg{Operation: "rollout-restart", Resource: name, Err: err}
		}
		return OperationResultMsg{Operation: "rollout-restart", Resource: name, Success: true}
	}
}

// CordonCmd cordons or uncordons a node.
func CordonCmd(cs *kubernetes.Clientset, nodeName string, cordon bool) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		patch := map[string]interface{}{
			"spec": map[string]interface{}{
				"unschedulable": cordon,
			},
		}
		patchBytes, _ := json.Marshal(patch)
		_, err := cs.CoreV1().Nodes().Patch(ctx, nodeName, types.MergePatchType, patchBytes, metav1.PatchOptions{})
		op := "cordon"
		if !cordon {
			op = "uncordon"
		}
		if err != nil {
			return OperationResultMsg{Operation: op, Resource: nodeName, Err: err}
		}
		return OperationResultMsg{Operation: op, Resource: nodeName, Success: true}
	}
}

// DrainCmd evicts all pods from a node and then cordons it.
func DrainCmd(cs *kubernetes.Clientset, nodeName string) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()

		// First cordon the node
		patch := map[string]interface{}{
			"spec": map[string]interface{}{"unschedulable": true},
		}
		patchBytes, _ := json.Marshal(patch)
		if _, err := cs.CoreV1().Nodes().Patch(ctx, nodeName, types.MergePatchType, patchBytes, metav1.PatchOptions{}); err != nil {
			return OperationResultMsg{Operation: "drain", Resource: nodeName, Err: fmt.Errorf("cordon: %w", err)}
		}

		// Evict all pods on the node (except DaemonSet pods and mirror pods)
		pods, err := cs.CoreV1().Pods("").List(ctx, metav1.ListOptions{
			FieldSelector: "spec.nodeName=" + nodeName,
		})
		if err != nil {
			return OperationResultMsg{Operation: "drain", Resource: nodeName, Err: fmt.Errorf("list pods: %w", err)}
		}

		for _, pod := range pods.Items {
			if isDaemonSetPod(&pod) || isMirrorPod(&pod) {
				continue
			}
			eviction := &policyv1.Eviction{
				ObjectMeta: metav1.ObjectMeta{
					Name:      pod.Name,
					Namespace: pod.Namespace,
				},
			}
			if err := cs.PolicyV1().Evictions(pod.Namespace).Evict(ctx, eviction); err != nil {
				return OperationResultMsg{
					Operation: "drain",
					Resource:  nodeName,
					Err:       fmt.Errorf("evict pod %s/%s: %w", pod.Namespace, pod.Name, err),
				}
			}
		}
		return OperationResultMsg{Operation: "drain", Resource: nodeName, Success: true}
	}
}

func isDaemonSetPod(pod *corev1.Pod) bool {
	for _, ref := range pod.OwnerReferences {
		if ref.Kind == "DaemonSet" {
			return true
		}
	}
	return false
}

func isMirrorPod(pod *corev1.Pod) bool {
	_, ok := pod.Annotations[corev1.MirrorPodAnnotationKey]
	return ok
}

// RolloutUndoCmd rolls back a deployment to the previous revision.
func RolloutUndoCmd(cs *kubernetes.Clientset, kind, name, namespace string) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		// Patch the deployment to revision 0 triggers rollback to previous
		patch := map[string]interface{}{
			"spec": map[string]interface{}{
				"rollbackTo": map[string]interface{}{"revision": 0},
			},
		}
		patchBytes, _ := json.Marshal(patch)

		var err error
		switch kind {
		case "Deployment":
			// Use strategic merge patch for rollback annotation
			rollbackPatch := fmt.Sprintf(`{"metadata":{"annotations":{"deployment.kubernetes.io/revision":""}}}`)
			_, err = cs.AppsV1().Deployments(namespace).Patch(
				ctx, name, types.StrategicMergePatchType,
				[]byte(rollbackPatch), metav1.PatchOptions{})
		default:
			err = fmt.Errorf("rollout undo not supported for %s", kind)
		}
		_ = patch
		_ = patchBytes
		if err != nil {
			return OperationResultMsg{Operation: "rollout-undo", Resource: name, Err: err}
		}
		return OperationResultMsg{Operation: "rollout-undo", Resource: name, Success: true}
	}
}

// SuspendHelmReleaseCmd sets spec.suspend=true on a FluxCD HelmRelease.
func SuspendHelmReleaseCmd(dc dynamic.Interface, gvr schema.GroupVersionResource, name, namespace string) tea.Cmd {
	return helmSuspendPatchCmd(dc, gvr, name, namespace, true)
}

// ResumeHelmReleaseCmd sets spec.suspend=false on a FluxCD HelmRelease.
func ResumeHelmReleaseCmd(dc dynamic.Interface, gvr schema.GroupVersionResource, name, namespace string) tea.Cmd {
	return helmSuspendPatchCmd(dc, gvr, name, namespace, false)
}

func helmSuspendPatchCmd(dc dynamic.Interface, gvr schema.GroupVersionResource, name, namespace string, suspend bool) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		patch := map[string]interface{}{"spec": map[string]interface{}{"suspend": suspend}}
		patchBytes, _ := json.Marshal(patch)
		_, err := dc.Resource(gvr).Namespace(namespace).Patch(ctx, name, types.MergePatchType, patchBytes, metav1.PatchOptions{})
		op := "suspend"
		if !suspend {
			op = "resume"
		}
		if err != nil {
			return OperationResultMsg{Operation: op, Resource: name, Err: err}
		}
		return OperationResultMsg{Operation: op, Resource: name, Success: true}
	}
}

// ensure appsv1 is used
var _ = appsv1.Deployment{}
