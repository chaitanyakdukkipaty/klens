package k8s

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"

	"github.com/chaitanyak/klens/internal/cluster"
)

const maxDeploymentPods = 10

// LogOptions controls log fetching for CLI subcommands.
type LogOptions struct {
	Container    string
	TailLines    int64 // 0 = no limit
	Follow       bool
	SinceSeconds *int64
	SinceTime    *metav1.Time
}

// LogLineFunc is called once per log line with pod and container context.
// Implementations must be safe for concurrent calls.
type LogLineFunc func(pod, container, line string)

// QueryContexts returns all context names from kubeconfig.
func QueryContexts(mgr *cluster.Manager) []string {
	return mgr.Contexts()
}

// QueryNamespaces lists all namespaces in the cluster.
func QueryNamespaces(ctx context.Context, cs *kubernetes.Clientset) ([]corev1.Namespace, error) {
	list, err := cs.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list namespaces: %w", err)
	}
	return list.Items, nil
}

// QueryPods lists pods in namespace (empty = all namespaces).
func QueryPods(ctx context.Context, cs *kubernetes.Clientset, namespace, labelSelector string) ([]corev1.Pod, error) {
	list, err := cs.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: labelSelector})
	if err != nil {
		return nil, fmt.Errorf("list pods in %q: %w", namespace, err)
	}
	return list.Items, nil
}

// QueryDeployments lists deployments in namespace (empty = all namespaces).
func QueryDeployments(ctx context.Context, cs *kubernetes.Clientset, namespace, labelSelector string) ([]appsv1.Deployment, error) {
	list, err := cs.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{LabelSelector: labelSelector})
	if err != nil {
		return nil, fmt.Errorf("list deployments in %q: %w", namespace, err)
	}
	return list.Items, nil
}

// QueryEvents lists events in namespace. fieldSelector may use
// "involvedObject.name=foo,involvedObject.kind=Pod" syntax.
// If since > 0 events older than since are excluded (client-side).
func QueryEvents(ctx context.Context, cs *kubernetes.Clientset, namespace, fieldSelector string, since time.Duration) ([]corev1.Event, error) {
	list, err := cs.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{FieldSelector: fieldSelector})
	if err != nil {
		return nil, fmt.Errorf("list events in %q: %w", namespace, err)
	}
	if since <= 0 {
		return list.Items, nil
	}
	cutoff := time.Now().Add(-since)
	out := make([]corev1.Event, 0, len(list.Items))
	for _, ev := range list.Items {
		ts := ev.LastTimestamp.Time
		if ts.IsZero() {
			ts = ev.EventTime.Time
		}
		if ts.After(cutoff) {
			out = append(out, ev)
		}
	}
	return out, nil
}

// QueryNodes lists all nodes in the cluster.
func QueryNodes(ctx context.Context, cs *kubernetes.Clientset) ([]corev1.Node, error) {
	list, err := cs.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list nodes: %w", err)
	}
	return list.Items, nil
}

// QueryStatefulSets lists statefulsets in namespace (empty = all namespaces).
func QueryStatefulSets(ctx context.Context, cs *kubernetes.Clientset, namespace, labelSelector string) ([]appsv1.StatefulSet, error) {
	list, err := cs.AppsV1().StatefulSets(namespace).List(ctx, metav1.ListOptions{LabelSelector: labelSelector})
	if err != nil {
		return nil, fmt.Errorf("list statefulsets in %q: %w", namespace, err)
	}
	return list.Items, nil
}

// QueryPVCs lists persistent volume claims in namespace (empty = all namespaces).
func QueryPVCs(ctx context.Context, cs *kubernetes.Clientset, namespace string) ([]corev1.PersistentVolumeClaim, error) {
	list, err := cs.CoreV1().PersistentVolumeClaims(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list pvcs in %q: %w", namespace, err)
	}
	return list.Items, nil
}

