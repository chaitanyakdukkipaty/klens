package k8s

import (
	"context"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
)

// helmReleaseVersions lists FluxCD HelmRelease API versions from newest to oldest.
// discoverHelmReleaseGVR picks the first one the cluster actually serves.
var helmReleaseVersions = []string{"v2", "v2beta2", "v2beta1"}

var helmReleaseGVR = schema.GroupVersionResource{
	Group:    "helm.toolkit.fluxcd.io",
	Version:  "v2",
	Resource: "helmreleases",
}

// DiscoverHelmReleaseGVR returns the GVR for whichever FluxCD HelmRelease API
// version the cluster serves, falling back to the v2 default if discovery fails.
func DiscoverHelmReleaseGVR(cfg *rest.Config) schema.GroupVersionResource {
	return discoverHelmReleaseGVR(cfg)
}

func discoverHelmReleaseGVR(cfg *rest.Config) schema.GroupVersionResource {
	dc, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		return helmReleaseGVR
	}
	for _, version := range helmReleaseVersions {
		_, err := dc.ServerResourcesForGroupVersion("helm.toolkit.fluxcd.io/" + version)
		if err == nil {
			return schema.GroupVersionResource{
				Group:    "helm.toolkit.fluxcd.io",
				Version:  version,
				Resource: "helmreleases",
			}
		}
	}
	return helmReleaseGVR
}

// ResourceUpdatedMsg is sent to the Bubbletea model when informer data changes.
type ResourceUpdatedMsg struct {
	Kind string
}

// AccessDeniedMsg is sent (once) when an informer receives a forbidden error.
type AccessDeniedMsg struct {
	Kind string
}

// CacheSyncedMsg is sent once when the primary informer (Pods) has completed
// its initial LIST. The remaining informers stream KindSyncedMsg as they sync
// individually so the splash isn't gated on the slowest informer (Events,
// HelmRelease, etc.).
type CacheSyncedMsg struct{}

// KindSyncedMsg is sent for each non-primary informer once its cache has
// completed the initial LIST. The model uses this to refresh the table when
// the user is currently viewing that kind.
type KindSyncedMsg struct {
	Kind string
}

// coalesceWindow is how long the watcher batches informer events before
// emitting a single ResourceUpdatedMsg per kind. 100ms is below the
// human-perceptible threshold and collapses busy-cluster bursts.
const coalesceWindow = 100 * time.Millisecond

// WatcherFactory manages SharedIndexInformer instances for one cluster.
type WatcherFactory struct {
	dynamicClient dynamic.Interface
	registry      *InformerRegistry
	hrGVR         schema.GroupVersionResource
	restCfg       *rest.Config // stored for async Helm GVR discovery in Start()
	cancel        context.CancelFunc
	msgCh         chan tea.Msg
	started       bool
	syncing       bool
	accessDenied  map[string]struct{}
	kindsSynced   map[string]bool // per-kind initial LIST completion
	mu            sync.RWMutex

	// Event coalescing: instead of emitting one ResourceUpdatedMsg per
	// informer event, dirtyKinds accumulates kinds touched within the last
	// coalesceWindow and a ticker flushes them as one msg per kind.
	coalesceMu   sync.Mutex
	dirtyKinds   map[string]struct{}
	coalesceStop chan struct{}
}

