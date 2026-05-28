package kinds

import (
	"context"
	"fmt"

	tea "charm.land/bubbletea/v2"
	k8s "github.com/chaitanyak/klens/internal/k8s"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	sigsyaml "sigs.k8s.io/yaml"
)

// Deps is the cluster-handle bag every dispatch helper consumes. Operation
// payloads (replica count, YAML body) are explicit arguments on the helpers
// rather than fields on Deps.
type Deps struct {
	Clientset kubernetes.Interface
	Dynamic   dynamic.Interface
	HelmGVR   schema.GroupVersionResource
}

// DeleteCmd returns a tea.Cmd that runs Deleter.Delete on the kind and emits
// an OperationResultMsg. Returns a nil cmd when the kind does not implement
// Deleter so callers can short-circuit ("not supported for X").
func DeleteCmd(k Kind, deps Deps, ns, name string) tea.Cmd {
	d, ok := any(k).(Deleter)
	if !ok {
		return nil
	}
	c := contextFor(deps, ns)
	return func() tea.Msg {
		if err := d.Delete(c, ns, name); err != nil {
			return k8s.OperationResultMsg{Operation: "delete", Resource: name, Err: err}
		}
		return k8s.OperationResultMsg{Operation: "delete", Resource: name, Success: true}
	}
}

// KillCmd returns a tea.Cmd that force-deletes the named object (grace=0).
// Kinds implementing Killer use that path; kinds with only Deleter fall back
// to DeleteCmd since grace=0 is a no-op for non-Pod resources. Returns nil
// only when the kind implements neither.
func KillCmd(k Kind, deps Deps, ns, name string) tea.Cmd {
	if kl, ok := any(k).(Killer); ok {
		c := contextFor(deps, ns)
		return func() tea.Msg {
			if err := kl.Kill(c, ns, name); err != nil {
				return k8s.OperationResultMsg{Operation: "kill", Resource: name, Err: err}
			}
			return k8s.OperationResultMsg{Operation: "kill", Resource: name, Success: true}
		}
	}
	return DeleteCmd(k, deps, ns, name)
}

// ScaleCmd returns a tea.Cmd that runs Scaler.Scale on the kind.
func ScaleCmd(k Kind, deps Deps, ns, name string, replicas int32) tea.Cmd {
	s, ok := any(k).(Scaler)
	if !ok {
		return nil
	}
	c := contextFor(deps, ns)
	return func() tea.Msg {
		if err := s.Scale(c, ns, name, replicas); err != nil {
			return k8s.OperationResultMsg{Operation: "scale", Resource: name, Err: err}
		}
		return k8s.OperationResultMsg{Operation: "scale", Resource: name, Success: true}
	}
}

// SuspendCmd returns a tea.Cmd that toggles spec.suspend via Suspender.
// suspend=true → "suspend"; false → "resume". The OperationResultMsg's
// Operation field follows the verb.
func SuspendCmd(k Kind, deps Deps, ns, name string, suspend bool) tea.Cmd {
	s, ok := any(k).(Suspender)
	if !ok {
		return nil
	}
	op := "resume"
	if suspend {
		op = "suspend"
	}
	c := contextFor(deps, ns)
	return func() tea.Msg {
		if err := s.Suspend(c, ns, name, suspend); err != nil {
			return k8s.OperationResultMsg{Operation: op, Resource: name, Err: err}
		}
		return k8s.OperationResultMsg{Operation: op, Resource: name, Success: true}
	}
}

// ApplyCmd patches the named object via Applier with the supplied YAML
// payload (converted to JSON merge-patch). Returns the same
// YAMLApplied/YAMLApplyErr message shape the editor's apply flow expects.
func ApplyCmd(k Kind, deps Deps, ns, name, yamlContent string) tea.Cmd {
	a, ok := any(k).(Applier)
	if !ok {
		return func() tea.Msg {
			return YAMLApplyErrMsg{Err: fmt.Errorf("patch not supported for kind %s", k.Meta().Kind)}
		}
	}
	kindName := k.Meta().Kind
	c := contextFor(deps, ns)
	return func() tea.Msg {
		body, err := sigsyaml.YAMLToJSON([]byte(yamlContent))
		if err != nil {
			return YAMLApplyErrMsg{Err: fmt.Errorf("invalid YAML: %w", err)}
		}
		if err := a.Apply(c, ns, name, body); err != nil {
			if k8serrors.IsForbidden(err) {
				return YAMLApplyErrMsg{Err: fmt.Errorf("forbidden: %w", err)}
			}
			return YAMLApplyErrMsg{Err: err}
		}
		return YAMLAppliedMsg{Kind: kindName, Name: name, Namespace: ns}
	}
}

func contextFor(deps Deps, ns string) Context {
	return Context{
		Ctx:       context.Background(),
		Namespace: ns,
		Clientset: deps.Clientset,
		Dynamic:   deps.Dynamic,
		HelmGVR:   deps.HelmGVR,
	}
}
