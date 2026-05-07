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
}

// ListRowsFunc returns rows for a kind, given a watcher and namespace.
// Implementations type-assert internally to the concrete Kubernetes type.
type ListRowsFunc func(wf *WatcherFactory, namespace string, ctx RowContext) []ResourceRow

// FetchFunc retrieves a live object by name (used by the YAML viewer).
type FetchFunc func(cs *kubernetes.Clientset, name, namespace string) (any, error)

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
	SupportsYAML     bool
	SupportsLogs     bool
	SupportsTopology bool
	SupportsMetrics  bool
	SupportsAttach   bool
	SupportsScale    bool
	SupportsDeletion bool

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
type Column struct {
	Header string
	Width  int
	Flex   bool
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
				{"NAME", 40, true}, {"READY", 6, false}, {"STATUS", 15, false}, {"RESTARTS", 9, false}, {"AGE", 6, false},
				{"CPU", 6, false}, {"%CPU/R", 7, false}, {"%CPU/L", 7, false},
				{"MEM", 7, false}, {"%MEM/R", 7, false}, {"%MEM/L", 7, false},
			},
		SupportsYAML:     true,
		SupportsLogs:     true,
		SupportsMetrics:  true,
		SupportsAttach:   true,
		SupportsDeletion: true,
	},
	{Kind: "Deployment", Plural: "deployments", Namespaced: true, Aliases: []string{"deploy", "dp"},
		Columns:          []Column{{"NAME", 40, true}, {"READY", 10, false}, {"UP-TO-DATE", 12, false}, {"AVAILABLE", 12, false}, {"AGE", 10, false}},
		SupportsYAML:     true,
		SupportsLogs:     true,
		SupportsTopology: true,
		SupportsScale:    true,
		SupportsDeletion: true,
	},
	{Kind: "StatefulSet", Plural: "statefulsets", Namespaced: true, Aliases: []string{"sts"},
		Columns:          []Column{{"NAME", 40, true}, {"READY", 10, false}, {"AGE", 10, false}},
		SupportsYAML:     true,
		SupportsLogs:     true,
		SupportsScale:    true,
		SupportsDeletion: true,
	},
	{Kind: "DaemonSet", Plural: "daemonsets", Namespaced: true, Aliases: []string{"ds"},
		Columns:          []Column{{"NAME", 40, true}, {"DESIRED", 10, false}, {"READY", 8, false}, {"UP-TO-DATE", 12, false}, {"AGE", 10, false}},
		SupportsYAML:     true,
		SupportsLogs:     true,
		SupportsDeletion: true,
	},
	{Kind: "ReplicaSet", Plural: "replicasets", Namespaced: true, Aliases: []string{"rs"},
		Columns:          []Column{{"NAME", 40, true}, {"DESIRED", 10, false}, {"CURRENT", 10, false}, {"READY", 8, false}, {"AGE", 10, false}},
		SupportsYAML:     true,
		SupportsLogs:     true,
		SupportsScale:    true,
		SupportsDeletion: true,
	},
	{Kind: "Job", Plural: "jobs", Namespaced: true, Aliases: []string{"jo"},
		Columns:          []Column{{"NAME", 40, true}, {"COMPLETIONS", 14, false}, {"DURATION", 12, false}, {"AGE", 10, false}},
		SupportsYAML:     true,
		SupportsLogs:     true,
		SupportsDeletion: true,
	},
	{Kind: "CronJob", Plural: "cronjobs", Namespaced: true, Aliases: []string{"cj"},
		Columns:          []Column{{"NAME", 40, true}, {"SCHEDULE", 20, false}, {"LAST SCHEDULE", 16, false}, {"AGE", 10, false}},
		SupportsYAML:     true,
		SupportsDeletion: true,
	},
	{Kind: "Service", Plural: "services", Namespaced: true, Aliases: []string{"svc"},
		Columns:          []Column{{"NAME", 40, true}, {"TYPE", 14, false}, {"CLUSTER-IP", 18, false}, {"PORT(S)", 20, false}, {"AGE", 10, false}},
		SupportsYAML:     true,
		SupportsTopology: true,
		SupportsDeletion: true,
	},
	{Kind: "Endpoints", Plural: "endpoints", Namespaced: true, Aliases: []string{"ep"},
		Columns: []Column{{"NAME", 40, true}, {"ENDPOINTS", 40, false}, {"AGE", 10, false}},
	},
	{Kind: "Ingress", Plural: "ingresses", Namespaced: true, Aliases: []string{"ing"},
		Columns:          []Column{{"NAME", 35, true}, {"ADDRESSES", 22, false}, {"RULES", 40, false}, {"AGE", 10, false}},
		SupportsYAML:     true,
		SupportsTopology: true,
		SupportsDeletion: true,
	},
	{Kind: "ConfigMap", Plural: "configmaps", Namespaced: true, Aliases: []string{"cm"},
		Columns:          []Column{{"NAME", 40, true}, {"DATA", 8, false}, {"AGE", 10, false}},
		SupportsYAML:     true,
		SupportsDeletion: true,
	},
	{Kind: "Secret", Plural: "secrets", Namespaced: true, Aliases: []string{"sec"},
		Columns:          []Column{{"NAME", 40, true}, {"TYPE", 30, false}, {"DATA", 8, false}, {"AGE", 10, false}},
		SupportsYAML:     true,
		SupportsDeletion: true,
	},
	{Kind: "ServiceAccount", Plural: "serviceaccounts", Namespaced: true, Aliases: []string{"sa"},
		Columns:      []Column{{"NAME", 40, true}, {"SECRETS", 10, false}, {"AGE", 10, false}},
		SupportsYAML: true,
	},
	{Kind: "PersistentVolumeClaim", Plural: "persistentvolumeclaims", Namespaced: true, Aliases: []string{"pvc"},
		Columns:          []Column{{"NAME", 40, true}, {"STATUS", 12, false}, {"VOLUME", 30, false}, {"CAPACITY", 12, false}, {"AGE", 10, false}},
		SupportsYAML:     true,
		SupportsDeletion: true,
	},
	{Kind: "HorizontalPodAutoscaler", Plural: "horizontalpodautoscalers", Namespaced: true, Aliases: []string{"hpa"},
		Columns: []Column{{"NAME", 40, true}, {"REFERENCE", 30, false}, {"TARGETS", 20, false}, {"MIN", 6, false}, {"MAX", 6, false}, {"AGE", 10, false}},
	},
	{Kind: "NetworkPolicy", Plural: "networkpolicies", Namespaced: true, Aliases: []string{"netpol"},
		Columns: []Column{{"NAME", 40, true}, {"POD-SELECTOR", 30, false}, {"AGE", 10, false}},
	},
	{Kind: "Role", Plural: "roles", APIGroup: "rbac.authorization.k8s.io", Namespaced: true, Aliases: []string{"role"},
		Columns: []Column{{"NAME", 40, true}, {"AGE", 10, false}},
	},
	{Kind: "RoleBinding", Plural: "rolebindings", APIGroup: "rbac.authorization.k8s.io", Namespaced: true, Aliases: []string{"rb"},
		Columns: []Column{{"NAME", 40, true}, {"ROLE", 30, false}, {"AGE", 10, false}},
	},
	// Cluster-scoped
	{Kind: "Node", Plural: "nodes", Namespaced: false, Aliases: []string{"no"},
		Columns:         []Column{{"NAME", 40, true}, {"STATUS", 14, false}, {"ROLES", 20, false}, {"VERSION", 16, false}, {"AGE", 10, false}},
		SupportsYAML:    true,
		SupportsMetrics: true,
	},
	{Kind: "PersistentVolume", Plural: "persistentvolumes", Namespaced: false, Aliases: []string{"pv"},
		Columns:          []Column{{"NAME", 40, true}, {"CAPACITY", 12, false}, {"ACCESS MODES", 16, false}, {"STATUS", 12, false}, {"AGE", 10, false}},
		SupportsYAML:     true,
		SupportsDeletion: true,
	},
	{Kind: "Namespace", Plural: "namespaces", Namespaced: false, Aliases: []string{"ns"},
		Columns:          []Column{{"NAME", 40, true}, {"STATUS", 14, false}, {"AGE", 10, false}},
		SupportsYAML:     true,
		SupportsDeletion: true,
	},
	{Kind: "ClusterRole", Plural: "clusterroles", APIGroup: "rbac.authorization.k8s.io", Namespaced: false, Aliases: []string{"cr"},
		Columns: []Column{{"NAME", 40, true}, {"AGE", 10, false}},
	},
	{Kind: "ClusterRoleBinding", Plural: "clusterrolebindings", APIGroup: "rbac.authorization.k8s.io", Namespaced: false, Aliases: []string{"crb"},
		Columns: []Column{{"NAME", 40, true}, {"ROLE", 30, false}, {"AGE", 10, false}},
	},
	{Kind: "StorageClass", Plural: "storageclasses", Namespaced: false, Aliases: []string{"sc"},
		Columns: []Column{{"NAME", 40, true}, {"PROVISIONER", 30, false}, {"AGE", 10, false}},
	},
	{Kind: "Event", Plural: "events", Namespaced: true, Aliases: []string{"ev"},
		Columns: []Column{{"LAST SEEN", 12, false}, {"COUNT", 6, false}, {"AGE", 10, false}, {"TYPE", 10, false}, {"REASON", 20, false}, {"OBJECT", 30, false}, {"MESSAGE", 40, true}},
	},
	{Kind: "HelmRelease", Plural: "helmreleases",
		APIGroup: "helm.toolkit.fluxcd.io", APIVersion: "v2",
		Namespaced: true, Aliases: []string{"hr"},
		Columns:      []Column{{"NAME", 36, true}, {"CHART", 24, false}, {"VERSION", 12, false}, {"READY", 8, false}, {"STATUS", 40, false}, {"SUSPENDED", 10, false}, {"AGE", 10, false}},
		SupportsYAML: true,
	},
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