// NewWatcherFactory creates a factory attached to the given clientset and rest config.
//
// The clientset is the kubernetes.Interface (not *Clientset) so tests can pass
// a fake clientset; informers.NewSharedInformerFactory already takes the
// interface, so the only real change is at the call site.
func NewWatcherFactory(cs kubernetes.Interface, cfg *rest.Config, namespace string, msgCh chan tea.Msg) *WatcherFactory {
	resync := 30 * time.Second
	var factory informers.SharedInformerFactory
	if namespace == "" || namespace == "all" || namespace == "default" {
		factory = informers.NewSharedInformerFactory(cs, resync)
	} else {
		factory = informers.NewSharedInformerFactoryWithOptions(cs, resync,
			informers.WithNamespace(namespace))
	}

	dc, _ := dynamic.NewForConfig(cfg)
	var dynFactory dynamicinformer.DynamicSharedInformerFactory
	if namespace == "" || namespace == "all" || namespace == "default" {
		dynFactory = dynamicinformer.NewDynamicSharedInformerFactory(dc, resync)
	} else {
		dynFactory = dynamicinformer.NewFilteredDynamicSharedInformerFactory(dc, resync, namespace, nil)
	}

	return &WatcherFactory{
		dynamicClient: dc,
		registry:      NewInformerRegistry(factory, dynFactory),
		hrGVR:         helmReleaseGVR, // will be overwritten by async discovery in Start()
		restCfg:       cfg,
		msgCh:         msgCh,
		accessDenied:  make(map[string]struct{}),
		kindsSynced:   make(map[string]bool),
	}
}

// DynamicClient returns the dynamic Kubernetes client.
func (w *WatcherFactory) DynamicClient() dynamic.Interface { return w.dynamicClient }

// HelmReleaseGVR returns the discovered GVR for HelmRelease on this cluster.
func (w *WatcherFactory) HelmReleaseGVR() schema.GroupVersionResource {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.hrGVR
}

// watchErrHandler returns a WatchErrorHandler that silently tracks forbidden
// errors and notifies the app once per kind instead of spamming klog.
func (w *WatcherFactory) watchErrHandler(kind string) cache.WatchErrorHandler {
	return func(_ *cache.Reflector, err error) {
		if err == nil {
			return
		}
		msg := strings.ToLower(err.Error())
		isForbidden := strings.Contains(msg, "forbidden")
		isNotFound := strings.Contains(msg, "not found") || strings.Contains(msg, "no kind is registered")
		if !isForbidden && !isNotFound {
			return
		}
		w.mu.Lock()
		_, already := w.accessDenied[kind]
		if !already {
			w.accessDenied[kind] = struct{}{}
		}
		w.mu.Unlock()
		if !already {
			select {
			case w.msgCh <- AccessDeniedMsg{Kind: kind}:
			default:
			}
		}
	}
}

// IsAccessDenied reports whether the given resource kind returned a forbidden error.
func (w *WatcherFactory) IsAccessDenied(kind string) bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	_, ok := w.accessDenied[kind]
	return ok
}

// IsSyncing reports whether the informer caches are still doing their initial LIST.
func (w *WatcherFactory) IsSyncing() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.syncing
}

// KindSynced reports whether the informer for `kind` has completed its initial
// LIST. The model uses this to render "Syncing <kind>…" in the table when the
// user navigates to a kind whose informer hasn't synced yet. For kinds we
// don't run an informer for, returns true so unrelated nav entries don't get
// stuck on the syncing placeholder.
//
// The set of informer-backed kinds is whatever Start() registered with the
// InformerRegistry — no parallel hard-coded map to drift out of sync.
func (w *WatcherFactory) KindSynced(kind string) bool {
	if !w.registry.IsRegistered(kind) {
		return true
	}
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.kindsSynced[kind]
}

// Registry returns the underlying InformerRegistry. Production callers should
// use ListAs[T] / CachedLister instead; the accessor exists for tests that
// want to inspect registration state.
func (w *WatcherFactory) Registry() *InformerRegistry { return w.registry }

// ListAs returns objects of type T from the informer cache for `kind`, optionally
// filtered by namespace. Items that fail the type assertion are skipped — in
// practice this only happens when the registry returns *unstructured.Unstructured
// for a kind and the caller asked for the typed shape, which is a programmer
// error. Returns nil if the kind is not informer-backed.
//
// This is the single seam for reading from the informer cache outside of the
// CachedLister.List dispatcher. Row builders, topology, and model-side scans
// all funnel through here, which is why the 17 per-kind List<Kind> methods
// are no longer needed.
func ListAs[T runtime.Object](wf *WatcherFactory, kind string, namespace string) []T {
	if wf == nil || wf.registry == nil {
		return nil
	}
	gvr, ok := wf.registry.GVRFor(kind)
	if !ok {
		return nil
	}
	objs := wf.registry.List(gvr, namespace)
	out := make([]T, 0, len(objs))
	for _, o := range objs {
		if t, ok := o.(T); ok {
			out = append(out, t)
		}
	}
	return out
}

