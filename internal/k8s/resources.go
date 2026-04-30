package k8s

import (
	"fmt"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ResourceDescriptor describes a Kubernetes resource type.
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
}

// Column defines a table column for a resource type.
type Column struct {
	Header string
	Width  int
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
				{"NAME", 40}, {"READY", 6}, {"STATUS", 15}, {"RESTARTS", 9}, {"AGE", 6},
				{"CPU", 6}, {"%CPU/R", 7}, {"%CPU/L", 7},
				{"MEM", 7}, {"%MEM/R", 7}, {"%MEM/L", 7},
			},
		SupportsYAML:     true,
		SupportsLogs:     true,
		SupportsMetrics:  true,
		SupportsAttach:   true,
		SupportsDeletion: true,
	},
	{Kind: "Deployment", Plural: "deployments", Namespaced: true, Aliases: []string{"deploy", "dp"},
		Columns:          []Column{{"NAME", 40}, {"READY", 10}, {"UP-TO-DATE", 12}, {"AVAILABLE", 12}, {"AGE", 10}},
		SupportsYAML:     true,
		SupportsLogs:     true,
		SupportsTopology: true,
		SupportsScale:    true,
		SupportsDeletion: true,
	},
	{Kind: "StatefulSet", Plural: "statefulsets", Namespaced: true, Aliases: []string{"sts"},
		Columns:          []Column{{"NAME", 40}, {"READY", 10}, {"AGE", 10}},
		SupportsYAML:     true,
		SupportsLogs:     true,
		SupportsScale:    true,
		SupportsDeletion: true,
	},
	{Kind: "DaemonSet", Plural: "daemonsets", Namespaced: true, Aliases: []string{"ds"},
		Columns:          []Column{{"NAME", 40}, {"DESIRED", 10}, {"READY", 8}, {"UP-TO-DATE", 12}, {"AGE", 10}},
		SupportsYAML:     true,
		SupportsLogs:     true,
		SupportsDeletion: true,
	},
	{Kind: "ReplicaSet", Plural: "replicasets", Namespaced: true, Aliases: []string{"rs"},
		Columns:          []Column{{"NAME", 40}, {"DESIRED", 10}, {"CURRENT", 10}, {"READY", 8}, {"AGE", 10}},
		SupportsYAML:     true,
		SupportsLogs:     true,
		SupportsScale:    true,
		SupportsDeletion: true,
	},
	{Kind: "Job", Plural: "jobs", Namespaced: true, Aliases: []string{"jo"},
		Columns:          []Column{{"NAME", 40}, {"COMPLETIONS", 14}, {"DURATION", 12}, {"AGE", 10}},
		SupportsYAML:     true,
		SupportsLogs:     true,
		SupportsDeletion: true,
	},
	{Kind: "CronJob", Plural: "cronjobs", Namespaced: true, Aliases: []string{"cj"},
		Columns:          []Column{{"NAME", 40}, {"SCHEDULE", 20}, {"LAST SCHEDULE", 16}, {"AGE", 10}},
		SupportsYAML:     true,
		SupportsDeletion: true,
	},
	{Kind: "Service", Plural: "services", Namespaced: true, Aliases: []string{"svc"},
		Columns:          []Column{{"NAME", 40}, {"TYPE", 14}, {"CLUSTER-IP", 18}, {"PORT(S)", 20}, {"AGE", 10}},
		SupportsYAML:     true,
		SupportsTopology: true,
		SupportsDeletion: true,
	},
	{Kind: "Endpoints", Plural: "endpoints", Namespaced: true, Aliases: []string{"ep"},
		Columns: []Column{{"NAME", 40}, {"ENDPOINTS", 40}, {"AGE", 10}},
	},
	{Kind: "Ingress", Plural: "ingresses", Namespaced: true, Aliases: []string{"ing"},
		Columns:          []Column{{"NAME", 35}, {"ADDRESSES", 22}, {"RULES", 40}, {"AGE", 10}},
		SupportsYAML:     true,
		SupportsTopology: true,
		SupportsDeletion: true,
	},
	{Kind: "ConfigMap", Plural: "configmaps", Namespaced: true, Aliases: []string{"cm"},
		Columns:          []Column{{"NAME", 40}, {"DATA", 8}, {"AGE", 10}},
		SupportsYAML:     true,
		SupportsDeletion: true,
	},
	{Kind: "Secret", Plural: "secrets", Namespaced: true, Aliases: []string{"sec"},
		Columns:          []Column{{"NAME", 40}, {"TYPE", 30}, {"DATA", 8}, {"AGE", 10}},
		SupportsYAML:     true,
		SupportsDeletion: true,
	},
	{Kind: "ServiceAccount", Plural: "serviceaccounts", Namespaced: true, Aliases: []string{"sa"},
		Columns:      []Column{{"NAME", 40}, {"SECRETS", 10}, {"AGE", 10}},
		SupportsYAML: true,
	},
	{Kind: "PersistentVolumeClaim", Plural: "persistentvolumeclaims", Namespaced: true, Aliases: []string{"pvc"},
		Columns:          []Column{{"NAME", 40}, {"STATUS", 12}, {"VOLUME", 30}, {"CAPACITY", 12}, {"AGE", 10}},
		SupportsYAML:     true,
		SupportsDeletion: true,
	},
	{Kind: "HorizontalPodAutoscaler", Plural: "horizontalpodautoscalers", Namespaced: true, Aliases: []string{"hpa"},
		Columns: []Column{{"NAME", 40}, {"REFERENCE", 30}, {"TARGETS", 20}, {"MIN", 6}, {"MAX", 6}, {"AGE", 10}},
	},
	{Kind: "NetworkPolicy", Plural: "networkpolicies", Namespaced: true, Aliases: []string{"netpol"},
		Columns: []Column{{"NAME", 40}, {"POD-SELECTOR", 30}, {"AGE", 10}},
	},
	{Kind: "Role", Plural: "roles", APIGroup: "rbac.authorization.k8s.io", Namespaced: true, Aliases: []string{"role"},
		Columns: []Column{{"NAME", 40}, {"AGE", 10}},
	},
	{Kind: "RoleBinding", Plural: "rolebindings", APIGroup: "rbac.authorization.k8s.io", Namespaced: true, Aliases: []string{"rb"},
		Columns: []Column{{"NAME", 40}, {"ROLE", 30}, {"AGE", 10}},
	},
	// Cluster-scoped
	{Kind: "Node", Plural: "nodes", Namespaced: false, Aliases: []string{"no"},
		Columns:         []Column{{"NAME", 40}, {"STATUS", 14}, {"ROLES", 20}, {"VERSION", 16}, {"AGE", 10}},
		SupportsYAML:    true,
		SupportsMetrics: true,
	},
	{Kind: "PersistentVolume", Plural: "persistentvolumes", Namespaced: false, Aliases: []string{"pv"},
		Columns:          []Column{{"NAME", 40}, {"CAPACITY", 12}, {"ACCESS MODES", 16}, {"STATUS", 12}, {"AGE", 10}},
		SupportsYAML:     true,
		SupportsDeletion: true,
	},
	{Kind: "Namespace", Plural: "namespaces", Namespaced: false, Aliases: []string{"ns"},
		Columns:          []Column{{"NAME", 40}, {"STATUS", 14}, {"AGE", 10}},
		SupportsYAML:     true,
		SupportsDeletion: true,
	},
	{Kind: "ClusterRole", Plural: "clusterroles", APIGroup: "rbac.authorization.k8s.io", Namespaced: false, Aliases: []string{"cr"},
		Columns: []Column{{"NAME", 40}, {"AGE", 10}},
	},
	{Kind: "ClusterRoleBinding", Plural: "clusterrolebindings", APIGroup: "rbac.authorization.k8s.io", Namespaced: false, Aliases: []string{"crb"},
		Columns: []Column{{"NAME", 40}, {"ROLE", 30}, {"AGE", 10}},
	},
	{Kind: "StorageClass", Plural: "storageclasses", Namespaced: false, Aliases: []string{"sc"},
		Columns: []Column{{"NAME", 40}, {"PROVISIONER", 30}, {"AGE", 10}},
	},
	{Kind: "Event", Plural: "events", Namespaced: true, Aliases: []string{"ev"},
		Columns: []Column{{"LAST SEEN", 12}, {"COUNT", 6}, {"AGE", 10}, {"TYPE", 10}, {"REASON", 20}, {"OBJECT", 30}, {"MESSAGE", 40}},
	},
	{Kind: "HelmRelease", Plural: "helmreleases",
		APIGroup: "helm.toolkit.fluxcd.io", APIVersion: "v2",
		Namespaced: true, Aliases: []string{"hr"},
		Columns:      []Column{{"NAME", 36}, {"CHART", 24}, {"VERSION", 12}, {"READY", 8}, {"STATUS", 40}, {"SUSPENDED", 10}, {"AGE", 10}},
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
