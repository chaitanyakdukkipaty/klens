package klenstests

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/chaitanyak/klens/internal/app"
	"github.com/chaitanyak/klens/internal/ui/widgets"

	corev1 "k8s.io/api/core/v1"
)

// TestPodContainerEntriesSingle — a one-container pod produces one entry,
// which is the signal actionAttach uses to skip the picker.
func TestPodContainerEntriesSingle(t *testing.T) {
	pod := &corev1.Pod{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "app"}},
		},
	}
	entries := app.PodContainerEntries(pod)
	if len(entries) != 1 {
		t.Fatalf("len(entries) = %d, want 1", len(entries))
	}
	if entries[0].Name != "app" || entries[0].Kind != widgets.ContainerRegular {
		t.Errorf("entry = %+v, want {app, Regular}", entries[0])
	}
}

// TestPodContainerEntriesMulti — a multi-container pod surfaces every
// container, including init and ephemeral, tagged by kind and in spec order.
// This is the case where actionAttach opens the picker.
func TestPodContainerEntriesMulti(t *testing.T) {
	pod := &corev1.Pod{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: "app"},
				{Name: "sidecar"},
			},
			InitContainers: []corev1.Container{
				{Name: "wait-db"},
			},
			EphemeralContainers: []corev1.EphemeralContainer{
				{EphemeralContainerCommon: corev1.EphemeralContainerCommon{Name: "debug"}},
			},
		},
	}
	entries := app.PodContainerEntries(pod)

	got := []widgets.ContainerEntry{
		{Name: "app", Kind: widgets.ContainerRegular},
		{Name: "sidecar", Kind: widgets.ContainerRegular},
		{Name: "wait-db", Kind: widgets.ContainerInit},
		{Name: "debug", Kind: widgets.ContainerEphemeral},
	}
	if len(entries) != len(got) {
		t.Fatalf("len(entries) = %d, want %d", len(entries), len(got))
	}
	for i := range got {
		if entries[i] != got[i] {
			t.Errorf("entries[%d] = %+v, want %+v", i, entries[i], got[i])
		}
	}
}

// TestPodContainerEntriesNil — a nil pod yields nil, matching the
// actionAttach fallback path that lets the API server pick the default.
func TestPodContainerEntriesNil(t *testing.T) {
	if got := app.PodContainerEntries(nil); got != nil {
		t.Errorf("PodContainerEntries(nil) = %v, want nil", got)
	}
}

// TestContainerPickerEnterEmitsPicked — selecting a container with enter
// emits ContainerPickedMsg carrying the pod, namespace, and chosen name.
func TestContainerPickerEnterEmitsPicked(t *testing.T) {
	p := widgets.NewContainerPicker().Show("p1", "ns", []widgets.ContainerEntry{
		{Name: "app", Kind: widgets.ContainerRegular},
		{Name: "sidecar", Kind: widgets.ContainerRegular},
	})

	// Move cursor down then press enter — should pick "sidecar".
	p, _ = p.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	p, cmd := p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	if p.IsVisible() {
		t.Error("picker should hide after enter")
	}
	if cmd == nil {
		t.Fatal("enter should return a command")
	}
	msg := cmd()
	picked, ok := msg.(widgets.ContainerPickedMsg)
	if !ok {
		t.Fatalf("got %T, want ContainerPickedMsg", msg)
	}
	if picked.Pod != "p1" || picked.Namespace != "ns" || picked.Container != "sidecar" {
		t.Errorf("picked = %+v, want {p1, ns, sidecar}", picked)
	}
}

// TestContainerPickerEscCancels — esc dismisses the picker and emits
// ContainerPickerCancelMsg so callers can drop the attach flow cleanly.
func TestContainerPickerEscCancels(t *testing.T) {
	p := widgets.NewContainerPicker().Show("p1", "ns", []widgets.ContainerEntry{
		{Name: "app", Kind: widgets.ContainerRegular},
	})
	p, cmd := p.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if p.IsVisible() {
		t.Error("picker should hide after esc")
	}
	if cmd == nil {
		t.Fatal("esc should return a command")
	}
	if _, ok := cmd().(widgets.ContainerPickerCancelMsg); !ok {
		t.Errorf("got %T, want ContainerPickerCancelMsg", cmd())
	}
}