// markDirty records that an informer for `kind` had an event. The coalesce
// goroutine flushes dirty kinds every coalesceWindow as a single
// ResourceUpdatedMsg per kind.
func (w *WatcherFactory) markDirty(kind string) {
	w.coalesceMu.Lock()
	if w.dirtyKinds == nil {
		w.dirtyKinds = make(map[string]struct{})
	}
	w.dirtyKinds[kind] = struct{}{}
	w.coalesceMu.Unlock()
}

// coalesceLoop periodically flushes dirtyKinds as ResourceUpdatedMsg events.
func (w *WatcherFactory) coalesceLoop() {
	ticker := time.NewTicker(coalesceWindow)
	defer ticker.Stop()
	for {
		select {
		case <-w.coalesceStop:
			return
		case <-ticker.C:
			w.coalesceMu.Lock()
			if len(w.dirtyKinds) == 0 {
				w.coalesceMu.Unlock()
				continue
			}
			kinds := make([]string, 0, len(w.dirtyKinds))
			for k := range w.dirtyKinds {
				kinds = append(kinds, k)
			}
			w.dirtyKinds = nil
			w.coalesceMu.Unlock()
			for _, k := range kinds {
				select {
				case w.msgCh <- ResourceUpdatedMsg{Kind: k}:
				default:
				}
			}
		}
	}
}

// informedKinds is the static list of built-in kinds that get a
// SharedIndexInformer at startup. The GVR for each comes from the kind's
// ResourceDescriptor (resources.Registry → GVR()), so adding a new informer
// is one row here plus one row in Registry.
//
// HelmRelease is registered separately because its GVR is discovered against
// the cluster (v2 / v2beta2 / v2beta1) before the informer can be wired.
var informedKinds = []string{
	"Pod", "Service", "Endpoints", "Node", "Namespace", "ConfigMap", "Secret",
	"ServiceAccount", "PersistentVolume", "PersistentVolumeClaim", "Event",
	"Deployment", "StatefulSet", "DaemonSet", "ReplicaSet", "Job", "CronJob",
	"Ingress", "NetworkPolicy",
}

// Start begins all informers. Should be called once per factory lifetime.
func (w *WatcherFactory) Start() {
	if w.started {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	w.cancel = cancel
	w.started = true
	w.mu.Lock()
	w.syncing = true
	w.mu.Unlock()
	w.coalesceStop = make(chan struct{})

	// Reserve HelmRelease so KindSynced("HelmRelease") returns false during
	// the GVR discovery window. The actual Register call happens once the
	// discovery goroutine resolves the served version.
	if w.dynamicClient != nil {
		w.registry.Reserve("HelmRelease")
	}

	// Wire all informer-backed kinds through the registry. Each kind's GVR
	// comes from its ResourceDescriptor; the registry picks the typed factory
	// for in-scheme GVRs and falls through to the dynamic factory for CRDs.
	gvrs := make([]schema.GroupVersionResource, 0, len(informedKinds))
	for _, kind := range informedKinds {
		rd, ok := Resolve(kind)
		if !ok {
			continue
		}
		gvr := rd.GVR()
		gvrs = append(gvrs, gvr)
		w.wireInformer(w.registry.Register(gvr, kind), gvr)
	}

	w.registry.Start(ctx)

	// Coalesce ticker — drains dirty kinds every coalesceWindow.
	go w.coalesceLoop()

	// Per-kind cache-sync events. Pods is the primary informer: as soon as
	// Pods is synced we drop the "Connecting…" splash via CacheSyncedMsg. The
	// rest stream KindSyncedMsg events as they each finish their initial LIST,
	// so the user can navigate to other kinds and see "Syncing <kind>…" until
	// that informer's data is ready.
	go w.runCacheSyncWaits(ctx, gvrs)

	// Helm GVR discovery and dynamic informer setup run in the background so
	// that namespace/context switches (which call NewWatcherFactory on the UI
	// goroutine) are not blocked by the API-server round-trips in
	// discoverHelmReleaseGVR.
	go w.runHelmReleaseAsync(ctx)
}

// wireInformer attaches the watch-error handler and event handler that
// translate informer events into Bubbletea messages. Both must be set before
// the informer starts; the registry's Start kicks the factories afterwards.
func (w *WatcherFactory) wireInformer(informer cache.SharedIndexInformer, gvr schema.GroupVersionResource) {
	kind := w.registry.KindFor(gvr)
	_ = informer.SetWatchErrorHandler(w.watchErrHandler(kind))
	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{ //nolint:errcheck
		AddFunc:    func(_ interface{}) { w.markDirty(kind) },
		UpdateFunc: func(_, _ interface{}) { w.markDirty(kind) },
		DeleteFunc: func(_ interface{}) { w.markDirty(kind) },
	})
}

