package k8s

import (
	"context"
	"sync"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/tools/cache"
)

// InformerRegistry is the single GVR-keyed store of SharedIndexInformers for a
// cluster. It owns the typed and dynamic factories; per-kind glue (event
// handlers, sync notifications, access-denied tracking) lives on
// WatcherFactory and consults the registry through GVR/kind lookups.
//
// Registration order is irrelevant: Register can be called before Start (the
// 19 built-in kinds) or after Start (HelmRelease, once its GVR is discovered).
// The factory used is the typed one when the GVR is in the scheme, the
// dynamic one otherwise.
type InformerRegistry struct {
	typed   informers.SharedInformerFactory
	dynamic dynamicinformer.DynamicSharedInformerFactory

	mu        sync.RWMutex
	informers map[schema.GroupVersionResource]cache.SharedIndexInformer
	gvrToKind map[schema.GroupVersionResource]string
	kindToGVR map[string]schema.GroupVersionResource
	synced    map[schema.GroupVersionResource]bool
	reserved  map[string]struct{} // kinds we promise to register later (HelmRelease before GVR discovery)
}

// NewInformerRegistry wraps the typed and dynamic shared informer factories.
// Both must be configured for the same namespace scope; namespace filtering at
// the call site (List) is the secondary filter.
func NewInformerRegistry(typed informers.SharedInformerFactory, dyn dynamicinformer.DynamicSharedInformerFactory) *InformerRegistry {
	return &InformerRegistry{
		typed:     typed,
		dynamic:   dyn,
		informers: make(map[schema.GroupVersionResource]cache.SharedIndexInformer),
		gvrToKind: make(map[schema.GroupVersionResource]string),
		kindToGVR: make(map[string]schema.GroupVersionResource),
		synced:    make(map[schema.GroupVersionResource]bool),
		reserved:  make(map[string]struct{}),
	}
}

// Register adds (gvr, kind) to the registry and returns the underlying
// informer. The typed factory is tried first; any error (typically a scheme
// miss for CRDs like HelmRelease) falls through to the dynamic factory.
//
// Register creates the informer but does NOT start it. Callers must attach
// event/error handlers via the returned informer (SetWatchErrorHandler in
// particular only works pre-start) and then call Start. This ordering is the
// same whether registration happens before or after the registry's first
// Start call — both paths land on Start to kick the new informer.
func (r *InformerRegistry) Register(gvr schema.GroupVersionResource, kind string) cache.SharedIndexInformer {
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.informers[gvr]; ok {
		return existing
	}

	var informer cache.SharedIndexInformer
	if gi, err := r.typed.ForResource(gvr); err == nil {
		informer = gi.Informer()
	} else {
		informer = r.dynamic.ForResource(gvr).Informer()
	}
	r.informers[gvr] = informer
	r.gvrToKind[gvr] = kind
	r.kindToGVR[kind] = gvr
	delete(r.reserved, kind)
	return informer
}

// Start kicks both factories with ctx.Done(). Idempotent: calling Start
// repeatedly only starts informers that aren't running yet, so post-Register
// callers (HelmRelease after GVR discovery) can re-invoke Start without
// double-starting earlier informers.
func (r *InformerRegistry) Start(ctx context.Context) {
	r.typed.Start(ctx.Done())
	r.dynamic.Start(ctx.Done())
}

// List returns all objects in the informer cache for `gvr`, optionally
// filtered by namespace. ns == "" or "all" returns everything; any other
// value matches against `obj.GetNamespace()` — cluster-scoped GVRs should
// be queried with ns == "" to receive their (always-namespace-empty) items.
//
// Returns nil if `gvr` was never registered.
func (r *InformerRegistry) List(gvr schema.GroupVersionResource, ns string) []runtime.Object {
	r.mu.RLock()
	inf, ok := r.informers[gvr]
	r.mu.RUnlock()
	if !ok {
		return nil
	}
	objs := inf.GetStore().List()
	out := make([]runtime.Object, 0, len(objs))
	wildcard := ns == "" || ns == "all"
	for _, o := range objs {
		ro, _ := o.(runtime.Object)
		if ro == nil {
			continue
		}
		if !wildcard {
			if mo, ok := o.(metav1.Object); ok && mo.GetNamespace() != ns {
				continue
			}
		}
		out = append(out, ro)
	}
	return out
}

// Synced reports whether MarkSynced has been called for `gvr`. The registry
// does not run its own WaitForCacheSync goroutines — that orchestration lives
// on WatcherFactory so it can emit Bubbletea messages and gate the splash.
func (r *InformerRegistry) Synced(gvr schema.GroupVersionResource) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.synced[gvr]
}

// MarkSynced records that the informer for `gvr` has completed its initial
// LIST. Called by WatcherFactory from its per-kind WaitForCacheSync goroutine.
func (r *InformerRegistry) MarkSynced(gvr schema.GroupVersionResource) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.synced[gvr] = true
}

// KindFor returns the kind string registered alongside `gvr`, or "" if the
// GVR is not registered.
func (r *InformerRegistry) KindFor(gvr schema.GroupVersionResource) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.gvrToKind[gvr]
}

// GVRFor returns the GVR registered for `kind`, or the zero GVR + false if
// the kind is not registered.
func (r *InformerRegistry) GVRFor(kind string) (schema.GroupVersionResource, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	gvr, ok := r.kindToGVR[kind]
	return gvr, ok
}

// Informer returns the SharedIndexInformer for `gvr`, or nil if unregistered.
// Used by WatcherFactory to await cache sync on a specific GVR.
func (r *InformerRegistry) Informer(gvr schema.GroupVersionResource) cache.SharedIndexInformer {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.informers[gvr]
}

// IsRegistered reports whether `kind` is informer-backed at all. Used by
// KindSynced to distinguish "no informer for this kind" (return true so the
// UI doesn't show a Syncing placeholder forever) from "informer exists but
// pending initial LIST" (return false). A reserved kind (Reserve) counts as
// registered for this check so the discovery window for HelmRelease still
// shows the loading state.
func (r *InformerRegistry) IsRegistered(kind string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if _, ok := r.kindToGVR[kind]; ok {
		return true
	}
	_, ok := r.reserved[kind]
	return ok
}

// Reserve marks `kind` as informer-backed before its GVR is known. Used by
// HelmRelease where the GVR is resolved asynchronously: without the reserve,
// IsRegistered("HelmRelease") would return false during the discovery window
// and KindSynced would incorrectly report "synced — no informer expected".
// Once Register lands with the discovered GVR the reservation is cleared.
func (r *InformerRegistry) Reserve(kind string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reserved[kind] = struct{}{}
}
