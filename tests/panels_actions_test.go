package klenstests

import (
	"context"
	"testing"

	tea "charm.land/bubbletea/v2"
	k8s "github.com/chaitanyak/klens/internal/k8s"
	"github.com/chaitanyak/klens/internal/k8s/kinds"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// TestKindCapabilitiesAdvertisedThroughInterfaces — every kind that has a
// destructive capability (delete, scale, suspend, apply) advertises it via
// the matching interface. After plan 08 the type system enforces this — a
// kind that claims a capability in its file but skips the method won't
// compile. This test is the runtime guard for the soft contract: "every
// kind that should have capability X actually implements X".
func TestKindCapabilitiesAdvertisedThroughInterfaces(t *testing.T) {
	wantDeleter := []string{
		"Pod", "Deployment", "StatefulSet", "DaemonSet", "ReplicaSet",
		"Service", "ConfigMap", "Secret", "Namespace", "Job", "CronJob",
		"PersistentVolumeClaim", "Ingress", "PersistentVolume",
	}
	for _, name := range wantDeleter {
		k, ok := kinds.Lookup(name)
		if !ok {
			t.Errorf("kind %s not registered", name)
			continue
		}
		if _, ok := any(k).(kinds.Deleter); !ok {
			t.Errorf("kind %s should implement Deleter", name)
		}
	}
	for _, name := range []string{"Deployment", "StatefulSet", "ReplicaSet"} {
		k, _ := kinds.Lookup(name)
		if _, ok := any(k).(kinds.Scaler); !ok {
			t.Errorf("kind %s should implement Scaler", name)
		}
	}
	for _, name := range []string{"Pod", "Deployment", "StatefulSet", "DaemonSet", "Service", "ConfigMap", "Secret"} {
		k, _ := kinds.Lookup(name)
		if _, ok := any(k).(kinds.Applier); !ok {
			t.Errorf("kind %s should implement Applier", name)
		}
	}
}

// TestPodDeleteViaDispatcherAgainstFakeClientset — the Pod delete dispatch,
// executed against a fake clientset, removes the pod and returns a success
// OperationResultMsg. Proves the kinds.DeleteCmd path dispatches to the
// kind's typed Delete call.
func TestPodDeleteViaDispatcherAgainstFakeClientset(t *testing.T) {
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "p1", Namespace: "ns"}}
	cs := fake.NewSimpleClientset(pod)

	k, _ := kinds.Lookup("Pod")
	cmd := kinds.DeleteCmd(k, kinds.Deps{Clientset: cs}, "ns", "p1")
	if cmd == nil {
		t.Fatal("DeleteCmd returned nil for Pod (should be Deleter)")
	}
	msg := runCmd(t, cmd)

	res, ok := msg.(k8s.OperationResultMsg)
	if !ok {
		t.Fatalf("got %T, want OperationResultMsg", msg)
	}
	if !res.Success || res.Err != nil {
		t.Fatalf("delete reported failure: success=%v err=%v", res.Success, res.Err)
	}
	if res.Operation != "delete" || res.Resource != "p1" {
		t.Errorf("operation=%q resource=%q; want delete/p1", res.Operation, res.Resource)
	}

	if _, err := cs.CoreV1().Pods("ns").Get(context.Background(), "p1", metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Errorf("expected Pod gone after delete; got %v", err)
	}
}

// TestDeploymentDeleteViaDispatcher — same shape, different kind, proves the
// kind→typed-call wiring is right for Apps/v1.
func TestDeploymentDeleteViaDispatcher(t *testing.T) {
	dep := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "d1", Namespace: "ns"}}
	cs := fake.NewSimpleClientset(dep)

	k, _ := kinds.Lookup("Deployment")
	cmd := kinds.DeleteCmd(k, kinds.Deps{Clientset: cs}, "ns", "d1")
	msg := runCmd(t, cmd)

	res, ok := msg.(k8s.OperationResultMsg)
	if !ok || !res.Success {
		t.Fatalf("delete failed: msg=%+v", msg)
	}
	if _, err := cs.AppsV1().Deployments("ns").Get(context.Background(), "d1", metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Errorf("expected Deployment gone after delete; got %v", err)
	}
}

