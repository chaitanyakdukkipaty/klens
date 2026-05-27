package k8s

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

// ActionDeps carries everything an Action might need to execute. Callers
// populate the fields they have; closures pull what they need.
//
// Clientset is the kubernetes.Interface (not the concrete *Clientset) so
// tests can pass a fake clientset.
type ActionDeps struct {
	Clientset   kubernetes.Interface
	Dynamic     dynamic.Interface
	HelmGVR     schema.GroupVersionResource
	Name        string
	Namespace   string
	Replicas    int32  // for scale operations
	YAMLContent string // for apply operations — raw YAML payload to merge-patch
}

// Action returns a Bubbletea command that executes the operation. The op
// name (the key under ResourceDescriptor.Actions) describes intent;
// implementations are kind-specific.
type Action func(ActionDeps) tea.Cmd

// RowContext carries cross-cutting state needed by row extractors.
// It is passed verbatim from caller (model) to descriptor.ListRows.
type RowContext struct {
	Metrics MetricsUpdatedMsg
	// PortForwardActive reports whether the named pod has at least one
	// active port-forward session. Optional — nil means "no PF state to
	// show", in which case the PF column renders an inactive marker.
	PortForwardActive func(namespace, name string) bool
}

// ListRowsFunc returns rows for a kind, given a watcher and namespace.
// Implementations type-assert internally to the concrete Kubernetes type.
type ListRowsFunc func(wf *WatcherFactory, namespace string, ctx RowContext) []ResourceRow

// FetchFunc retrieves a live object by name (used by the YAML viewer).
//
// The clientset is the kubernetes.Interface (not the concrete *Clientset) so
// tests can swap in fake.NewSimpleClientset.
type FetchFunc func(cs kubernetes.Interface, name, namespace string) (any, error)

// BuildTopologyFunc returns a topology tree rooted at the named resource.
type BuildTopologyFunc func(wf *WatcherFactory, namespace, name string) *TreeNode

// ResourceDescriptor describes a Kubernetes resource type.
//
// The metadata fields (Kind, Plural, Columns, Supports*) are static and live
// in the Registry. The behavior closures (ListRows, Fetch, BuildTopology) are
// attached via SetHandlers from a registration site so that per-kind code can
// live in one place even when it depends on the UI layer (lipgloss-rendered
// percentages, etc.) that the k8s package itself doesn't import.
type ResourceDescriptor struct {
	Kind             string
	Plural           string
	APIGroup         string
	APIVersion       string
	Namespaced       bool
	Aliases          []string
	Columns          []Column
	SupportsYAML        bool
	SupportsLogs        bool
	SupportsTopology    bool
	SupportsMetrics     bool
	SupportsAttach      bool
	SupportsScale       bool
	SupportsDeletion    bool
	SupportsPortForward bool

	// Behavior — populated via SetHandlers, optional per kind.
	ListRows      ListRowsFunc
	Fetch         FetchFunc
	BuildTopology BuildTopologyFunc

	// Actions are op-name-keyed closures: "delete", "scale", "suspend",
	// "resume", etc. The executeConfirmedOp path looks up the op on the
	// descriptor and runs it — no kind-keyed switch in operations.go or in
	// the model. Populated via RegisterAction.
	Actions map[string]Action
}

// Column defines a table column for a resource type.
// When Flex is true, Width is the minimum width and the column expands into
// any surplus horizontal space available to the table.
// When Scrollable is true, the resource table offers horizontal scrolling on
// this column (←/→ keys and horizontal wheel ticks shift its rune offset).
// At most one column per resource should be marked Scrollable; the first one
// wins if multiple are set.
type Column struct {
	Header     string
	Width      int
	Flex       bool
	Scrollable bool
}

// ResourceRow is a single row in the resource table.
type ResourceRow struct {
	Name       string
	Namespace  string
	Status     string    // used as color key for status styling
	Age        string
	SortByTime time.Time // when non-zero, WithRows sorts descending by this instead of Name
	Values     []string  // ordered display values matching column layout; when set, buildRow uses these
	Extra      []string  // additional column values (legacy)
	Raw        interface{} // underlying k8s object
}

