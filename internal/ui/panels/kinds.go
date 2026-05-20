package panels

import (
	"context"
	"encoding/json"
	"fmt"

	tea "charm.land/bubbletea/v2"
	k8sres "github.com/chaitanyak/klens/internal/k8s"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	sigsyaml "sigs.k8s.io/yaml"
)

// init registers per-kind behavior on the k8s.Registry. Every kind that
// previously had a case in model.listRows / yaml_viewer.fetchObject /
// model.buildTopology is registered here. New kinds add ONE entry.
func init() {
	// Helpers ------------------------------------------------------------

	rows := func(fn func() []k8sres.ResourceRow) []k8sres.ResourceRow { return fn() }

	get := func(getter func(ctx context.Context) (any, error)) (any, error) {
		return getter(context.Background())
	}

	// Topology lookup adapters: each searches the cached list for `name`
	// and delegates to the typed BuildXxxTopology constructor. This is the
	// shape that used to live as a switch in model.buildTopology.
	k8sres.SetHandlers("Pod",
		func(wf *k8sres.WatcherFactory, ns string, ctx k8sres.RowContext) []k8sres.ResourceRow {
			return rows(func() []k8sres.ResourceRow {
				return BuildPodRows(wf.ListPods(ns), ctx.Metrics, ctx.PortForwardActive)
			})
		},
		func(cs *kubernetes.Clientset, name, ns string) (any, error) {
			return get(func(c context.Context) (any, error) {
				return cs.CoreV1().Pods(ns).Get(c, name, metav1.GetOptions{})
			})
		},
		nil,
	)

	k8sres.SetHandlers("Deployment",
		func(wf *k8sres.WatcherFactory, ns string, _ k8sres.RowContext) []k8sres.ResourceRow {
			return BuildDeploymentRows(wf.ListDeployments(ns))
		},
		func(cs *kubernetes.Clientset, name, ns string) (any, error) {
			return cs.AppsV1().Deployments(ns).Get(context.Background(), name, metav1.GetOptions{})
		},
		func(wf *k8sres.WatcherFactory, ns, name string) *k8sres.TreeNode {
			for _, d := range wf.ListDeployments(ns) {
				if d.Name == name {
					return k8sres.BuildDeploymentTopology(d, wf)
				}
			}
			return nil
		},
	)

	k8sres.SetHandlers("StatefulSet",
		func(wf *k8sres.WatcherFactory, ns string, _ k8sres.RowContext) []k8sres.ResourceRow {
			return BuildStatefulSetRows(wf.ListStatefulSets(ns))
		},
		func(cs *kubernetes.Clientset, name, ns string) (any, error) {
			return cs.AppsV1().StatefulSets(ns).Get(context.Background(), name, metav1.GetOptions{})
		},
		nil,
	)

	k8sres.SetHandlers("DaemonSet",
		func(wf *k8sres.WatcherFactory, ns string, _ k8sres.RowContext) []k8sres.ResourceRow {
			return BuildDaemonSetRows(wf.ListDaemonSets(ns))
		},
		func(cs *kubernetes.Clientset, name, ns string) (any, error) {
			return cs.AppsV1().DaemonSets(ns).Get(context.Background(), name, metav1.GetOptions{})
		},
		nil,
	)

	k8sres.SetHandlers("ReplicaSet",
		func(wf *k8sres.WatcherFactory, ns string, _ k8sres.RowContext) []k8sres.ResourceRow {
			return BuildReplicaSetRows(wf.ListReplicaSets(ns))
		},
		func(cs *kubernetes.Clientset, name, ns string) (any, error) {
			return cs.AppsV1().ReplicaSets(ns).Get(context.Background(), name, metav1.GetOptions{})
		},
		nil,
	)

	k8sres.SetHandlers("Job",
		func(wf *k8sres.WatcherFactory, ns string, _ k8sres.RowContext) []k8sres.ResourceRow {
			return BuildJobRows(wf.ListJobs(ns))
		},
		func(cs *kubernetes.Clientset, name, ns string) (any, error) {
			return cs.BatchV1().Jobs(ns).Get(context.Background(), name, metav1.GetOptions{})
		},
		nil,
	)

	k8sres.SetHandlers("CronJob",
		func(wf *k8sres.WatcherFactory, ns string, _ k8sres.RowContext) []k8sres.ResourceRow {
			return BuildCronJobRows(wf.ListCronJobs(ns))
		},
		func(cs *kubernetes.Clientset, name, ns string) (any, error) {
			return cs.BatchV1().CronJobs(ns).Get(context.Background(), name, metav1.GetOptions{})
		},
		nil,
	)

	k8sres.SetHandlers("Service",
		func(wf *k8sres.WatcherFactory, ns string, _ k8sres.RowContext) []k8sres.ResourceRow {
			return BuildServiceRows(wf.ListServices(ns))
		},
		func(cs *kubernetes.Clientset, name, ns string) (any, error) {
			return cs.CoreV1().Services(ns).Get(context.Background(), name, metav1.GetOptions{})
		},
		func(wf *k8sres.WatcherFactory, ns, name string) *k8sres.TreeNode {
			for _, s := range wf.ListServices(ns) {
				if s.Name == name {
					return k8sres.BuildServiceTopology(s, wf)
				}
			}
			return nil
		},
	)

	k8sres.SetHandlers("Ingress",
		func(wf *k8sres.WatcherFactory, ns string, _ k8sres.RowContext) []k8sres.ResourceRow {
			return BuildIngressRows(wf.ListIngresses(ns))
		},
		func(cs *kubernetes.Clientset, name, ns string) (any, error) {
			return cs.NetworkingV1().Ingresses(ns).Get(context.Background(), name, metav1.GetOptions{})
		},
		func(wf *k8sres.WatcherFactory, ns, name string) *k8sres.TreeNode {
			for _, ing := range wf.ListIngresses(ns) {
				if ing.Name == name {
					return k8sres.BuildIngressTopology(ing, wf)
				}
			}
			return nil
		},
	)

	k8sres.SetHandlers("ConfigMap",
		func(wf *k8sres.WatcherFactory, ns string, _ k8sres.RowContext) []k8sres.ResourceRow {
			return BuildConfigMapRows(wf.ListConfigMaps(ns))
		},
		func(cs *kubernetes.Clientset, name, ns string) (any, error) {
			return cs.CoreV1().ConfigMaps(ns).Get(context.Background(), name, metav1.GetOptions{})
		},
		nil,
	)

	k8sres.SetHandlers("Secret",
		func(wf *k8sres.WatcherFactory, ns string, _ k8sres.RowContext) []k8sres.ResourceRow {
			return BuildSecretRows(wf.ListSecrets(ns))
		},
		func(cs *kubernetes.Clientset, name, ns string) (any, error) {
			return cs.CoreV1().Secrets(ns).Get(context.Background(), name, metav1.GetOptions{})
		},
		nil,
	)

	k8sres.SetHandlers("ServiceAccount",
		nil, // no live row-listing today
		func(cs *kubernetes.Clientset, name, ns string) (any, error) {
			return cs.CoreV1().ServiceAccounts(ns).Get(context.Background(), name, metav1.GetOptions{})
		},
		nil,
	)

	k8sres.SetHandlers("Node",
		func(wf *k8sres.WatcherFactory, _ string, _ k8sres.RowContext) []k8sres.ResourceRow {
			return BuildNodeRows(wf.ListNodes())
		},
		func(cs *kubernetes.Clientset, name, _ string) (any, error) {
			return cs.CoreV1().Nodes().Get(context.Background(), name, metav1.GetOptions{})
		},
		nil,
	)

	k8sres.SetHandlers("PersistentVolumeClaim",
		func(wf *k8sres.WatcherFactory, ns string, _ k8sres.RowContext) []k8sres.ResourceRow {
			return BuildPVCRows(wf.ListPVCs(ns))
		},
		func(cs *kubernetes.Clientset, name, ns string) (any, error) {
			return cs.CoreV1().PersistentVolumeClaims(ns).Get(context.Background(), name, metav1.GetOptions{})
		},
		nil,
	)

	k8sres.SetHandlers("PersistentVolume",
		func(wf *k8sres.WatcherFactory, _ string, _ k8sres.RowContext) []k8sres.ResourceRow {
			return BuildPVRows(wf.ListPersistentVolumes())
		},
		func(cs *kubernetes.Clientset, name, _ string) (any, error) {
			return cs.CoreV1().PersistentVolumes().Get(context.Background(), name, metav1.GetOptions{})
		},
		nil,
	)

	k8sres.SetHandlers("Namespace",
		nil,
		func(cs *kubernetes.Clientset, name, _ string) (any, error) {
			return cs.CoreV1().Namespaces().Get(context.Background(), name, metav1.GetOptions{})
		},
		nil,
	)

	k8sres.SetHandlers("Event",
		func(wf *k8sres.WatcherFactory, ns string, _ k8sres.RowContext) []k8sres.ResourceRow {
			return BuildEventRows(wf.ListEvents(ns))
		},
		nil,
		nil,
	)

	k8sres.SetHandlers("HelmRelease",
		func(wf *k8sres.WatcherFactory, ns string, _ k8sres.RowContext) []k8sres.ResourceRow {
			return BuildHelmReleaseRows(wf.ListHelmReleases(ns))
		},
		nil, // HelmRelease YAML uses the cached unstructured (FetchHelmReleaseYAMLCmd)
		nil,
	)

	registerActions()
}