// TestDeleteDispatchPropagatesError — when the typed call errors, the
// dispatcher surfaces it on the message rather than swallowing.
func TestDeleteDispatchPropagatesError(t *testing.T) {
	cs := fake.NewSimpleClientset() // no pod present
	k, _ := kinds.Lookup("Pod")
	cmd := kinds.DeleteCmd(k, kinds.Deps{Clientset: cs}, "ns", "ghost")
	msg := runCmd(t, cmd)

	res, ok := msg.(k8s.OperationResultMsg)
	if !ok {
		t.Fatalf("got %T, want OperationResultMsg", msg)
	}
	if res.Success {
		t.Error("delete of non-existent pod reported success")
	}
	if res.Err == nil {
		t.Error("expected non-nil error")
	}
}

// TestPodApplyDispatcher — the Pod apply dispatch, executed against a fake
// clientset, patches the pod with the YAML payload and returns a
// YAMLAppliedMsg with the right kind/name/namespace.
func TestPodApplyDispatcher(t *testing.T) {
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "p1", Namespace: "ns", Labels: map[string]string{"a": "1"}}}
	cs := fake.NewSimpleClientset(pod)

	k, _ := kinds.Lookup("Pod")
	yamlBody := "metadata:\n  labels:\n    a: \"2\"\n"
	cmd := kinds.ApplyCmd(k, kinds.Deps{Clientset: cs}, "ns", "p1", yamlBody)
	msg := runCmd(t, cmd)

	applied, ok := msg.(kinds.YAMLAppliedMsg)
	if !ok {
		t.Fatalf("got %T, want YAMLAppliedMsg", msg)
	}
	if applied.Kind != "Pod" || applied.Name != "p1" || applied.Namespace != "ns" {
		t.Errorf("YAMLAppliedMsg = %+v", applied)
	}

	got, err := cs.CoreV1().Pods("ns").Get(context.Background(), "p1", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got.Labels["a"] != "2" {
		t.Errorf("expected label a=2 after apply, got %q", got.Labels["a"])
	}
}

// TestApplyDispatcherInvalidYAML — bad YAML payload returns YAMLApplyErrMsg
// before any API call.
func TestApplyDispatcherInvalidYAML(t *testing.T) {
	cs := fake.NewSimpleClientset(&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "p1", Namespace: "ns"}})
	k, _ := kinds.Lookup("Pod")
	cmd := kinds.ApplyCmd(k, kinds.Deps{Clientset: cs}, "ns", "p1", "::: not yaml :::")
	msg := runCmd(t, cmd)
	if _, ok := msg.(kinds.YAMLApplyErrMsg); !ok {
		t.Fatalf("got %T, want YAMLApplyErrMsg", msg)
	}
}

// TestDeploymentScaleDispatcher — scale patches replicas to the requested value.
func TestDeploymentScaleDispatcher(t *testing.T) {
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "d1", Namespace: "ns"},
		Spec:       appsv1.DeploymentSpec{Replicas: int32Ptr(1)},
	}
	cs := fake.NewSimpleClientset(dep)

	k, _ := kinds.Lookup("Deployment")
	cmd := kinds.ScaleCmd(k, kinds.Deps{Clientset: cs}, "ns", "d1", 5)
	if cmd == nil {
		t.Fatal("ScaleCmd returned nil for Deployment")
	}
	msg := runCmd(t, cmd)

	res, ok := msg.(k8s.OperationResultMsg)
	if !ok || !res.Success {
		t.Fatalf("scale failed: msg=%+v", msg)
	}
}

// TestDispatchHelpersShortCircuitOnMissingCapability — DeleteCmd against a
// Kind that does not implement Deleter returns a nil tea.Cmd so the caller
// can surface a "not supported" message rather than dispatching a no-op.
func TestDispatchHelpersShortCircuitOnMissingCapability(t *testing.T) {
	// Namespace does not implement Scaler.
	k, ok := kinds.Lookup("Namespace")
	if !ok {
		t.Fatal("Namespace kind not registered")
	}
	if cmd := kinds.ScaleCmd(k, kinds.Deps{}, "", "default", 3); cmd != nil {
		t.Errorf("ScaleCmd on Namespace should return nil, got %T", cmd)
	}
}

// runCmd executes a tea.Cmd and returns the message it produces. tea.Cmd is
// a no-arg func returning tea.Msg, so we just call it.
func runCmd(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	if cmd == nil {
		t.Fatal("nil command")
	}
	return cmd()
}
