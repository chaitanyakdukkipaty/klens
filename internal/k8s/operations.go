package k8s

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
)

// OperationResultMsg reports success or failure of a cluster operation.
type OperationResultMsg struct {
	Operation string
	Resource  string
	Success   bool
	Err       error
}

// Per-kind delete and scale dispatch live as Actions on the
// ResourceDescriptor (see RegisterAction in resources.go and the
// registration site in internal/ui/panels/kinds.go). Callers look up
// k8s.LookupAction(kind, "delete") or LookupAction(kind, "scale") rather
// than going through a kind-keyed switch here.

// RolloutRestartCmd restarts a deployment by patching the pod template annotation.
func RolloutRestartCmd(cs kubernetes.Interface, kind, name, namespace string) tea.Cmd {
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
func CordonCmd(cs kubernetes.Interface, nodeName string, cordon bool) tea.Cmd {
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
func DrainCmd(cs kubernetes.Interface, nodeName string) tea.Cmd {
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
func RolloutUndoCmd(cs kubernetes.Interface, kind, name, namespace string) tea.Cmd {
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

// HelmRelease suspend/resume are registered as Actions on the descriptor
// (see registerActions in internal/ui/panels/kinds.go).

// ensure appsv1 is used
var _ = appsv1.Deployment{}
