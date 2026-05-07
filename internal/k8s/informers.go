package k8s

import (
	"context"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	corev1 "k8s.io/api/core/v1"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
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
	factory        informers.SharedInformerFactory
	dynamicClient  dynamic.Interface
	dynamicFactory dynamicinformer.DynamicSharedInformerFactory
	hrGVR          schema.GroupVersionResource
	restCfg        *rest.Config // stored for async Helm GVR discovery in Start()
	cancel         context.CancelFunc
	msgCh          chan tea.Msg
	started        bool
	syncing        bool
	accessDenied   map[string]struct{}
	kindsSynced    map[string]bool // per-kind initial LIST completion
	mu             sync.RWMutex

	// Event coalescing: instead of emitting one ResourceUpdatedMsg per
	// informer event, dirtyKinds accumulates kinds touched within the last
	// coalesceWindow and a ticker flushes them as one msg per kind.
	coalesceMu   sync.Mutex
	dirtyKinds   map[string]struct{}
	coalesceStop chan struct{}
}

// NewWatcherFactory creates a factory attached to the given clientset and rest config.
func NewWatcherFactory(cs *kubernetes.Clientset, cfg *rest.Config, namespace string, msgCh chan tea.Msg) *WatcherFactory {
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
		factory:        factory,
		dynamicClient:  dc,
		dynamicFactory: dynFactory,
		hrGVR:          helmReleaseGVR, // will be overwritten by async discovery in Start()
		restCfg:        cfg,
		msgCh:          msgCh,
		accessDenied:   make(map[string]struct{}),
		kindsSynced:    make(map[string]bool),
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

// trackedKinds is the canonical set of resource kinds this WatcherFactory runs
// informers for. KindSynced returns true (= "ready, do not gate UI") for any
// kind outside this set so a future nav-panel addition that forgets to set up
// an informer here doesn't leave the table stuck on "Syncing…" forever.
var trackedKinds = map[string]bool{
	"Pod": true, "Service": true, "Endpoints": true, "Node": true,
	"Namespace": true, "ConfigMap": true, "Secret": true, "ServiceAccount": true,
	"PersistentVolume": true, "PersistentVolumeClaim": true, "Event": true,
	"Deployment": true, "StatefulSet": true, "DaemonSet": true, "ReplicaSet": true,
	"Job": true, "CronJob": true, "Ingress": true, "NetworkPolicy": true,
	"HelmRelease": true,
}

// KindSynced reports whether the informer for `kind` has completed its initial
// LIST. The model uses this to render "Syncing <kind>…" in the table when the
// user navigates to a kind whose informer hasn't synced yet. For kinds we
// don't run an informer for, returns true so unrelated nav entries don't get
// stuck on the syncing placeholder.
func (w *WatcherFactory) KindSynced(kind string) bool {
	if !trackedKinds[kind] {
		return true
	}
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.kindsSynced[kind]
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

	// Event handler: mark the kind dirty; the coalesce loop flushes all dirty
	// kinds every coalesceWindow as a single ResourceUpdatedMsg per kind.
	handler := func(kind string) cache.ResourceEventHandlerFuncs {
		mark := func(_ interface{}) { w.markDirty(kind) }
		return cache.ResourceEventHandlerFuncs{
			AddFunc:    mark,
			UpdateFunc: func(_, obj interface{}) { mark(obj) },
			DeleteFunc: mark,
		}
	}

	// setup wires an informer with our error handler and event handler before
	// the factory is started. SetWatchErrorHandler must be called pre-Start.
	setup := func(informer cache.SharedIndexInformer, kind string) cache.SharedIndexInformer {
		_ = informer.SetWatchErrorHandler(w.watchErrHandler(kind))
		informer.AddEventHandler(handler(kind)) //nolint:errcheck
		return informer
	}

	// Track each informer alongside its kind so we can wait per-kind below.
	type kindInformer struct {
		kind string
		inf  cache.SharedIndexInformer
	}
	syncList := []kindInformer{
		{"Pod", setup(w.factory.Core().V1().Pods().Informer(), "Pod")},
		{"Service", setup(w.factory.Core().V1().Services().Informer(), "Service")},
		{"Endpoints", setup(w.factory.Core().V1().Endpoints().Informer(), "Endpoints")},
		{"Node", setup(w.factory.Core().V1().Nodes().Informer(), "Node")},
		{"Namespace", setup(w.factory.Core().V1().Namespaces().Informer(), "Namespace")},
		{"ConfigMap", setup(w.factory.Core().V1().ConfigMaps().Informer(), "ConfigMap")},
		{"Secret", setup(w.factory.Core().V1().Secrets().Informer(), "Secret")},
		{"ServiceAccount", setup(w.factory.Core().V1().ServiceAccounts().Informer(), "ServiceAccount")},
		{"PersistentVolume", setup(w.factory.Core().V1().PersistentVolumes().Informer(), "PersistentVolume")},
		{"PersistentVolumeClaim", setup(w.factory.Core().V1().PersistentVolumeClaims().Informer(), "PersistentVolumeClaim")},
		{"Event", setup(w.factory.Core().V1().Events().Informer(), "Event")},
		{"Deployment", setup(w.factory.Apps().V1().Deployments().Informer(), "Deployment")},
		{"StatefulSet", setup(w.factory.Apps().V1().StatefulSets().Informer(), "StatefulSet")},
		{"DaemonSet", setup(w.factory.Apps().V1().DaemonSets().Informer(), "DaemonSet")},
		{"ReplicaSet", setup(w.factory.Apps().V1().ReplicaSets().Informer(), "ReplicaSet")},
		{"Job", setup(w.factory.Batch().V1().Jobs().Informer(), "Job")},
		{"CronJob", setup(w.factory.Batch().V1().CronJobs().Informer(), "CronJob")},
		{"Ingress", setup(w.factory.Networking().V1().Ingresses().Informer(), "Ingress")},
		{"NetworkPolicy", setup(w.factory.Networking().V1().NetworkPolicies().Informer(), "NetworkPolicy")},
	}

	w.factory.Start(ctx.Done())

	// Coalesce ticker — drains dirty kinds every coalesceWindow.
	go w.coalesceLoop()

	// Per-kind cache-sync events. Pods is the primary informer: as soon as
	// Pods is synced we drop the "Connecting…" splash via CacheSyncedMsg. The
	// rest stream KindSyncedMsg events as they each finish their initial LIST,
	// so the user can navigate to other kinds and see "Syncing <kind>…" until
	// that informer's data is ready.
	go func() {
		// Pods first.
		pods := w.factory.Core().V1().Pods().Informer()
		if cache.WaitForCacheSync(ctx.Done(), pods.HasSynced) {
			w.mu.Lock()
			w.syncing = false
			w.kindsSynced["Pod"] = true
			w.mu.Unlock()
			select {
			case w.msgCh <- CacheSyncedMsg{}:
			default:
			}
		}
		// All other kinds, individually so each emits its own event when ready.
		for _, ki := range syncList {
			if ki.kind == "Pod" {
				continue
			}
			if cache.WaitForCacheSync(ctx.Done(), ki.inf.HasSynced) {
				w.mu.Lock()
				w.kindsSynced[ki.kind] = true
				w.mu.Unlock()
				select {
				case w.msgCh <- KindSyncedMsg{Kind: ki.kind}:
				default:
				}
			}
		}
	}()

	// Helm GVR discovery and dynamic informer setup run in the background so that
	// namespace/context switches (which call NewWatcherFactory on the UI goroutine)
	// are not blocked by the API-server round-trips in discoverHelmReleaseGVR.
	go func() {
		if w.dynamicFactory == nil {
			return
		}
		gvr := discoverHelmReleaseGVR(w.restCfg)
		w.mu.Lock()
		w.hrGVR = gvr
		w.mu.Unlock()
		// SetWatchErrorHandler must be called before dynamicFactory.Start().
		setup(w.dynamicFactory.ForResource(gvr).Informer(), "HelmRelease")
		w.dynamicFactory.Start(ctx.Done())
		w.dynamicFactory.WaitForCacheSync(ctx.Done())
		w.mu.Lock()
		w.kindsSynced["HelmRelease"] = true
		w.mu.Unlock()
		select {
		case w.msgCh <- KindSyncedMsg{Kind: "HelmRelease"}:
		default:
		}
	}()
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

// ListPods returns pods from the informer cache for the given namespace.
func (w *WatcherFactory) ListPods(namespace string) []*corev1.Pod {
	objs := w.factory.Core().V1().Pods().Informer().GetStore().List()
	out := make([]*corev1.Pod, 0, len(objs))
	for _, o := range objs {
		pod := o.(*corev1.Pod)
		if namespace == "" || namespace == "all" || pod.Namespace == namespace {
			out = append(out, pod)
		}
	}
	return out
}

func (w *WatcherFactory) ListDeployments(namespace string) []*appsv1.Deployment {
	objs := w.factory.Apps().V1().Deployments().Informer().GetStore().List()
	out := make([]*appsv1.Deployment, 0, len(objs))
	for _, o := range objs {
		d := o.(*appsv1.Deployment)
		if namespace == "" || namespace == "all" || d.Namespace == namespace {
			out = append(out, d)
		}
	}
	return out
}

func (w *WatcherFactory) ListServices(namespace string) []*corev1.Service {
	objs := w.factory.Core().V1().Services().Informer().GetStore().List()
	out := make([]*corev1.Service, 0, len(objs))
	for _, o := range objs {
		svc := o.(*corev1.Service)
		if namespace == "" || namespace == "all" || svc.Namespace == namespace {
			out = append(out, svc)
		}
	}
	return out
}

func (w *WatcherFactory) ListNodes() []*corev1.Node {
	objs := w.factory.Core().V1().Nodes().Informer().GetStore().List()
	out := make([]*corev1.Node, 0, len(objs))
	for _, o := range objs {
		out = append(out, o.(*corev1.Node))
	}
	return out
}

func (w *WatcherFactory) ListStatefulSets(namespace string) []*appsv1.StatefulSet {
	objs := w.factory.Apps().V1().StatefulSets().Informer().GetStore().List()
	out := make([]*appsv1.StatefulSet, 0)
	for _, o := range objs {
		d := o.(*appsv1.StatefulSet)
		if namespace == "" || namespace == "all" || d.Namespace == namespace {
			out = append(out, d)
		}
	}
	return out
}

func (w *WatcherFactory) ListDaemonSets(namespace string) []*appsv1.DaemonSet {
	objs := w.factory.Apps().V1().DaemonSets().Informer().GetStore().List()
	out := make([]*appsv1.DaemonSet, 0)
	for _, o := range objs {
		d := o.(*appsv1.DaemonSet)
		if namespace == "" || namespace == "all" || d.Namespace == namespace {
			out = append(out, d)
		}
	}
	return out
}

func (w *WatcherFactory) ListReplicaSets(namespace string) []*appsv1.ReplicaSet {
	objs := w.factory.Apps().V1().ReplicaSets().Informer().GetStore().List()
	out := make([]*appsv1.ReplicaSet, 0)
	for _, o := range objs {
		d := o.(*appsv1.ReplicaSet)
		if namespace == "" || namespace == "all" || d.Namespace == namespace {
			out = append(out, d)
		}
	}
	return out
}

func (w *WatcherFactory) ListJobs(namespace string) []*batchv1.Job {
	objs := w.factory.Batch().V1().Jobs().Informer().GetStore().List()
	out := make([]*batchv1.Job, 0)
	for _, o := range objs {
		d := o.(*batchv1.Job)
		if namespace == "" || namespace == "all" || d.Namespace == namespace {
			out = append(out, d)
		}
	}
	return out
}

func (w *WatcherFactory) ListCronJobs(namespace string) []*batchv1.CronJob {
	objs := w.factory.Batch().V1().CronJobs().Informer().GetStore().List()
	out := make([]*batchv1.CronJob, 0)
	for _, o := range objs {
		d := o.(*batchv1.CronJob)
		if namespace == "" || namespace == "all" || d.Namespace == namespace {
			out = append(out, d)
		}
	}
	return out
}

func (w *WatcherFactory) ListIngresses(namespace string) []*networkingv1.Ingress {
	objs := w.factory.Networking().V1().Ingresses().Informer().GetStore().List()
	out := make([]*networkingv1.Ingress, 0)
	for _, o := range objs {
		d := o.(*networkingv1.Ingress)
		if namespace == "" || namespace == "all" || d.Namespace == namespace {
			out = append(out, d)
		}
	}
	return out
}

func (w *WatcherFactory) ListConfigMaps(namespace string) []*corev1.ConfigMap {
	objs := w.factory.Core().V1().ConfigMaps().Informer().GetStore().List()
	out := make([]*corev1.ConfigMap, 0)
	for _, o := range objs {
		d := o.(*corev1.ConfigMap)
		if namespace == "" || namespace == "all" || d.Namespace == namespace {
			out = append(out, d)
		}
	}
	return out
}

func (w *WatcherFactory) ListSecrets(namespace string) []*corev1.Secret {
	objs := w.factory.Core().V1().Secrets().Informer().GetStore().List()
	out := make([]*corev1.Secret, 0)
	for _, o := range objs {
		d := o.(*corev1.Secret)
		if namespace == "" || namespace == "all" || d.Namespace == namespace {
			out = append(out, d)
		}
	}
	return out
}

func (w *WatcherFactory) ListPersistentVolumes() []*corev1.PersistentVolume {
	objs := w.factory.Core().V1().PersistentVolumes().Informer().GetStore().List()
	out := make([]*corev1.PersistentVolume, 0)
	for _, o := range objs {
		out = append(out, o.(*corev1.PersistentVolume))
	}
	return out
}

func (w *WatcherFactory) ListPVCs(namespace string) []*corev1.PersistentVolumeClaim {
	objs := w.factory.Core().V1().PersistentVolumeClaims().Informer().GetStore().List()
	out := make([]*corev1.PersistentVolumeClaim, 0)
	for _, o := range objs {
		d := o.(*corev1.PersistentVolumeClaim)
		if namespace == "" || namespace == "all" || d.Namespace == namespace {
			out = append(out, d)
		}
	}
	return out
}

func (w *WatcherFactory) ListEvents(namespace string) []*corev1.Event {
	objs := w.factory.Core().V1().Events().Informer().GetStore().List()
	out := make([]*corev1.Event, 0)
	for _, o := range objs {
		d := o.(*corev1.Event)
		if namespace == "" || namespace == "all" || d.Namespace == namespace {
			out = append(out, d)
		}
	}
	return out
}

// ListNamespaces returns namespace names from the informer cache.
// Returns an empty slice if the user lacks cluster-wide namespace listing permission.
func (w *WatcherFactory) ListNamespaces() []string {
	objs := w.factory.Core().V1().Namespaces().Informer().GetStore().List()
	out := make([]string, 0, len(objs))
	for _, o := range objs {
		out = append(out, o.(*corev1.Namespace).Name)
	}
	return out
}

// ListHelmReleases returns FluxCD HelmRelease objects from the dynamic informer cache.
func (w *WatcherFactory) ListHelmReleases(namespace string) []*unstructured.Unstructured {
	if w.dynamicFactory == nil {
		return nil
	}
	w.mu.RLock()
	gvr := w.hrGVR
	w.mu.RUnlock()
	objs := w.dynamicFactory.ForResource(gvr).Informer().GetStore().List()
	out := make([]*unstructured.Unstructured, 0, len(objs))
	for _, o := range objs {
		u, ok := o.(*unstructured.Unstructured)
		if !ok {
			continue
		}
		if namespace != "" && namespace != "all" && u.GetNamespace() != namespace {
			continue
		}
		out = append(out, u)
	}
	return out
}

// WatchCmd returns a tea.Cmd that reads from msgCh and relays to the Bubbletea loop.
func WatchCmd(msgCh <-chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		return <-msgCh
	}
}
