package kinds

import (
	"context"
	"fmt"

	tea "charm.land/bubbletea/v2"
	k8s "github.com/chaitanyak/klens/internal/k8s"
	"github.com/chaitanyak/klens/internal/ui/panels"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/kubernetes"
	sigsyaml "sigs.k8s.io/yaml"
)

// implements[T] reports whether k satisfies the capability interface T. Used
// by the shim to derive Supports* booleans (and Action wiring) from interface
// satisfaction. The two-step assignment is mandatory: Go's type-assertion
// generic helper needs the explicit conversion to `any(k)` to inspect the
// dynamic type — a direct `k.(T)` would not compile for a Kind value.
func implements[T any](k Kind) bool {
	_, ok := any(k).(T)
	return ok
}

// legacyDescriptorFrom builds a k8s.ResourceDescriptor equivalent to what
// the static k8s.Registry held for `k.Meta().Kind`. Every migrated Kind
// goes through this once at init time; the resulting descriptor is appended
// to k8s.Registry via RegisterDescriptor so the unmigrated callers
// (model.go::listRows, setStatusBarKind, LookupAction) keep working
// unchanged.
//
// This is the keystone of the kind-by-kind migration: as long as the shim
// builds an equivalent descriptor (handlers + actions + Supports* bools),
// the rest of the codebase can't tell the difference between a static
// registry entry and a Kind-derived one. Step 6 deletes this file entirely.
func legacyDescriptorFrom(k Kind) k8s.ResourceDescriptor {
	m := k.Meta()
	rd := k8s.ResourceDescriptor{
		Kind:                m.Kind,
		Plural:              m.Plural,
		Aliases:             m.Aliases,
		Namespaced:          m.Namespaced,
		APIGroup:            m.GVR.Group,
		APIVersion:          m.GVR.Version,
		Columns:             k.Columns(),
		SupportsYAML:        true, // every Kind implements Fetch
		SupportsLogs:        implements[Logger](k),
		SupportsTopology:    implements[Topologer](k),
		SupportsAttach:      implements[Attacher](k),
		SupportsScale:       implements[Scaler](k),
		SupportsDeletion:    implements[Deleter](k),
		SupportsPortForward: implements[PortForwarder](k),
		SupportsMetrics:     implements[MetricsSupporter](k),
	}
	rd.ListRows = bridgeListRows(k)
	rd.Fetch = bridgeFetch(k)
	if t, ok := any(k).(Topologer); ok {
		rd.BuildTopology = bridgeTopology(t)
	}
	rd.Actions = bridgeActions(k)
	return rd
}

// bridgeListRows adapts a Kind's List(Context) into the legacy ListRowsFunc
// shape that model.listRows / setStatusBarKind expect. The closure wraps a
// WatcherFactory + namespace + RowContext into a kinds.Context, calls
// Kind.List, and returns the rows directly (Row is a type alias to
// k8s.ResourceRow).
func bridgeListRows(k Kind) k8s.ListRowsFunc {
	return func(wf *k8s.WatcherFactory, ns string, rctx k8s.RowContext) []k8s.ResourceRow {
		c := Context{
			Ctx:       context.Background(),
			Namespace: ns,
			Lister:    k8s.NewCachedLister(wf),
			Metrics:   rctx.Metrics,
			PFActive:  rctx.PortForwardActive,
			HelmGVR:   wf.HelmReleaseGVR(),
		}
		rows, err := k.List(c)
		if err != nil {
			return nil
		}
		return rows
	}
}

// bridgeFetch adapts Kind.Fetch into the legacy FetchFunc shape. The legacy
// signature drops the context argument; we recreate it as Background.
func bridgeFetch(k Kind) k8s.FetchFunc {
	return func(cs kubernetes.Interface, name, ns string) (any, error) {
		// HelmGVR can't be threaded here without a WatcherFactory handle;
		// the legacy FetchFunc signature doesn't carry one. HelmRelease's
		// YAML view is currently routed through model.go::ActiveKind ==
		// "HelmRelease" → FetchHelmReleaseYAMLCmd (using the cached
		// unstructured), so this fallback isn't hot. Step 6 of plan 01
		// replaces the legacy signature entirely.
		c := Context{Ctx: context.Background(), Clientset: cs, Namespace: ns}
		return k.Fetch(c.Ctx, c, ns, name)
	}
}