// Registry is the static list of all known resource types.
var Registry = []ResourceDescriptor{
	{Kind: "Pod", Plural: "pods", Namespaced: true, Aliases: []string{"po"},
		Columns: []Column{
				{"NAME", 40, true, false}, {"PF", 3, false, false}, {"READY", 6, false, false}, {"STATUS", 15, false, false}, {"RESTARTS", 9, false, false}, {"AGE", 6, false, false},
				{"CPU", 6, false, false}, {"%CPU/R", 7, false, false}, {"%CPU/L", 7, false, false},
				{"MEM", 7, false, false}, {"%MEM/R", 7, false, false}, {"%MEM/L", 7, false, false},
			},
		SupportsYAML:        true,
		SupportsLogs:        true,
		SupportsMetrics:     true,
		SupportsAttach:      true,
		SupportsDeletion:    true,
		SupportsPortForward: true,
	},
	{Kind: "Deployment", Plural: "deployments", APIGroup: "apps", Namespaced: true, Aliases: []string{"deploy", "dp"},
		Columns:          []Column{{"NAME", 40, true, false}, {"READY", 10, false, false}, {"UP-TO-DATE", 12, false, false}, {"AVAILABLE", 12, false, false}, {"AGE", 10, false, false}},
		SupportsYAML:     true,
		SupportsLogs:     true,
		SupportsTopology: true,
		SupportsScale:    true,
		SupportsDeletion: true,
	},
	{Kind: "StatefulSet", Plural: "statefulsets", APIGroup: "apps", Namespaced: true, Aliases: []string{"sts"},
		Columns:          []Column{{"NAME", 40, true, false}, {"READY", 10, false, false}, {"AGE", 10, false, false}},
		SupportsYAML:     true,
		SupportsLogs:     true,
		SupportsScale:    true,
		SupportsDeletion: true,
	},
	{Kind: "DaemonSet", Plural: "daemonsets", APIGroup: "apps", Namespaced: true, Aliases: []string{"ds"},
		Columns:          []Column{{"NAME", 40, true, false}, {"DESIRED", 10, false, false}, {"READY", 8, false, false}, {"UP-TO-DATE", 12, false, false}, {"AGE", 10, false, false}},
		SupportsYAML:     true,
		SupportsLogs:     true,
		SupportsDeletion: true,
	},
	{Kind: "ReplicaSet", Plural: "replicasets", APIGroup: "apps", Namespaced: true, Aliases: []string{"rs"},
		Columns:          []Column{{"NAME", 40, true, false}, {"DESIRED", 10, false, false}, {"CURRENT", 10, false, false}, {"READY", 8, false, false}, {"AGE", 10, false, false}},
		SupportsYAML:     true,
		SupportsLogs:     true,
		SupportsScale:    true,
		SupportsDeletion: true,
	},
	{Kind: "Job", Plural: "jobs", APIGroup: "batch", Namespaced: true, Aliases: []string{"jo"},
		Columns:          []Column{{"NAME", 40, true, false}, {"COMPLETIONS", 14, false, false}, {"DURATION", 12, false, false}, {"AGE", 10, false, false}},
		SupportsYAML:     true,
		SupportsLogs:     true,
		SupportsDeletion: true,
	},
	{Kind: "CronJob", Plural: "cronjobs", APIGroup: "batch", Namespaced: true, Aliases: []string{"cj"},
		Columns:          []Column{{"NAME", 40, true, false}, {"SCHEDULE", 20, false, false}, {"LAST SCHEDULE", 16, false, false}, {"AGE", 10, false, false}},
		SupportsYAML:     true,
		SupportsDeletion: true,
	},
	{Kind: "Service", Plural: "services", Namespaced: true, Aliases: []string{"svc"},
		Columns:          []Column{{"NAME", 40, true, false}, {"TYPE", 14, false, false}, {"CLUSTER-IP", 18, false, false}, {"PORT(S)", 20, false, false}, {"AGE", 10, false, false}},
		SupportsYAML:     true,
		SupportsTopology: true,
		SupportsDeletion: true,
	},
	{Kind: "Endpoints", Plural: "endpoints", Namespaced: true, Aliases: []string{"ep"},
		Columns: []Column{{"NAME", 40, true, false}, {"ENDPOINTS", 40, false, false}, {"AGE", 10, false, false}},
	},
	{Kind: "Ingress", Plural: "ingresses", APIGroup: "networking.k8s.io", Namespaced: true, Aliases: []string{"ing"},
		Columns:          []Column{{"NAME", 35, true, false}, {"ADDRESSES", 22, false, false}, {"RULES", 40, false, false}, {"AGE", 10, false, false}},
		SupportsYAML:     true,
		SupportsTopology: true,
		SupportsDeletion: true,
	},
	{Kind: "ConfigMap", Plural: "configmaps", Namespaced: true, Aliases: []string{"cm"},
		Columns:          []Column{{"NAME", 40, true, false}, {"DATA", 8, false, false}, {"AGE", 10, false, false}},
		SupportsYAML:     true,
		SupportsDeletion: true,
	},
	{Kind: "Secret", Plural: "secrets", Namespaced: true, Aliases: []string{"sec"},
		Columns:          []Column{{"NAME", 40, true, false}, {"TYPE", 30, false, false}, {"DATA", 8, false, false}, {"AGE", 10, false, false}},
		SupportsYAML:     true,
		SupportsDeletion: true,
	},
	{Kind: "ServiceAccount", Plural: "serviceaccounts", Namespaced: true, Aliases: []string{"sa"},
		Columns:      []Column{{"NAME", 40, true, false}, {"SECRETS", 10, false, false}, {"AGE", 10, false, false}},
		SupportsYAML: true,
	},
	{Kind: "PersistentVolumeClaim", Plural: "persistentvolumeclaims", Namespaced: true, Aliases: []string{"pvc"},
		Columns:          []Column{{"NAME", 40, true, false}, {"STATUS", 12, false, false}, {"VOLUME", 30, false, false}, {"CAPACITY", 12, false, false}, {"AGE", 10, false, false}},
		SupportsYAML:     true,
		SupportsDeletion: true,
	},
	{Kind: "HorizontalPodAutoscaler", Plural: "horizontalpodautoscalers", APIGroup: "autoscaling", Namespaced: true, Aliases: []string{"hpa"},
		Columns: []Column{{"NAME", 40, true, false}, {"REFERENCE", 30, false, false}, {"TARGETS", 20, false, false}, {"MIN", 6, false, false}, {"MAX", 6, false, false}, {"AGE", 10, false, false}},
	},
	{Kind: "NetworkPolicy", Plural: "networkpolicies", APIGroup: "networking.k8s.io", Namespaced: true, Aliases: []string{"netpol"},
		Columns: []Column{{"NAME", 40, true, false}, {"POD-SELECTOR", 30, false, false}, {"AGE", 10, false, false}},
	},
	{Kind: "Role", Plural: "roles", APIGroup: "rbac.authorization.k8s.io", Namespaced: true, Aliases: []string{"role"},
		Columns: []Column{{"NAME", 40, true, false}, {"AGE", 10, false, false}},
	},
	{Kind: "RoleBinding", Plural: "rolebindings", APIGroup: "rbac.authorization.k8s.io", Namespaced: true, Aliases: []string{"rb"},
		Columns: []Column{{"NAME", 40, true, false}, {"ROLE", 30, false, false}, {"AGE", 10, false, false}},
	},
	// Cluster-scoped
	{Kind: "Node", Plural: "nodes", Namespaced: false, Aliases: []string{"no"},
		Columns:         []Column{{"NAME", 40, true, false}, {"STATUS", 14, false, false}, {"ROLES", 20, false, false}, {"VERSION", 16, false, false}, {"AGE", 10, false, false}},
		SupportsYAML:    true,
		SupportsMetrics: true,
	},
	{Kind: "PersistentVolume", Plural: "persistentvolumes", Namespaced: false, Aliases: []string{"pv"},
		Columns:          []Column{{"NAME", 40, true, false}, {"CAPACITY", 12, false, false}, {"ACCESS MODES", 16, false, false}, {"STATUS", 12, false, false}, {"AGE", 10, false, false}},
		SupportsYAML:     true,
		SupportsDeletion: true,
	},
	{Kind: "Namespace", Plural: "namespaces", Namespaced: false, Aliases: []string{"ns"},
		Columns:          []Column{{"NAME", 40, true, false}, {"STATUS", 14, false, false}, {"AGE", 10, false, false}},
		SupportsYAML:     true,
		SupportsDeletion: true,
	},
	{Kind: "ClusterRole", Plural: "clusterroles", APIGroup: "rbac.authorization.k8s.io", Namespaced: false, Aliases: []string{"cr"},
		Columns: []Column{{"NAME", 40, true, false}, {"AGE", 10, false, false}},
	},
	{Kind: "ClusterRoleBinding", Plural: "clusterrolebindings", APIGroup: "rbac.authorization.k8s.io", Namespaced: false, Aliases: []string{"crb"},
		Columns: []Column{{"NAME", 40, true, false}, {"ROLE", 30, false, false}, {"AGE", 10, false, false}},
	},
	{Kind: "StorageClass", Plural: "storageclasses", APIGroup: "storage.k8s.io", Namespaced: false, Aliases: []string{"sc"},
		Columns: []Column{{"NAME", 40, true, false}, {"PROVISIONER", 30, false, false}, {"AGE", 10, false, false}},
	},
	{Kind: "Event", Plural: "events", Namespaced: true, Aliases: []string{"ev"},
		Columns: []Column{
			{Header: "LAST SEEN", Width: 12},
			{Header: "COUNT", Width: 6},
			{Header: "AGE", Width: 10},
			{Header: "TYPE", Width: 10},
			{Header: "REASON", Width: 20},
			{Header: "OBJECT", Width: 30},
			{Header: "MESSAGE", Width: 40, Flex: true, Scrollable: true},
		},
	},
	{Kind: "HelmRelease", Plural: "helmreleases",
		APIGroup: "helm.toolkit.fluxcd.io", APIVersion: "v2",
		Namespaced: true, Aliases: []string{"hr"},
		Columns:      []Column{{"NAME", 36, true, false}, {"CHART", 24, false, false}, {"VERSION", 12, false, false}, {"READY", 8, false, false}, {"STATUS", 40, false, false}, {"SUSPENDED", 10, false, false}, {"AGE", 10, false, false}},
		SupportsYAML: true,
	},
}

