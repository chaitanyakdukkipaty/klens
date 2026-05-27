package k8s

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// CachedLister reads through the informer cache via WatcherFactory. This is
// the production adapter for the TUI — sub-millisecond reads, watch-driven.
type CachedLister struct {
	wf *WatcherFactory
}

// NewCachedLister wraps a started WatcherFactory. The factory's per-kind
// caches must be synced (KindSynced) for List to return populated slices;
// before sync, List returns an empty slice with no error.
func NewCachedLister(wf *WatcherFactory) *CachedLister { return &CachedLister{wf: wf} }

// List dispatches by GVR to the corresponding WatcherFactory.List<Kind>
// method. Cluster-scoped kinds ignore ns; namespaced kinds accept "" or "all"
// for the cluster-wide view, the same convention WatcherFactory uses.
//
// HelmRelease's GVR is resolved at runtime (the cluster may serve v2,
// v2beta2, or v2beta1) — we check the factory's discovered GVR before the
// static-GVR switch so a match wins regardless of the version in flight.
func (c *CachedLister) List(_ context.Context, gvr schema.GroupVersionResource, ns string) ([]runtime.Object, error) {
	if c.wf == nil {
		return nil, fmt.Errorf("CachedLister: no WatcherFactory")
	}
	if hrGVR := c.wf.HelmReleaseGVR(); gvr == hrGVR {
		return wrapObjs(c.wf.ListHelmReleases(ns)), nil
	}
	switch gvr {
	case PodGVR:
		return wrapObjs(c.wf.ListPods(ns)), nil
	case ServiceGVR:
		return wrapObjs(c.wf.ListServices(ns)), nil
	case NodeGVR:
		return wrapObjs(c.wf.ListNodes()), nil
	case NamespaceGVR:
		return wrapObjs(c.listNamespaceObjects()), nil
	case ConfigMapGVR:
		return wrapObjs(c.wf.ListConfigMaps(ns)), nil
	case SecretGVR:
		return wrapObjs(c.wf.ListSecrets(ns)), nil
	case PersistentVolumeGVR:
		return wrapObjs(c.wf.ListPersistentVolumes()), nil
	case PersistentVolumeClaimGVR:
		return wrapObjs(c.wf.ListPVCs(ns)), nil
	case EventGVR:
		return wrapObjs(c.wf.ListEvents(ns)), nil
	case DeploymentGVR:
		return wrapObjs(c.wf.ListDeployments(ns)), nil
	case StatefulSetGVR:
		return wrapObjs(c.wf.ListStatefulSets(ns)), nil
	case DaemonSetGVR:
		return wrapObjs(c.wf.ListDaemonSets(ns)), nil
	case ReplicaSetGVR:
		return wrapObjs(c.wf.ListReplicaSets(ns)), nil
	case JobGVR:
		return wrapObjs(c.wf.ListJobs(ns)), nil
	case CronJobGVR:
		return wrapObjs(c.wf.ListCronJobs(ns)), nil
	case IngressGVR:
		return wrapObjs(c.wf.ListIngresses(ns)), nil
	}
	return nil, fmt.Errorf("CachedLister: no informer for GVR %s", gvr.String())
}

// listNamespaceObjects pulls *corev1.Namespace directly from the informer
// store. WatcherFactory.ListNamespaces returns []string for legacy callers
// that only need names; the Lister contract is []runtime.Object, so we read
// from the store ourselves rather than fan out a Get per name.
func (c *CachedLister) listNamespaceObjects() []*corev1.Namespace {
	objs := c.wf.factory.Core().V1().Namespaces().Informer().GetStore().List()
	out := make([]*corev1.Namespace, 0, len(objs))
	for _, o := range objs {
		if ns, ok := o.(*corev1.Namespace); ok {
			out = append(out, ns)
		}
	}
	return out
}

// wrapObjs converts []*T to []runtime.Object. Every k8s.io/api pointer type
// satisfies runtime.Object via generated DeepCopyObject, so the constraint
// is satisfied for every WatcherFactory.List* return type.
func wrapObjs[T runtime.Object](items []T) []runtime.Object {
	out := make([]runtime.Object, 0, len(items))
	for _, it := range items {
		out = append(out, it)
	}
	return out
}
