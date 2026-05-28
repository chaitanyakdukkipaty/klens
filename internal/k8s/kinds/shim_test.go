package kinds

import (
	"context"
	"testing"

	tea "charm.land/bubbletea/v2"
	k8s "github.com/chaitanyak/klens/internal/k8s"
)

// fakeNoCaps is the truth-table reference for "Kind with no capabilities."
// Mirrors how a freshly-added kind looks before any capability is implemented.
type fakeNoCaps struct{}

func (fakeNoCaps) Meta() Meta                                                     { return Meta{Kind: "TruthTableA", Plural: "truthtablesa"} }
func (fakeNoCaps) Columns() []k8s.Column                                          { return nil }
func (fakeNoCaps) List(Context) ([]Row, error)                                    { return nil, nil }
func (fakeNoCaps) Fetch(context.Context, Context, string, string) (Object, error) { return nil, nil }

// fakeFullCaps is the truth-table reference for "Kind with every capability."
// Mirrors Pod's eventual shape — Logger, Attacher, Scaler, Deleter, Killer,
// PortForwarder, Topologer, Applier, Suspender.
type fakeFullCaps struct{ fakeNoCaps }

func (fakeFullCaps) Meta() Meta                                                       { return Meta{Kind: "TruthTableB", Plural: "truthtablesb"} }
func (fakeFullCaps) LogTargets(Context, string, string) ([]LogTarget, error)          { return nil, nil }
func (fakeFullCaps) Attach(Context, string, string) tea.Cmd                           { return nil }
func (fakeFullCaps) Scale(Context, string, string, int32) error                       { return nil }
func (fakeFullCaps) CurrentReplicas(Object) int32                                     { return 0 }
func (fakeFullCaps) Delete(Context, string, string) error                             { return nil }
func (fakeFullCaps) Kill(Context, string, string) error                               { return nil }
func (fakeFullCaps) ContainerPorts(Context, string, string) ([]ContainerPort, error)  { return nil, nil }
func (fakeFullCaps) Topology(Context, string, string) (*k8s.TreeNode, error)          { return nil, nil }
func (fakeFullCaps) Apply(Context, string, string, []byte) error                      { return nil }
func (fakeFullCaps) Suspend(Context, string, string, bool) error                      { return nil }

// TestCapabilityAssertions asserts that direct type assertions on Kind
// values surface every capability for a full-cap type and none for a
// no-cap type. This is the same guard the old shim helper provided, kept
// because plan 08's risk section called out silent capability disappearance.
func TestCapabilityAssertions(t *testing.T) {
	cases := []struct {
		name string
		k    Kind
		caps map[string]bool
	}{
		{
			name: "no capabilities",
			k:    fakeNoCaps{},
			caps: map[string]bool{},
		},
		{
			name: "every capability",
			k:    fakeFullCaps{},
			caps: map[string]bool{
				"Logger":           true,
				"Attacher":         true,
				"Scaler":           true,
				"Deleter":          true,
				"Killer":           true,
				"PortForwarder":    true,
				"Topologer":        true,
				"Applier":          true,
				"Suspender":        true,
			},
		},
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			_, isLogger := any(c.k).(Logger)
			_, isAttacher := any(c.k).(Attacher)
			_, isScaler := any(c.k).(Scaler)
			_, isDeleter := any(c.k).(Deleter)
			_, isKiller := any(c.k).(Killer)
			_, isPF := any(c.k).(PortForwarder)
			_, isTopo := any(c.k).(Topologer)
			_, isApplier := any(c.k).(Applier)
			_, isSuspender := any(c.k).(Suspender)
			got := map[string]bool{
				"Logger":        isLogger,
				"Attacher":      isAttacher,
				"Scaler":        isScaler,
				"Deleter":       isDeleter,
				"Killer":        isKiller,
				"PortForwarder": isPF,
				"Topologer":     isTopo,
				"Applier":       isApplier,
				"Suspender":     isSuspender,
			}
			for k := range c.caps {
				if !got[k] {
					t.Errorf("expected %s capability to be satisfied", k)
				}
			}
			for k, v := range got {
				if v && !c.caps[k] {
					t.Errorf("unexpected %s capability satisfied", k)
				}
			}
		})
	}
}

// TestLegacyDescriptorMetadata confirms the surviving shim still copies
// Kind, Plural, Aliases, and Columns from the Kind onto the legacy
// ResourceDescriptor so model.go::listRows + setStatusBarKind keep
// resolving kinds by name.
func TestLegacyDescriptorMetadata(t *testing.T) {
	rd := legacyDescriptorFrom(fakeNoCaps{})
	if rd.Kind != "TruthTableA" || rd.Plural != "truthtablesa" {
		t.Errorf("descriptor: kind=%q plural=%q", rd.Kind, rd.Plural)
	}
	if rd.ListRows == nil {
		t.Errorf("expected ListRows wired")
	}
	if rd.Fetch == nil {
		t.Errorf("expected Fetch wired")
	}
	if rd.BuildTopology != nil {
		t.Errorf("BuildTopology should be nil for no-caps kind")
	}
	rdFull := legacyDescriptorFrom(fakeFullCaps{})
	if rdFull.BuildTopology == nil {
		t.Errorf("BuildTopology should be wired for Topologer")
	}
}
