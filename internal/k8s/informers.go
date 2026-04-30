package k8s

import (
	"context"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
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

// discoverHelmReleaseGVR returns the GVR for whichever FluxCD HelmRelease API
// version the cluster serves, falling back to the v2 default if discovery fails.
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

// WatcherFactory manages SharedIndexInformer instances for one cluster.
type WatcherFactory struct {
	factory        informers.SharedInformerFactory
	dynamicClient  dynamic.Interface
	dynamicFactory dynamicinformer.DynamicSharedInformerFactory
	hrGVR          schema.GroupVersionResource
	cancel         context.CancelFunc
	msgCh          chan tea.Msg
	started        bool
	accessDenied   map[string]struct{}
	mu             sync.RWMutex
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
		hrGVR:          discoverHelmReleaseGVR(cfg),
		msgCh:          msgCh,
		accessDenied:   make(map[string]struct{}),
	}
}

// DynamicClient returns the dynamic Kubernetes client.
func (w *WatcherFactory) DynamicClient() dynamic.Interface { return w.dynamicClient }

// HelmReleaseGVR returns the discovered GVR for HelmRelease on this cluster.
func (w *WatcherFactory) HelmReleaseGVR() schema.GroupVersionResource { return w.hrGVR }

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

// Start begins all informers. Should be called once per factory lifetime.
func (w *WatcherFactory) Start() {
	if w.started {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	w.cancel = cancel
	w.started = true

	notify := func(kind string) func(interface{}) {
		return func(_ interface{}) {
			select {
			case w.msgCh <- ResourceUpdatedMsg{Kind: kind}:
			default:
			}
		}
	}

	handler := func(kind string) cache.ResourceEventHandlerFuncs {
		n := notify(kind)
		return cache.ResourceEventHandlerFuncs{
			AddFunc:    n,
			UpdateFunc: func(_, obj interface{}) { n(obj) },
			DeleteFunc: n,
		}
	}

	// setup wires an informer with our error handler and event handler before
	// the factory is started. SetWatchErrorHandler must be called pre-Start.
	setup := func(informer cache.SharedIndexInformer, kind string) {
		_ = informer.SetWatchErrorHandler(w.watchErrHandler(kind))
		informer.AddEventHandler(handler(kind)) //nolint:errcheck
	}

	// Core
	setup(w.factory.Core().V1().Pods().Informer(), "Pod")
	setup(w.factory.Core().V1().Services().Informer(), "Service")
	setup(w.factory.Core().V1().Endpoints().Informer(), "Endpoints")
	setup(w.factory.Core().V1().Nodes().Informer(), "Node")
	setup(w.factory.Core().V1().Namespaces().Informer(), "Namespace")
	setup(w.factory.Core().V1().ConfigMaps().Informer(), "ConfigMap")
	setup(w.factory.Core().V1().Secrets().Informer(), "Secret")
	setup(w.factory.Core().V1().ServiceAccounts().Informer(), "ServiceAccount")
	setup(w.factory.Core().V1().PersistentVolumes().Informer(), "PersistentVolume")
	setup(w.factory.Core().V1().PersistentVolumeClaims().Informer(), "PersistentVolumeClaim")
	setup(w.factory.Core().V1().Events().Informer(), "Event")

	// Apps
	setup(w.factory.Apps().V1().Deployments().Informer(), "Deployment")
	setup(w.factory.Apps().V1().StatefulSets().Informer(), "StatefulSet")
	setup(w.factory.Apps().V1().DaemonSets().Informer(), "DaemonSet")
	setup(w.factory.Apps().V1().ReplicaSets().Informer(), "ReplicaSet")

	// Batch
	setup(w.factory.Batch().V1().Jobs().Informer(), "Job")
	setup(w.factory.Batch().V1().CronJobs().Informer(), "CronJob")

	// Networking
	setup(w.factory.Networking().V1().Ingresses().Informer(), "Ingress")
	setup(w.factory.Networking().V1().NetworkPolicies().Informer(), "NetworkPolicy")

	w.factory.Start(ctx.Done())
	go func() {
		w.factory.WaitForCacheSync(ctx.Done())
	}()

	// HelmRelease CRD (FluxCD helm.toolkit.fluxcd.io/v2) — gracefully degrades if not installed.
	if w.dynamicFactory != nil {
		setup(w.dynamicFactory.ForResource(w.hrGVR).Informer(), "HelmRelease")
		w.dynamicFactory.Start(ctx.Done())
		go func() { w.dynamicFactory.WaitForCacheSync(ctx.Done()) }()
	}
}

// Stop cancels the informers and goroutines for this factory.
func (w *WatcherFactory) Stop() {
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
	objs := w.dynamicFactory.ForResource(w.hrGVR).Informer().GetStore().List()
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
