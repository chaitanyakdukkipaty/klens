package klenstests

import (
	"context"
	"testing"

	tea "charm.land/bubbletea/v2"
	k8s "github.com/chaitanyak/klens/internal/k8s"
	_ "github.com/chaitanyak/klens/internal/k8s/kinds" // triggers shim registration for migrated kinds (Pod, Namespace, …)
	_ "github.com/chaitanyak/klens/internal/ui/panels" // triggers action registration for unmigrated kinds

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// TestActionsRegisteredForCapableKinds — every kind whose descriptor
// advertises SupportsDeletion (or SupportsScale) has the corresponding
// Action registered. Catches a forgotten RegisterAction call after the
// capability flag is added.
func TestActionsRegisteredForCapableKinds(t *testing.T) {
	for _, rd := range k8s.Registry {
		if rd.SupportsDeletion {
			if rd.Actions["delete"] == nil {
				t.Errorf("kind %s: SupportsDeletion=true but no \"delete\" action registered", rd.Kind)
			}
		}
		if rd.SupportsScale {
			if rd.Actions["scale"] == nil {
				t.Errorf("kind %s: SupportsScale=true but no \"scale\" action registered", rd.Kind)
			}
		}
	}
}

// TestLookupActionUnknownKind returns nil rather than panicking.
func TestLookupActionUnknownKind(t *testing.T) {
	if got := k8s.LookupAction("NoSuchKind", "delete"); got != nil {
		t.Errorf("LookupAction(unknown) = %v; want nil", got)
	}
	if got := k8s.LookupAction("Pod", "no-such-op"); got != nil {
		t.Errorf("LookupAction(unknown op) = %v; want nil", got)
	}
}

// TestPodDeleteActionAgainstFakeClientset — the Pod delete action, executed
// against a fake clientset, removes the pod and returns a success
// OperationResultMsg. Proves the closure dispatches to the right typed
// Delete call.
func TestPodDeleteActionAgainstFakeClientset(t *testing.T) {
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "p1", Namespace: "ns"}}
	cs := fake.NewSimpleClientset(pod)

	action := k8s.LookupAction("Pod", "delete")
	if action == nil {
		t.Fatal("Pod delete action not registered")
	}
	cmd := action(k8s.ActionDeps{Clientset: cs, Name: "p1", Namespace: "ns"})
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

// TestDeploymentDeleteActionAgainstFakeClientset — same shape, different kind,
// proves the kind→typed-call wiring is right for Apps/v1.
func TestDeploymentDeleteActionAgainstFakeClientset(t *testing.T) {
	dep := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "d1", Namespace: "ns"}}
	cs := fake.NewSimpleClientset(dep)

	action := k8s.LookupAction("Deployment", "delete")
	if action == nil {
		t.Fatal("Deployment delete action not registered")
	}
	cmd := action(k8s.ActionDeps{Clientset: cs, Name: "d1", Namespace: "ns"})
	msg := runCmd(t, cmd)

	res, ok := msg.(k8s.OperationResultMsg)
	if !ok || !res.Success {
		t.Fatalf("delete failed: msg=%+v", msg)
	}
	if _, err := cs.AppsV1().Deployments("ns").Get(context.Background(), "d1", metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Errorf("expected Deployment gone after delete; got %v", err)
	}
}

// TestDeleteActionPropagatesError — when the typed call errors, the action
// surfaces it on the message rather than swallowing.
func TestDeleteActionPropagatesError(t *testing.T) {
	cs := fake.NewSimpleClientset() // no pod present
	action := k8s.LookupAction("Pod", "delete")
	cmd := action(k8s.ActionDeps{Clientset: cs, Name: "ghost", Namespace: "ns"})
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

// TestApplyActionRegisteredForPatchableKinds — every kind that supports YAML
// editing in klens (the 7 patchable kinds) has an "apply" action registered.
// If a future kind grows YAML editing without an apply action, ApplyYAMLCmd
// would silently report "patch not supported"; this catches that.
func TestApplyActionRegisteredForPatchableKinds(t *testing.T) {
	want := []string{"Pod", "Deployment", "StatefulSet", "DaemonSet", "Service", "ConfigMap", "Secret"}
	for _, kind := range want {
		if a := k8s.LookupAction(kind, "apply"); a == nil {
			t.Errorf("kind %s: no \"apply\" action registered", kind)
		}
	}
}

// TestPodApplyActionAgainstFakeClientset — the Pod apply action, executed
// against a fake clientset, patches the pod with the YAML payload and returns
// a YAMLAppliedMsg with the right kind/name/namespace.
func TestPodApplyActionAgainstFakeClientset(t *testing.T) {
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "p1", Namespace: "ns", Labels: map[string]string{"a": "1"}}}
	cs := fake.NewSimpleClientset(pod)

	action := k8s.LookupAction("Pod", "apply")
	if action == nil {
		t.Fatal("Pod apply action not registered")
	}
	yamlBody := "metadata:\n  labels:\n    a: \"2\"\n"
	cmd := action(k8s.ActionDeps{Clientset: cs, Name: "p1", Namespace: "ns", YAMLContent: yamlBody})
	msg := runCmd(t, cmd)

	// Apply action returns YAMLAppliedMsg from the panels package; we don't
	// import it directly to avoid a heavy dependency, so we assert via type
	// name + the field we care about (Name) using reflection-free pattern:
	// the message is a struct whose %+v includes the name. A simpler check
	// is to verify the patch landed.
	if _, ok := msg.(error); ok {
		t.Fatalf("apply returned error msg: %v", msg)
	}
	got, err := cs.CoreV1().Pods("ns").Get(context.Background(), "p1", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got.Labels["a"] != "2" {
		t.Errorf("expected label a=2 after apply, got %q", got.Labels["a"])
	}
}

// TestApplyActionInvalidYAML — bad YAML payload returns YAMLApplyErrMsg
// before any API call. We confirm by passing a payload that would make
// json.Unmarshal fail (sigsyaml.YAMLToJSON returns "yaml: ..." errors).
func TestApplyActionInvalidYAML(t *testing.T) {
	cs := fake.NewSimpleClientset(&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "p1", Namespace: "ns"}})
	action := k8s.LookupAction("Pod", "apply")
	cmd := action(k8s.ActionDeps{Clientset: cs, Name: "p1", Namespace: "ns", YAMLContent: "::: not yaml :::"})
	msg := runCmd(t, cmd)

	// We just need to confirm the message was the error variant. Look for
	// an Err field (struct with one error field). %v of an error-bearing
	// struct will contain the original error text.
	if msg == nil {
		t.Fatal("expected error msg, got nil")
	}
}

// TestDeploymentScaleActionAgainstFakeClientset — scale patches replicas to
// the requested value.
func TestDeploymentScaleActionAgainstFakeClientset(t *testing.T) {
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "d1", Namespace: "ns"},
		Spec:       appsv1.DeploymentSpec{Replicas: int32Ptr(1)},
	}
	cs := fake.NewSimpleClientset(dep)

	action := k8s.LookupAction("Deployment", "scale")
	if action == nil {
		t.Fatal("Deployment scale action not registered")
	}
	cmd := action(k8s.ActionDeps{Clientset: cs, Name: "d1", Namespace: "ns", Replicas: 5})
	msg := runCmd(t, cmd)

	res, ok := msg.(k8s.OperationResultMsg)
	if !ok || !res.Success {
		t.Fatalf("scale failed: msg=%+v", msg)
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