// QueryHelmReleases lists FluxCD HelmRelease resources using the dynamic client.
// Returns an empty slice (no error) if the CRD is absent on the cluster.
func QueryHelmReleases(ctx context.Context, dc dynamic.Interface, gvr schema.GroupVersionResource, namespace string) ([]unstructured.Unstructured, error) {
	list, err := dc.Resource(gvr).Namespace(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) || isAbsentCRD(err) {
			return []unstructured.Unstructured{}, nil
		}
		return nil, fmt.Errorf("list helmreleases: %w", err)
	}
	return list.Items, nil
}

// isAbsentCRD returns true for errors that indicate the CRD is not installed.
func isAbsentCRD(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "no kind is registered") ||
		strings.Contains(msg, "could not find the requested resource") ||
		strings.Contains(msg, "the server could not find the requested resource")
}

// StreamPodLogs streams logs from a single pod, calling fn for each line.
// Blocks until the stream ends (batch) or ctx is cancelled (follow).
func StreamPodLogs(ctx context.Context, cs *kubernetes.Clientset, namespace, pod string, opts LogOptions, fn LogLineFunc) error {
	container := opts.Container
	if container == "" {
		p, err := cs.CoreV1().Pods(namespace).Get(ctx, pod, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("get pod %q: %w", pod, err)
		}
		if len(p.Spec.Containers) > 0 {
			container = p.Spec.Containers[0].Name
			if len(p.Spec.Containers) > 1 {
				names := make([]string, len(p.Spec.Containers))
				for i, c := range p.Spec.Containers {
					names[i] = c.Name
				}
				fmt.Fprintf(os.Stderr, "note: pod %q has containers %v; using %q\n", pod, names, container)
			}
		}
	}

	podLogOpts := &corev1.PodLogOptions{
		Container:    container,
		Follow:       opts.Follow,
		SinceSeconds: opts.SinceSeconds,
		SinceTime:    opts.SinceTime,
	}
	if opts.TailLines > 0 {
		podLogOpts.TailLines = &opts.TailLines
	}

	req := cs.CoreV1().Pods(namespace).GetLogs(pod, podLogOpts)
	stream, err := req.Stream(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return fmt.Errorf("open log stream for %q: %w", pod, err)
	}
	defer stream.Close()

	scanner := bufio.NewScanner(stream)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		if ctx.Err() != nil {
			return nil
		}
		fn(pod, container, scanner.Text())
	}
	if err := scanner.Err(); err != nil && ctx.Err() == nil {
		return fmt.Errorf("scan logs for %q: %w", pod, err)
	}
	return nil
}

// StreamDeploymentLogs streams logs from all pods of a deployment concurrently.
// fn calls are serialised internally; safe for multi-pod interleaving.
func StreamDeploymentLogs(ctx context.Context, cs *kubernetes.Clientset, namespace, deployment string, opts LogOptions, fn LogLineFunc) error {
	dep, err := cs.AppsV1().Deployments(namespace).Get(ctx, deployment, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get deployment %q: %w", deployment, err)
	}

	selStr := metav1.FormatLabelSelector(dep.Spec.Selector)
	pods, err := cs.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: selStr})
	if err != nil {
		return fmt.Errorf("list pods for deployment %q: %w", deployment, err)
	}
	if len(pods.Items) == 0 {
		return fmt.Errorf("no pods found for deployment %q in %q", deployment, namespace)
	}

	items := pods.Items
	if len(items) > maxDeploymentPods {
		fmt.Fprintf(os.Stderr, "warning: %d pods found; streaming from first %d\n", len(items), maxDeploymentPods)
		items = items[:maxDeploymentPods]
	}

	// Serialise fn calls from concurrent goroutines.
	var mu sync.Mutex
	safeFn := func(pod, container, line string) {
		mu.Lock()
		fn(pod, container, line)
		mu.Unlock()
	}

	var wg sync.WaitGroup
	errs := make(chan error, len(items))

	for i := range items {
		pod := items[i]
		wg.Add(1)
		go func() {
			defer wg.Done()
			podOpts := opts
			if podOpts.Container == "" && len(pod.Spec.Containers) > 0 {
				podOpts.Container = pod.Spec.Containers[0].Name
			}
			if err := StreamPodLogs(ctx, cs, namespace, pod.Name, podOpts, safeFn); err != nil {
				errs <- err
			}
		}()
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}
