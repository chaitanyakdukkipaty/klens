package k8s

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
)

// FakeLister reads through a kubernetes.Interface directly — no informer,
// no cache, no watch. Wrap a fake.NewSimpleClientset to get a Lister that
// reflects whatever objects were seeded into the fake store.
//
// HelmRelease is not supported: fake.NewSimpleClientset doesn't model the
// dynamic CRD path. Tests that need HelmRelease coverage should route
// through envtest, not this adapter.
type FakeLister struct {
	cs kubernetes.Interface
}

// NewFakeLister wraps a kubernetes.Interface — typically fake.NewSimpleClientset
// seeded with test objects.
func NewFakeLister(cs kubernetes.Interface) *FakeLister { return &FakeLister{cs: cs} }

// List dispatches by GVR to the typed client. Namespace semantics match
// WatcherFactory: "" and "all" both mean cluster-wide. The translation
// matters because client-go's typed lister takes "" as the cluster-wide
// marker — "all" would be interpreted as a literal namespace name.
func (f *FakeLister) List(ctx context.Context, gvr schema.GroupVersionResource, ns string) ([]runtime.Object, error) {
	if f.cs == nil {
		return nil, fmt.Errorf("FakeLister: no clientset")
	}
	scoped := ns
	if scoped == "all" {
		scoped = ""
	}
	opts := metav1.ListOptions{}

	switch gvr {
	case PodGVR:
		l, err := f.cs.CoreV1().Pods(scoped).List(ctx, opts)
		if err != nil {
			return nil, err
		}
		return wrapItems(l.Items, func(i int) runtime.Object { return &l.Items[i] }), nil
	case ServiceGVR:
		l, err := f.cs.CoreV1().Services(scoped).List(ctx, opts)
		if err != nil {
			return nil, err
		}
		return wrapItems(l.Items, func(i int) runtime.Object { return &l.Items[i] }), nil
	case NodeGVR:
		l, err := f.cs.CoreV1().Nodes().List(ctx, opts)
		if err != nil {
			return nil, err
		}
		return wrapItems(l.Items, func(i int) runtime.Object { return &l.Items[i] }), nil
	case NamespaceGVR:
		l, err := f.cs.CoreV1().Namespaces().List(ctx, opts)
		if err != nil {
			return nil, err
		}
		return wrapItems(l.Items, func(i int) runtime.Object { return &l.Items[i] }), nil
	case ConfigMapGVR:
		l, err := f.cs.CoreV1().ConfigMaps(scoped).List(ctx, opts)
		if err != nil {
			return nil, err
		}
		return wrapItems(l.Items, func(i int) runtime.Object { return &l.Items[i] }), nil
	case SecretGVR:
		l, err := f.cs.CoreV1().Secrets(scoped).List(ctx, opts)
		if err != nil {
			return nil, err
		}
		return wrapItems(l.Items, func(i int) runtime.Object { return &l.Items[i] }), nil
	case PersistentVolumeGVR:
		l, err := f.cs.CoreV1().PersistentVolumes().List(ctx, opts)
		if err != nil {
			return nil, err
		}
		return wrapItems(l.Items, func(i int) runtime.Object { return &l.Items[i] }), nil
	case PersistentVolumeClaimGVR:
		l, err := f.cs.CoreV1().PersistentVolumeClaims(scoped).List(ctx, opts)
		if err != nil {
			return nil, err
		}
		return wrapItems(l.Items, func(i int) runtime.Object { return &l.Items[i] }), nil
	case EventGVR:
		l, err := f.cs.CoreV1().Events(scoped).List(ctx, opts)
		if err != nil {
			return nil, err
		}
		return wrapItems(l.Items, func(i int) runtime.Object { return &l.Items[i] }), nil
	case DeploymentGVR:
		l, err := f.cs.AppsV1().Deployments(scoped).List(ctx, opts)
		if err != nil {
			return nil, err
		}
		return wrapItems(l.Items, func(i int) runtime.Object { return &l.Items[i] }), nil
	case StatefulSetGVR:
		l, err := f.cs.AppsV1().StatefulSets(scoped).List(ctx, opts)
		if err != nil {
			return nil, err
		}
		return wrapItems(l.Items, func(i int) runtime.Object { return &l.Items[i] }), nil
	case DaemonSetGVR:
		l, err := f.cs.AppsV1().DaemonSets(scoped).List(ctx, opts)
		if err != nil {
			return nil, err
		}
		return wrapItems(l.Items, func(i int) runtime.Object { return &l.Items[i] }), nil
	case ReplicaSetGVR:
		l, err := f.cs.AppsV1().ReplicaSets(scoped).List(ctx, opts)
		if err != nil {
			return nil, err
		}
		return wrapItems(l.Items, func(i int) runtime.Object { return &l.Items[i] }), nil
	case JobGVR:
		l, err := f.cs.BatchV1().Jobs(scoped).List(ctx, opts)
		if err != nil {
			return nil, err
		}
		return wrapItems(l.Items, func(i int) runtime.Object { return &l.Items[i] }), nil
	case CronJobGVR:
		l, err := f.cs.BatchV1().CronJobs(scoped).List(ctx, opts)
		if err != nil {
			return nil, err
		}
		return wrapItems(l.Items, func(i int) runtime.Object { return &l.Items[i] }), nil
	case IngressGVR:
		l, err := f.cs.NetworkingV1().Ingresses(scoped).List(ctx, opts)
		if err != nil {
			return nil, err
		}
		return wrapItems(l.Items, func(i int) runtime.Object { return &l.Items[i] }), nil
	}
	return nil, fmt.Errorf("FakeLister: unsupported GVR %s", gvr.String())
}

// wrapItems turns a typed Items slice into []runtime.Object via an indexed
// getter — taking the address of items[i] avoids a per-iteration copy and
// hands the caller a pointer into the underlying array. The slice is the
// caller's to keep; we don't retain it.
func wrapItems[T any](items []T, get func(i int) runtime.Object) []runtime.Object {
	out := make([]runtime.Object, len(items))
	for i := range items {
		out[i] = get(i)
	}
	return out
}
