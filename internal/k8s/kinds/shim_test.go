package kinds

import (
	"context"
	"testing"

	tea "charm.land/bubbletea/v2"
	k8s "github.com/chaitanyak/klens/internal/k8s"
)

// fakeNoCaps is the truth-table reference for "Kind with no capabilities."
// Mirrors how a freshly-added kind (no actions, no logs, no topology) looks
// before any capability is implemented.
type fakeNoCaps struct{}

func (fakeNoCaps) Meta() Meta { return Meta{Kind: "TruthTableA", Plural: "truthtablesa"} }
func (fakeNoCaps) Columns() []k8s.Column                                          { return nil }
func (fakeNoCaps) List(Context) ([]Row, error)                                    { return nil, nil }
func (fakeNoCaps) Fetch(context.Context, Context, string, string) (Object, error) { return nil, nil }

// fakeFullCaps is the truth-table reference for "Kind with every capability."
// Mirrors Pod's eventual shape — Logger, Attacher, Scaler, Deleter,
// PortForwarder, Topologer, Applier.
type fakeFullCaps struct{ fakeNoCaps }

func (fakeFullCaps) Meta() Meta { return Meta{Kind: "TruthTableB", Plural: "truthtablesb"} }
func (fakeFullCaps) LogTargets(Context, string, string) ([]LogTarget, error)          { return nil, nil }
func (fakeFullCaps) Attach(Context, string, string) tea.Cmd                          { return nil }
func (fakeFullCaps) Scale(Context, string, string, int32) error                       { return nil }
func (fakeFullCaps) CurrentReplicas(Object) int32                                     { return 0 }
func (fakeFullCaps) Delete(Context, string, string) error                             { return nil }
func (fakeFullCaps) ContainerPorts(Context, string, string) ([]ContainerPort, error)  { return nil, nil }
func (fakeFullCaps) Topology(Context, string, string) (*k8s.TreeNode, error)          { return nil, nil }
func (fakeFullCaps) Apply(Context, string, string, []byte) error                      { return nil }

// TestImplementsTruthTable asserts the shim's interface-satisfaction
// detection matches a hand-written truth table. This is the keystone check
// the plan calls out (risk: "implements[T] helper is wrong … UI hints
// silently disappear for migrated kinds").
func TestImplementsTruthTable(t *testing.T) {
	cases := []struct {
		name           string
		k              Kind
		wantLogs       bool
		wantAttach     bool
		wantScale      bool
		wantDelete     bool
		wantPF         bool
		wantTopology   bool
		wantApply      bool
	}{
		{name: "no capabilities", k: fakeNoCaps{}},
		{
			name:         "every capability",
			k:            fakeFullCaps{},
			wantLogs:     true,
			wantAttach:   true,
			wantScale:    true,
			wantDelete:   true,
			wantPF:       true,
			wantTopology: true,
			wantApply:    true,
		},
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			if got := implements[Logger](c.k); got != c.wantLogs {
				t.Errorf("Logger: got %v want %v", got, c.wantLogs)
			}
			if got := implements[Attacher](c.k); got != c.wantAttach {
				t.Errorf("Attacher: got %v want %v", got, c.wantAttach)
			}
			if got := implements[Scaler](c.k); got != c.wantScale {
				t.Errorf("Scaler: got %v want %v", got, c.wantScale)
			}
			if got := implements[Deleter](c.k); got != c.wantDelete {
				t.Errorf("Deleter: got %v want %v", got, c.wantDelete)
			}
			if got := implements[PortForwarder](c.k); got != c.wantPF {
				t.Errorf("PortForwarder: got %v want %v", got, c.wantPF)
			}
			if got := implements[Topologer](c.k); got != c.wantTopology {
				t.Errorf("Topologer: got %v want %v", got, c.wantTopology)
			}
			if got := implements[Applier](c.k); got != c.wantApply {
				t.Errorf("Applier: got %v want %v", got, c.wantApply)
			}
		})
	}
}

// TestLegacyDescriptorFromBools confirms that legacyDescriptorFrom maps
// every capability interface to the matching Supports* boolean on the
// generated ResourceDescriptor. Catches a missed mapping in shim.go that
// would silently disable a UI hint or a help-bar entry.
func TestLegacyDescriptorFromBools(t *testing.T) {
	t.Run("no capabilities", func(t *testing.T) {
		rd := legacyDescriptorFrom(fakeNoCaps{})
		if rd.SupportsYAML != true {
			t.Errorf("SupportsYAML should be true (Fetch is always present)")
		}
		if rd.SupportsLogs || rd.SupportsAttach || rd.SupportsScale ||
			rd.SupportsDeletion || rd.SupportsPortForward || rd.SupportsTopology {
			t.Errorf("expected no capability bools set, got %+v", rd)
		}
	})
	t.Run("every capability", func(t *testing.T) {
		rd := legacyDescriptorFrom(fakeFullCaps{})
		want := []struct {
			name string
			got  bool
		}{
			{"SupportsYAML", rd.SupportsYAML},
			{"SupportsLogs", rd.SupportsLogs},
			{"SupportsAttach", rd.SupportsAttach},
			{"SupportsScale", rd.SupportsScale},
			{"SupportsDeletion", rd.SupportsDeletion},
			{"SupportsPortForward", rd.SupportsPortForward},
			{"SupportsTopology", rd.SupportsTopology},
		}
		for _, w := range want {
			if !w.got {
				t.Errorf("%s should be true on full-capability kind", w.name)
			}
		}
		// And the action map: full-cap kind should have delete, scale, apply.
		for _, op := range []string{"delete", "scale", "apply"} {
			if rd.Actions[op] == nil {
				t.Errorf("expected Actions[%q] to be wired", op)
			}
		}
	})
}
