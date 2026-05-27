package klenstests

import (
	"context"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	k8s "github.com/chaitanyak/klens/internal/k8s"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
)

// TestCachedListerReadsFromInformerCache proves the production adapter:
// pods seeded into a fake clientset, observed through a real WatcherFactory's
// informer cache, come back through Lister.List once the Pod informer
// finishes its initial LIST. Mirrors the FakeLister test but exercises the
// cache path that production uses.
func TestCachedListerReadsFromInformerCache(t *testing.T) {
	cs := fake.NewSimpleClientset(
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "a", Namespace: "ns"}},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "b", Namespace: "ns"}},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "c", Namespace: "ns"}},
	)
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

	l := k8s.NewCachedLister(wf)
	objs, err := l.List(context.Background(), k8s.PodGVR, "ns")
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(objs) != 3 {
		t.Fatalf("List returned %d objects, want 3", len(objs))
	}
}