// deleteAction wraps a per-kind "do the typed Delete call" closure into an
// Action. Used by every kind below that supports deletion. The Action's
// closure is the only kind-specific code; everything else (grace period,
// returning OperationResultMsg) is uniform.
func deleteAction(call func(d k8sres.ActionDeps, ctx context.Context, opts metav1.DeleteOptions) error) k8sres.Action {
	return func(d k8sres.ActionDeps) tea.Cmd {
		return func() tea.Msg {
			ctx := context.Background()
			grace := int64(0)
			opts := metav1.DeleteOptions{GracePeriodSeconds: &grace}
			if err := call(d, ctx, opts); err != nil {
				return k8sres.OperationResultMsg{Operation: "delete", Resource: d.Name, Err: err}
			}
			return k8sres.OperationResultMsg{Operation: "delete", Resource: d.Name, Success: true}
		}
	}
}

// applyAction wraps a per-kind "patch with raw merge body" closure. The body
// is the user-edited YAML converted to JSON. Returns YAMLAppliedMsg on success
// and YAMLApplyErrMsg on failure (matching the messages the editor expects).
// `kind` is captured in the closure so YAMLAppliedMsg carries it without
// adding another field to ActionDeps.
func applyAction(kind string, call func(d k8sres.ActionDeps, ctx context.Context, body []byte) error) k8sres.Action {
	return func(d k8sres.ActionDeps) tea.Cmd {
		return func() tea.Msg {
			jsonBytes, err := sigsyaml.YAMLToJSON([]byte(d.YAMLContent))
			if err != nil {
				return YAMLApplyErrMsg{fmt.Errorf("invalid YAML: %w", err)}
			}
			if err := call(d, context.Background(), jsonBytes); err != nil {
				if k8serrors.IsForbidden(err) {
					return YAMLApplyErrMsg{fmt.Errorf("forbidden: %w", err)}
				}
				return YAMLApplyErrMsg{err}
			}
			return YAMLAppliedMsg{Kind: kind, Name: d.Name, Namespace: d.Namespace}
		}
	}
}

