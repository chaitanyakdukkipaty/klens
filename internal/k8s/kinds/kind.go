// Package kinds owns per-Kubernetes-kind behavior. Each kind is one file: it
// implements the Kind interface plus zero or more capability interfaces
// (Logger, Attacher, Scaler, Deleter, PortForwarder, Topologer, Applier,
// Suspender, MetricsSupporter). Capability presence is "does this type
// satisfy the interface" — there is no Supports* boolean. Adding a kind is
// a single file, not five coordinated edits.
//
// The dispatch helpers in dispatch.go (DeleteCmd, ScaleCmd, ApplyCmd,
// SuspendCmd) turn a (Kind, capability) tuple into a tea.Cmd that emits
// the messages model.go already handles. Action call sites in the model
// look like `kinds.DeleteCmd(k, deps, ns, name)` — a one-liner per action.
//
// During the per-kind migration (plan 01) the legacy k8s.Registry coexists
// with this package via the shim in shim.go that builds a metadata-only
// k8s.ResourceDescriptor for every Kind registered here. Step 6 of that
// plan deletes the shim and the legacy registry; model.go's remaining
// listRows / buildTopology paths then switch to kinds.Lookup directly.
package kinds

import (
	"context"

	tea "charm.land/bubbletea/v2"
	k8s "github.com/chaitanyak/klens/internal/k8s"
)

// Kind is the single owner of per-kind behavior. Every kind implements it.
// Capability interfaces below extend it; presence is checked via type
// assertion at the call site.
type Kind interface {
	Meta() Meta
	Columns() []k8s.Column
	List(ctx Context) ([]Row, error)
	Fetch(ctx context.Context, c Context, ns, name string) (Object, error)
}

// Object is the value returned by Fetch — typically a typed metav1.Object,
// occasionally an *unstructured.Unstructured (HelmRelease). Callers convert
// to YAML via the existing yaml_viewer helpers.
type Object = any

// Logger streams container logs for a workload kind.
type Logger interface {
	Kind
	// LogTargets returns the (namespace, pod, container) tuples to stream
	// when the user opens the log viewer for `name` in `ns`. The actual
	// streaming is handled by the existing k8s.LogStreamer; this method
	// describes which pods to attach to.
	LogTargets(c Context, ns, name string) ([]LogTarget, error)
}

// LogTarget identifies one pod to stream logs from. Pods themselves return a
// single target; controllers (Deployment, etc.) fan out across their pods.
type LogTarget struct {
	Namespace string
	Pod       string
	// Container is empty for "all containers in the pod"; otherwise the
	// named container.
	Container string
}

// Attacher opens an interactive exec session against a pod. Returns a
// tea.Cmd because the actual session is asynchronous and may either take
// over the screen (tea.Exec) or spawn a tmux window (TmuxAttachWindowCmd).
type Attacher interface {
	Kind
	Attach(c Context, ns, name string) tea.Cmd
}

// Scaler patches `spec.replicas` on the workload.
type Scaler interface {
	Kind
	Scale(c Context, ns, name string, replicas int32) error
	// CurrentReplicas returns the current spec.replicas value for the named
	// workload, or 1 if unknown. Used to seed the scale dialog.
	CurrentReplicas(obj Object) int32
}

// Deleter removes the named object from the cluster.
type Deleter interface {
	Kind
	Delete(c Context, ns, name string) error
}

// PortForwarder establishes a port-forward to a pod.
type PortForwarder interface {
	Kind
	// PortForward delegates to k8s.PortForwardManager — the actual SPDY
	// session lives there. This method's responsibility is checking that
	// the kind supports forwarding (pods only today) and producing the
	// container ports the dialog will offer.
	ContainerPorts(c Context, ns, name string) ([]ContainerPort, error)
}

// ContainerPort describes one exposed pod port, used to populate the
// port-forward dialog.
type ContainerPort struct {
	Name string // container port name; may be empty
	Port int32  // container port number
}

// Topologer returns a topology tree rooted at the named resource.
type Topologer interface {
	Kind
	Topology(c Context, ns, name string) (*k8s.TreeNode, error)
}

// Applier patches a kind with raw merge-patch JSON. Every kind that supports
// YAML edit must implement this; the apply path dispatches through this
// interface (via kinds.ApplyCmd).
type Applier interface {
	Kind
	Apply(c Context, ns, name string, mergePatchJSON []byte) error
}

// MetricsSupporter marks a kind as openable in the metrics panel. The
// method is intentionally informational rather than dispatch-shaped — the
// metrics panel reads c.Metrics directly; UI hint code asks "does this
// Kind satisfy MetricsSupporter?" to decide whether to surface the "m"
// keybinding.
type MetricsSupporter interface {
	Kind
	MetricsKey(ns, name string) string
}

// Suspender toggles spec.suspend on a kind (today: HelmRelease only). The
// boolean parameter avoids a second symmetric "Resumer" interface — the
// suspend/resume distinction is one bit, not two methods.
type Suspender interface {
	Kind
	Suspend(c Context, ns, name string, suspend bool) error
}