// runCacheSyncWaits awaits the initial LIST for each registered GVR and
// emits the corresponding sync message. Pod is the primary informer that
// drives the "Connecting…" splash via CacheSyncedMsg; everything else
// streams KindSyncedMsg.
func (w *WatcherFactory) runCacheSyncWaits(ctx context.Context, gvrs []schema.GroupVersionResource) {
	podGVR, _ := w.registry.GVRFor("Pod")
	if pods := w.registry.Informer(podGVR); pods != nil {
		if cache.WaitForCacheSync(ctx.Done(), pods.HasSynced) {
			w.registry.MarkSynced(podGVR)
			w.mu.Lock()
			w.syncing = false
			w.kindsSynced["Pod"] = true
			w.mu.Unlock()
			select {
			case w.msgCh <- CacheSyncedMsg{}:
			default:
			}
		}
	}
	for _, gvr := range gvrs {
		if gvr == podGVR {
			continue
		}
		inf := w.registry.Informer(gvr)
		if inf == nil {
			continue
		}
		if cache.WaitForCacheSync(ctx.Done(), inf.HasSynced) {
			w.registry.MarkSynced(gvr)
			kind := w.registry.KindFor(gvr)
			w.mu.Lock()
			w.kindsSynced[kind] = true
			w.mu.Unlock()
			select {
			case w.msgCh <- KindSyncedMsg{Kind: kind}:
			default:
			}
		}
	}
}

// runHelmReleaseAsync discovers the FluxCD HelmRelease GVR, registers the
// dynamic informer through the same registry path as the built-in kinds,
// and awaits its initial LIST. Failure modes (no FluxCD installed, no
// permission) surface as AccessDeniedMsg via the registered watch-error
// handler — the same code path as any other kind.
func (w *WatcherFactory) runHelmReleaseAsync(ctx context.Context) {
	if w.dynamicClient == nil {
		return
	}
	gvr := discoverHelmReleaseGVR(w.restCfg)
	w.mu.Lock()
	w.hrGVR = gvr
	w.mu.Unlock()
	informer := w.registry.Register(gvr, "HelmRelease")
	w.wireInformer(informer, gvr)
	w.registry.Start(ctx)
	if cache.WaitForCacheSync(ctx.Done(), informer.HasSynced) {
		w.registry.MarkSynced(gvr)
		w.mu.Lock()
		w.kindsSynced["HelmRelease"] = true
		w.mu.Unlock()
		select {
		case w.msgCh <- KindSyncedMsg{Kind: "HelmRelease"}:
		default:
		}
	}
}

// Stop cancels the informers and goroutines for this factory.
func (w *WatcherFactory) Stop() {
	if w.coalesceStop != nil {
		select {
		case <-w.coalesceStop:
			// already closed
		default:
			close(w.coalesceStop)
		}
	}
	if w.cancel != nil {
		w.cancel()
	}
}

// WatchCmd returns a tea.Cmd that reads from msgCh and relays to the Bubbletea loop.
func WatchCmd(msgCh <-chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		return <-msgCh
	}
}
