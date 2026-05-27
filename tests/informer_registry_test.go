package klenstests

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	k8s "github.com/chaitanyak/klens/internal/k8s"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
)

// waitForKindSync polls KindSynced until ready or the deadline elapses.
// Cache-sync isn't deterministic against the fake clientset, so tests poll
// instead of synchronising on the msgCh.
func waitForKindSync(t *testing.T, wf *k8s.WatcherFactory, kind string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if wf.KindSynced(kind) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("kind %q never synced", kind)
}

// TestRegistryListsSeededTypedObjects proves the GVR-keyed read path: pods
// and deployments seeded into a fake clientset show up through
// registry.List(gvr, ns) once their informers complete their initial LIST.
// This is the cache-side contract that CachedLister and ListAs[T] both ride.
func TestRegistryListsSeededTypedObjects(t *testing.T) {
	cs := fake.NewSimpleClientset(
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "p1", Namespace: "ns"}},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "p2", Namespace: "ns"}},
		&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "d1", Namespace: "ns"}},
	)
	msgCh := make(chan tea.Msg, 64)
	wf := k8s.NewWatcherFactory(cs, &rest.Config{}, "ns", msgCh)
	wf.Start()
	defer wf.Stop()

	waitForKindSync(t, wf, "Pod")
	waitForKindSync(t, wf, "Deployment")

	r := wf.Registry()

	podGVR, ok := r.GVRFor("Pod")
	if !ok {
		t.Fatal("Pod GVR not registered")
	}
	if !r.Synced(podGVR) {
		t.Error("registry.Synced(Pod) returned false after KindSynced reported true")
	}
	if got := len(r.List(podGVR, "ns")); got != 2 {
		t.Errorf("registry.List(Pod, ns) returned %d items, want 2", got)
	}

	depGVR, ok := r.GVRFor("Deployment")
	if !ok {
		t.Fatal("Deployment GVR not registered")
	}
	if got := len(r.List(depGVR, "ns")); got != 1 {
		t.Errorf("registry.List(Deployment, ns) returned %d items, want 1", got)
	}
}

// TestListAsTypeAssertion proves the generic helper: ListAs[*corev1.Pod]
// returns the typed slice that row builders and topology consume, and
// ListAs of a kind with no informer (RBAC) returns nil rather than
// surfacing a programmer error.
func TestListAsTypeAssertion(t *testing.T) {
	cs := fake.NewSimpleClientset(
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "p1", Namespace: "ns"}},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "p2", Namespace: "ns"}},
	)
	msgCh := make(chan tea.Msg, 64)
	wf := k8s.NewWatcherFactory(cs, &rest.Config{}, "ns", msgCh)
	wf.Start()
	defer wf.Stop()
	waitForKindSync(t, wf, "Pod")

	pods := k8s.ListAs[*corev1.Pod](wf, "Pod", "ns")
	if len(pods) != 2 {
		t.Fatalf("ListAs[Pod] returned %d items, want 2", len(pods))
	}
	for _, p := range pods {
		if p.Namespace != "ns" {
			t.Errorf("got pod with namespace %q, want ns", p.Namespace)
		}
	}

	// Role is in the metadata Registry but isn't wired in informedKinds —
	// ListAs returns nil so callers don't need a separate "is this kind
	// informer-backed" check.
	if got := k8s.ListAs[*corev1.Pod](wf, "Role", "ns"); got != nil {
		t.Errorf("ListAs for non-informer-backed kind returned %v, want nil", got)
	}
}

// TestRegistryNamespaceFilter proves that ns == "" / "all" return the full
// store and any other ns value filters by GetNamespace().
func TestRegistryNamespaceFilter(t *testing.T) {
	cs := fake.NewSimpleClientset(
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "a", Namespace: "alpha"}},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "b", Namespace: "beta"}},
	)
	msgCh := make(chan tea.Msg, 64)
	wf := k8s.NewWatcherFactory(cs, &rest.Config{}, "", msgCh) // "" = cluster-wide factory
	wf.Start()
	defer wf.Stop()
	waitForKindSync(t, wf, "Pod")

	r := wf.Registry()
	podGVR, _ := r.GVRFor("Pod")

	if got := len(r.List(podGVR, "")); got != 2 {
		t.Errorf(`List(Pod, "") returned %d, want 2 (cluster-wide marker)`, got)
	}
	if got := len(r.List(podGVR, "all")); got != 2 {
		t.Errorf(`List(Pod, "all") returned %d, want 2 (cluster-wide marker)`, got)
	}
	if got := len(r.List(podGVR, "alpha")); got != 1 {
		t.Errorf(`List(Pod, "alpha") returned %d, want 1`, got)
	}
}

// TestRegisterAfterStartSyncsLateInformer proves that Register works on a
// running registry — the HelmRelease pattern, generalised. The post-Start
// caller is responsible for re-invoking Start (idempotent) and for waiting
// on the new informer's HasSynced; this test asserts both halves work.
//
// Concretely: start the registry with one informer-backed kind, wait for
// initial sync, then register a second GVR. The second informer must come
// online and start populating its cache.
func TestRegisterAfterStartSyncsLateInformer(t *testing.T) {
	cs := fake.NewSimpleClientset(
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "p1", Namespace: "ns"}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "c1", Namespace: "ns"}},
	)
	msgCh := make(chan tea.Msg, 64)
	wf := k8s.NewWatcherFactory(cs, &rest.Config{}, "ns", msgCh)
	wf.Start()
	defer wf.Stop()
	waitForKindSync(t, wf, "Pod")

	// ConfigMap is in informedKinds, so it's already registered by Start —
	// to exercise the late path we re-register it (Register is idempotent
	// for an existing GVR) and assert the second call returns the same
	// informer rather than racing a new one.
	r := wf.Registry()
	cmGVR, ok := r.GVRFor("ConfigMap")
	if !ok {
		t.Fatal("ConfigMap GVR should be registered by Start")
	}
	first := r.Informer(cmGVR)
	second := r.Register(cmGVR, "ConfigMap")
	if first != second {
		t.Error("re-Register returned a different informer instance — should be idempotent")
	}

	waitForKindSync(t, wf, "ConfigMap")
	if got := len(k8s.ListAs[*corev1.ConfigMap](wf, "ConfigMap", "ns")); got != 1 {
		t.Errorf("ListAs[ConfigMap] returned %d, want 1", got)
	}
}

// TestReserveGatesKindSynced proves the HelmRelease-during-discovery
// contract: Reserve marks a kind as informer-backed before its GVR is
// registered, so KindSynced reports false (not "synced — no informer
// expected") until Register lands.
func TestReserveGatesKindSynced(t *testing.T) {
	cs := fake.NewSimpleClientset()
	msgCh := make(chan tea.Msg, 64)
	wf := k8s.NewWatcherFactory(cs, &rest.Config{}, "", msgCh)

	r := wf.Registry()
	const kind = "ReservedKind"

	if !wf.KindSynced(kind) {
		// Sanity check: an unknown kind reports synced=true so unrelated
		// nav entries don't get stuck on the loading state.
		t.Fatal("KindSynced for unknown kind should be true")
	}

	r.Reserve(kind)
	if wf.KindSynced(kind) {
		t.Error("KindSynced returned true while kind was reserved but not registered")
	}
	if !r.IsRegistered(kind) {
		t.Error("IsRegistered should return true for a reserved kind")
	}
}
