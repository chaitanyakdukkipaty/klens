package klenstests

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	k8s "github.com/chaitanyak/klens/internal/k8s"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
)

// TestWatcherFactoryAcceptsFakeClientset proves the client seam: NewWatcherFactory
// takes kubernetes.Interface, so fake.NewSimpleClientset drops in unchanged.
// Three pods seeded → three pods listed from the informer cache. If a future
// refactor narrows the signature back to *kubernetes.Clientset, this test
// stops compiling.
func TestWatcherFactoryAcceptsFakeClientset(t *testing.T) {
	cs := fake.NewSimpleClientset(
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "a", Namespace: "ns"}},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "b", Namespace: "ns"}},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "c", Namespace: "ns"}},
	)

	// rest.Config has to be non-nil because Start spins up a dynamic client
	// for HelmRelease discovery; a zero-value Config is enough — the
	// discovery goroutine fails silently against the fake transport. msgCh is
	// buffered so the cache-sync goroutine doesn't block when nothing reads.
	msgCh := make(chan tea.Msg, 64)
	wf := k8s.NewWatcherFactory(cs, &rest.Config{}, "ns", msgCh)
	wf.Start()
	defer wf.Stop()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if wf.KindSynced("Pod") {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !wf.KindSynced("Pod") {
		t.Fatal("Pod informer never synced from fake clientset")
	}

	pods := k8s.ListAs[*corev1.Pod](wf, "Pod", "ns")
	if len(pods) != 3 {
		t.Fatalf("ListAs[Pod] returned %d pods, want 3", len(pods))
	}
}

// TestFetchFuncAcceptsFakeClientset proves the FetchFunc seam: the Pod
// descriptor's Fetch closure, executed against a fake clientset, returns the
// seeded pod. Mirrors the action-side coverage in panels_actions_test for the
// read-side path the YAML viewer uses.
func TestFetchFuncAcceptsFakeClientset(t *testing.T) {
	cs := fake.NewSimpleClientset(
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "p1", Namespace: "ns"}},
	)
	rd, ok := k8s.Resolve("Pod")
	if !ok || rd.Fetch == nil {
		t.Fatal("Pod descriptor missing Fetch handler")
	}
	obj, err := rd.Fetch(cs, "p1", "ns")
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}
	got, ok := obj.(*corev1.Pod)
	if !ok {
		t.Fatalf("Fetch returned %T, want *corev1.Pod", obj)
	}
	if got.Name != "p1" || got.Namespace != "ns" {
		t.Errorf("got pod %s/%s, want ns/p1", got.Namespace, got.Name)
	}

	// Negative path: a missing object surfaces an error rather than a
	// stale-looking nil value.
	if _, err := rd.Fetch(cs, "ghost", "ns"); err == nil {
		t.Error("Fetch of missing pod should error")
	}
}