// bridgeTopology lifts Topologer onto the legacy BuildTopologyFunc.
func bridgeTopology(t Topologer) k8s.BuildTopologyFunc {
	return func(wf *k8s.WatcherFactory, ns, name string) *k8s.TreeNode {
		c := Context{
			Ctx:       context.Background(),
			Namespace: ns,
			Lister:    k8s.NewCachedLister(wf),
			HelmGVR:   wf.HelmReleaseGVR(),
		}
		tree, err := t.Topology(c, ns, name)
		if err != nil {
			return nil
		}
		return tree
	}
}

// bridgeActions builds the Actions map for a Kind by interface-asserting
// every capability it supports. The set of op keys matches what the legacy
// panels/kinds.go registered: "delete" (Deleter), "scale" (Scaler), "apply"
// (Applier). Kinds without the capability simply don't get the entry.
func bridgeActions(k Kind) map[string]k8s.Action {
	actions := map[string]k8s.Action{}
	if d, ok := any(k).(Deleter); ok {
		actions["delete"] = deleteAction(d)
	}
	if s, ok := any(k).(Scaler); ok {
		actions["scale"] = scaleAction(s)
	}
	if a, ok := any(k).(Applier); ok {
		actions["apply"] = applyAction(k.Meta().Kind, a)
	}
	return actions
}

// deleteAction wraps Deleter.Delete into the legacy Action signature.
// Mirrors the closure pattern in internal/ui/panels/kinds.go::deleteAction
// so the OperationResultMsg the model expects continues to arrive
// identically.
func deleteAction(d Deleter) k8s.Action {
	return func(deps k8s.ActionDeps) tea.Cmd {
		return func() tea.Msg {
			c := contextFromDeps(deps)
			if err := d.Delete(c, deps.Namespace, deps.Name); err != nil {
				return k8s.OperationResultMsg{Operation: "delete", Resource: deps.Name, Err: err}
			}
			return k8s.OperationResultMsg{Operation: "delete", Resource: deps.Name, Success: true}
		}
	}
}

// scaleAction wraps Scaler.Scale into the legacy Action signature.
func scaleAction(s Scaler) k8s.Action {
	return func(deps k8s.ActionDeps) tea.Cmd {
		return func() tea.Msg {
			c := contextFromDeps(deps)
			if err := s.Scale(c, deps.Namespace, deps.Name, deps.Replicas); err != nil {
				return k8s.OperationResultMsg{Operation: "scale", Resource: deps.Name, Err: err}
			}
			return k8s.OperationResultMsg{Operation: "scale", Resource: deps.Name, Success: true}
		}
	}
}

// applyAction wraps Applier.Apply into the legacy Action signature, including
// the YAML→JSON conversion and forbidden-error tagging the legacy closure did.
func applyAction(kind string, a Applier) k8s.Action {
	return func(deps k8s.ActionDeps) tea.Cmd {
		return func() tea.Msg {
			body, err := sigsyaml.YAMLToJSON([]byte(deps.YAMLContent))
			if err != nil {
				return panels.YAMLApplyErrMsg{Err: fmt.Errorf("invalid YAML: %w", err)}
			}
			c := contextFromDeps(deps)
			if err := a.Apply(c, deps.Namespace, deps.Name, body); err != nil {
				if k8serrors.IsForbidden(err) {
					return panels.YAMLApplyErrMsg{Err: fmt.Errorf("forbidden: %w", err)}
				}
				return panels.YAMLApplyErrMsg{Err: err}
			}
			return panels.YAMLAppliedMsg{Kind: kind, Name: deps.Name, Namespace: deps.Namespace}
		}
	}
}

// contextFromDeps converts the legacy ActionDeps bag (clientset, dynamic,
// helm GVR) into the kinds.Context every capability method consumes.
func contextFromDeps(deps k8s.ActionDeps) Context {
	return Context{
		Ctx:       context.Background(),
		Namespace: deps.Namespace,
		Clientset: deps.Clientset,
		Dynamic:   deps.Dynamic,
		HelmGVR:   deps.HelmGVR,
	}
}

// register adds k to the kinds Default registry and appends its shim
// descriptor to k8s.Registry. Idempotent against k8s.Registry on duplicates
// (RegisterDescriptor returns false) but does panic on collision because
// every Kind here is supposed to own a unique key.
func register(k Kind) {
	Default.Register(k)
	rd := legacyDescriptorFrom(k)
	if !k8s.RegisterDescriptor(rd) {
		panic(fmt.Sprintf("kinds: %q already in k8s.Registry — remove the static entry before registering the Kind", rd.Kind))
	}
}

