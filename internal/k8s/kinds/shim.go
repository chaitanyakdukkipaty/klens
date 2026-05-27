package kinds

import (
	"context"
	"fmt"

	k8s "github.com/chaitanyak/klens/internal/k8s"
	"k8s.io/client-go/kubernetes"
)

// legacyDescriptorFrom builds a k8s.ResourceDescriptor equivalent to what
// the static k8s.Registry held for `k.Meta().Kind`. Every migrated Kind
// goes through this once at init time; the resulting descriptor is appended
// to k8s.Registry via RegisterDescriptor so the unmigrated callers
// (model.go::listRows, buildTopology) keep working unchanged.
//
// Now that capability presence is checked via direct interface assertion at
// every action site, this shim no longer derives Supports* booleans or
// wires an Actions map — its only remaining job is to expose Columns +
// ListRows + Fetch + BuildTopology so the unmigrated dispatch sites in
// model.go::listRows / buildTopology still resolve. Step 6 of plan 01
// deletes this file entirely.
func legacyDescriptorFrom(k Kind) k8s.ResourceDescriptor {
	m := k.Meta()
	rd := k8s.ResourceDescriptor{
		Kind:       m.Kind,
		Plural:     m.Plural,
		Aliases:    m.Aliases,
		Namespaced: m.Namespaced,
		APIGroup:   m.GVR.Group,
		APIVersion: m.GVR.Version,
		Columns:    k.Columns(),
	}
	rd.ListRows = bridgeListRows(k)
	rd.Fetch = bridgeFetch(k)
	if t, ok := any(k).(Topologer); ok {
		rd.BuildTopology = bridgeTopology(t)
	}
	return rd
}

// bridgeListRows adapts a Kind's List(Context) into the legacy ListRowsFunc
// shape that model.listRows expects. The closure wraps a WatcherFactory +
// namespace + RowContext into a kinds.Context, calls Kind.List, and returns
// the rows directly (Row is a type alias to k8s.ResourceRow).
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

// register adds k to the kinds Default registry and appends its shim
// descriptor to k8s.Registry. Panics on collision because every Kind here
// is supposed to own a unique key.
func register(k Kind) {
	Default.Register(k)
	rd := legacyDescriptorFrom(k)
	if !k8s.RegisterDescriptor(rd) {
		panic(fmt.Sprintf("kinds: %q already in k8s.Registry — remove the static entry before registering the Kind", rd.Kind))
	}
}