// GVR returns the GroupVersionResource for this descriptor. APIVersion
// defaults to "v1"; APIGroup defaults to "" (core). Resource is the Plural.
// HelmRelease's version is discovered at runtime — its descriptor's value is
// the v2 default; callers that care about the live version go through
// WatcherFactory.HelmReleaseGVR instead.
func (rd ResourceDescriptor) GVR() schema.GroupVersionResource {
	v := rd.APIVersion
	if v == "" {
		v = "v1"
	}
	return schema.GroupVersionResource{
		Group:    rd.APIGroup,
		Version:  v,
		Resource: rd.Plural,
	}
}

// aliasMap maps alias/kind (lowercase) → ResourceDescriptor index.
var aliasMap map[string]int

func init() {
	aliasMap = make(map[string]int, len(Registry)*3)
	for i, r := range Registry {
		aliasMap[strings.ToLower(r.Kind)] = i
		aliasMap[strings.ToLower(r.Plural)] = i
		for _, a := range r.Aliases {
			aliasMap[strings.ToLower(a)] = i
		}
	}
}

// Resolve returns the ResourceDescriptor for a given kind, plural, or alias.
func Resolve(input string) (ResourceDescriptor, bool) {
	i, ok := aliasMap[strings.ToLower(input)]
	if !ok {
		return ResourceDescriptor{}, false
	}
	return Registry[i], true
}