// scaleAction wraps a per-kind "patch replicas" closure. The patch payload is
// uniform; only the typed Patch call differs.
func scaleAction(call func(d k8sres.ActionDeps, ctx context.Context, body []byte) error) k8sres.Action {
	return func(d k8sres.ActionDeps) tea.Cmd {
		return func() tea.Msg {
			ctx := context.Background()
			body, _ := json.Marshal(map[string]any{"spec": map[string]any{"replicas": d.Replicas}})
			if err := call(d, ctx, body); err != nil {
				return k8sres.OperationResultMsg{Operation: "scale", Resource: d.Name, Err: err}
			}
			return k8sres.OperationResultMsg{Operation: "scale", Resource: d.Name, Success: true}
		}
	}
}

func registerActions() {
	// Pod
	k8sres.RegisterAction("Pod", "delete", deleteAction(func(d k8sres.ActionDeps, ctx context.Context, o metav1.DeleteOptions) error {
		return d.Clientset.CoreV1().Pods(d.Namespace).Delete(ctx, d.Name, o)
	}))
	k8sres.RegisterAction("Pod", "apply", applyAction("Pod", func(d k8sres.ActionDeps, ctx context.Context, body []byte) error {
		_, err := d.Clientset.CoreV1().Pods(d.Namespace).Patch(ctx, d.Name, types.MergePatchType, body, metav1.PatchOptions{FieldManager: "klens"})
		return err
	}))

	// Deployment
	k8sres.RegisterAction("Deployment", "delete", deleteAction(func(d k8sres.ActionDeps, ctx context.Context, o metav1.DeleteOptions) error {
		return d.Clientset.AppsV1().Deployments(d.Namespace).Delete(ctx, d.Name, o)
	}))
	k8sres.RegisterAction("Deployment", "scale", scaleAction(func(d k8sres.ActionDeps, ctx context.Context, body []byte) error {
		_, err := d.Clientset.AppsV1().Deployments(d.Namespace).Patch(ctx, d.Name, types.MergePatchType, body, metav1.PatchOptions{})
		return err
	}))
	k8sres.RegisterAction("Deployment", "apply", applyAction("Deployment", func(d k8sres.ActionDeps, ctx context.Context, body []byte) error {
		_, err := d.Clientset.AppsV1().Deployments(d.Namespace).Patch(ctx, d.Name, types.MergePatchType, body, metav1.PatchOptions{FieldManager: "klens"})
		return err
	}))

	// StatefulSet
	k8sres.RegisterAction("StatefulSet", "delete", deleteAction(func(d k8sres.ActionDeps, ctx context.Context, o metav1.DeleteOptions) error {
		return d.Clientset.AppsV1().StatefulSets(d.Namespace).Delete(ctx, d.Name, o)
	}))
	k8sres.RegisterAction("StatefulSet", "scale", scaleAction(func(d k8sres.ActionDeps, ctx context.Context, body []byte) error {
		_, err := d.Clientset.AppsV1().StatefulSets(d.Namespace).Patch(ctx, d.Name, types.MergePatchType, body, metav1.PatchOptions{})
		return err
	}))
	k8sres.RegisterAction("StatefulSet", "apply", applyAction("StatefulSet", func(d k8sres.ActionDeps, ctx context.Context, body []byte) error {
		_, err := d.Clientset.AppsV1().StatefulSets(d.Namespace).Patch(ctx, d.Name, types.MergePatchType, body, metav1.PatchOptions{FieldManager: "klens"})
		return err
	}))

	// DaemonSet
	k8sres.RegisterAction("DaemonSet", "delete", deleteAction(func(d k8sres.ActionDeps, ctx context.Context, o metav1.DeleteOptions) error {
		return d.Clientset.AppsV1().DaemonSets(d.Namespace).Delete(ctx, d.Name, o)
	}))
	k8sres.RegisterAction("DaemonSet", "apply", applyAction("DaemonSet", func(d k8sres.ActionDeps, ctx context.Context, body []byte) error {
		_, err := d.Clientset.AppsV1().DaemonSets(d.Namespace).Patch(ctx, d.Name, types.MergePatchType, body, metav1.PatchOptions{FieldManager: "klens"})
		return err
	}))

	// ReplicaSet
	k8sres.RegisterAction("ReplicaSet", "delete", deleteAction(func(d k8sres.ActionDeps, ctx context.Context, o metav1.DeleteOptions) error {
		return d.Clientset.AppsV1().ReplicaSets(d.Namespace).Delete(ctx, d.Name, o)
	}))
	k8sres.RegisterAction("ReplicaSet", "scale", scaleAction(func(d k8sres.ActionDeps, ctx context.Context, body []byte) error {
		_, err := d.Clientset.AppsV1().ReplicaSets(d.Namespace).Patch(ctx, d.Name, types.MergePatchType, body, metav1.PatchOptions{})
		return err
	}))

	// Service
	k8sres.RegisterAction("Service", "delete", deleteAction(func(d k8sres.ActionDeps, ctx context.Context, o metav1.DeleteOptions) error {
		return d.Clientset.CoreV1().Services(d.Namespace).Delete(ctx, d.Name, o)
	}))
	k8sres.RegisterAction("Service", "apply", applyAction("Service", func(d k8sres.ActionDeps, ctx context.Context, body []byte) error {
		_, err := d.Clientset.CoreV1().Services(d.Namespace).Patch(ctx, d.Name, types.MergePatchType, body, metav1.PatchOptions{FieldManager: "klens"})
		return err
	}))

	// ConfigMap
	k8sres.RegisterAction("ConfigMap", "delete", deleteAction(func(d k8sres.ActionDeps, ctx context.Context, o metav1.DeleteOptions) error {
		return d.Clientset.CoreV1().ConfigMaps(d.Namespace).Delete(ctx, d.Name, o)
	}))
	k8sres.RegisterAction("ConfigMap", "apply", applyAction("ConfigMap", func(d k8sres.ActionDeps, ctx context.Context, body []byte) error {
		_, err := d.Clientset.CoreV1().ConfigMaps(d.Namespace).Patch(ctx, d.Name, types.MergePatchType, body, metav1.PatchOptions{FieldManager: "klens"})
		return err
	}))

	// Secret
	k8sres.RegisterAction("Secret", "delete", deleteAction(func(d k8sres.ActionDeps, ctx context.Context, o metav1.DeleteOptions) error {
		return d.Clientset.CoreV1().Secrets(d.Namespace).Delete(ctx, d.Name, o)
	}))
	k8sres.RegisterAction("Secret", "apply", applyAction("Secret", func(d k8sres.ActionDeps, ctx context.Context, body []byte) error {
		_, err := d.Clientset.CoreV1().Secrets(d.Namespace).Patch(ctx, d.Name, types.MergePatchType, body, metav1.PatchOptions{FieldManager: "klens"})
		return err
	}))

	// Ingress
	k8sres.RegisterAction("Ingress", "delete", deleteAction(func(d k8sres.ActionDeps, ctx context.Context, o metav1.DeleteOptions) error {
		return d.Clientset.NetworkingV1().Ingresses(d.Namespace).Delete(ctx, d.Name, o)
	}))

	// Job
	k8sres.RegisterAction("Job", "delete", deleteAction(func(d k8sres.ActionDeps, ctx context.Context, o metav1.DeleteOptions) error {
		return d.Clientset.BatchV1().Jobs(d.Namespace).Delete(ctx, d.Name, o)
	}))

	// CronJob
	k8sres.RegisterAction("CronJob", "delete", deleteAction(func(d k8sres.ActionDeps, ctx context.Context, o metav1.DeleteOptions) error {
		return d.Clientset.BatchV1().CronJobs(d.Namespace).Delete(ctx, d.Name, o)
	}))

	// PersistentVolumeClaim
	k8sres.RegisterAction("PersistentVolumeClaim", "delete", deleteAction(func(d k8sres.ActionDeps, ctx context.Context, o metav1.DeleteOptions) error {
		return d.Clientset.CoreV1().PersistentVolumeClaims(d.Namespace).Delete(ctx, d.Name, o)
	}))

	// PersistentVolume
	k8sres.RegisterAction("PersistentVolume", "delete", deleteAction(func(d k8sres.ActionDeps, ctx context.Context, o metav1.DeleteOptions) error {
		return d.Clientset.CoreV1().PersistentVolumes().Delete(ctx, d.Name, o)
	}))

	// Namespace
	k8sres.RegisterAction("Namespace", "delete", deleteAction(func(d k8sres.ActionDeps, ctx context.Context, o metav1.DeleteOptions) error {
		return d.Clientset.CoreV1().Namespaces().Delete(ctx, d.Name, o)
	}))

	// HelmRelease — suspend/resume go through the dynamic client.
	helmSuspendCmd := func(suspend bool) k8sres.Action {
		op := "resume"
		if suspend {
			op = "suspend"
		}
		return func(d k8sres.ActionDeps) tea.Cmd {
			return func() tea.Msg {
				ctx := context.Background()
				body, _ := json.Marshal(map[string]any{"spec": map[string]any{"suspend": suspend}})
				_, err := d.Dynamic.Resource(d.HelmGVR).Namespace(d.Namespace).Patch(ctx, d.Name, types.MergePatchType, body, metav1.PatchOptions{})
				if err != nil {
					return k8sres.OperationResultMsg{Operation: op, Resource: d.Name, Err: err}
				}
				return k8sres.OperationResultMsg{Operation: op, Resource: d.Name, Success: true}
			}
		}
	}
	k8sres.RegisterAction("HelmRelease", "suspend", helmSuspendCmd(true))
	k8sres.RegisterAction("HelmRelease", "resume", helmSuspendCmd(false))
}
