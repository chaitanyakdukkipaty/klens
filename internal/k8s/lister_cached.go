package k8s

import (
	"context"
	"fmt"

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

// List delegates to the InformerRegistry. Cluster-scoped kinds ignore ns;
// namespaced kinds accept "" or "all" for the cluster-wide view, the same
// convention WatcherFactory uses internally.
//
// HelmRelease's GVR is resolved at runtime (the cluster may serve v2,
// v2beta2, or v2beta1) — callers should consult WatcherFactory.HelmReleaseGVR
// before invoking List for HelmRelease.
func (c *CachedLister) List(_ context.Context, gvr schema.GroupVersionResource, ns string) ([]runtime.Object, error) {
	if c.wf == nil || c.wf.registry == nil {
		return nil, fmt.Errorf("CachedLister: no WatcherFactory")
	}
	return c.wf.registry.List(gvr, ns), nil
}