// SetHandlers attaches behavior closures to the descriptor for `kind`. Returns
// false if the kind is not registered. Pass nil for any handler that does not
// apply (e.g. BuildTopology for kinds without SupportsTopology).
//
// This registration pattern lets per-kind glue live in one file (typically
// internal/ui/panels/kinds.go) while keeping the metadata Registry pure data.
func SetHandlers(kind string, list ListRowsFunc, fetch FetchFunc, topology BuildTopologyFunc) bool {
	i, ok := aliasMap[strings.ToLower(kind)]
	if !ok {
		return false
	}
	if list != nil {
		Registry[i].ListRows = list
	}
	if fetch != nil {
		Registry[i].Fetch = fetch
	}
	if topology != nil {
		Registry[i].BuildTopology = topology
	}
	return true
}

// RegisterAction attaches an Action under `op` to the descriptor for `kind`.
// Returns false if the kind is not registered.
//
// Multiple calls for the same (kind, op) pair overwrite. Op names are
// arbitrary strings agreed between the caller (typically the model) and the
// registration site — common ones are "delete", "scale", "suspend", "resume".
func RegisterAction(kind, op string, action Action) bool {
	i, ok := aliasMap[strings.ToLower(kind)]
	if !ok {
		return false
	}
	if Registry[i].Actions == nil {
		Registry[i].Actions = make(map[string]Action)
	}
	Registry[i].Actions[op] = action
	return true
}

// LookupAction returns the action registered for (kind, op), or nil if either
// the kind isn't registered or no action exists for that op.
func LookupAction(kind, op string) Action {
	rd, ok := Resolve(kind)
	if !ok {
		return nil
	}
	return rd.Actions[op]
}

// AgeString converts a creation timestamp to a human-readable age string.
func AgeString(t metav1.Time) string {
	if t.IsZero() {
		return "—"
	}
	d := metav1.Now().Sub(t.Time)
	switch {
	case d.Hours() >= 24*365:
		return fmt.Sprintf("%dy", int(d.Hours()/(24*365)))
	case d.Hours() >= 24:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	case d.Hours() >= 1:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d.Minutes() >= 1:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	default:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
}
