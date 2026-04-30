package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"

	"github.com/chaitanyak/klens/internal/cluster"
	"github.com/chaitanyak/klens/internal/k8s"
)

// RunGet handles the "get" subcommand. Returns exit code.
func RunGet(args []string) int {
	fs := flag.NewFlagSet("get", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: klens get <resource> [name] [flags]")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "resources: pods|po, deployments|deploy, events|ev, namespaces|ns, contexts|ctx,")
		fmt.Fprintln(os.Stderr, "           nodes|no, statefulsets|sts, pvcs|pvc, helmreleases|hr")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "flags:")
		fs.PrintDefaults()
	}

	var (
		namespace string
		output    string
		selector  string
		ctxName   string
		sinceStr  string
		forObj    string
	)
	fs.StringVar(&namespace, "n", "", "namespace (empty = all namespaces)")
	fs.StringVar(&namespace, "namespace", "", "namespace (empty = all namespaces)")
	fs.StringVar(&output, "o", "table", "output format: table|json")
	fs.StringVar(&output, "output", "table", "output format: table|json")
	fs.StringVar(&selector, "l", "", "label selector (e.g. app=nginx)")
	fs.StringVar(&selector, "selector", "", "label selector")
	fs.StringVar(&ctxName, "context", "", "kubeconfig context (default = current context)")
	fs.StringVar(&sinceStr, "since", "", "show events newer than duration, e.g. 30m or 1h (events only)")
	fs.StringVar(&forObj, "for", "", "filter events for object, e.g. pod/my-pod (events only)")

	if err := fs.Parse(reorderArgs(args, nil)); err != nil {
		return 1
	}

	rest := fs.Args()
	if len(rest) == 0 {
		fs.Usage()
		return 1
	}

	resource := rest[0]
	name := ""
	if len(rest) > 1 {
		name = rest[1]
	}

	// contexts reads kubeconfig only — no cluster connection needed
	if resource == "contexts" || resource == "ctx" {
		return runGetContexts(output)
	}

	mgr, err := cluster.New()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: load kubeconfig: %v\n", err)
		return 1
	}

	activeCtx := mgr.ActiveContext()
	if ctxName != "" {
		activeCtx = ctxName
	}

	cs, err := mgr.ClientsetFor(activeCtx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	ctx := context.Background()

	switch resource {
	case "namespaces", "namespace", "ns":
		return runGetNamespaces(ctx, cs, output)

	case "pods", "pod", "po":
		if name != "" {
			return runDescribePod(ctx, cs, namespace, name)
		}
		return runGetPods(ctx, cs, namespace, selector, output)

	case "deployments", "deployment", "deploy":
		if name != "" {
			return runDescribeDeployment(ctx, cs, namespace, name)
		}
		return runGetDeployments(ctx, cs, namespace, selector, output)

	case "events", "event", "ev":
		var sinceDur time.Duration
		if sinceStr != "" {
			sinceDur, err = time.ParseDuration(sinceStr)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: invalid --since %q: %v\n", sinceStr, err)
				return 1
			}
		}
		fieldSel := buildEventFieldSelector(forObj)
		return runGetEvents(ctx, cs, namespace, fieldSel, sinceDur, output)

	case "nodes", "node", "no":
		return runGetNodes(ctx, cs, output)

	case "statefulsets", "statefulset", "sts":
		return runGetStatefulSets(ctx, cs, namespace, selector, output)

	case "pvcs", "pvc", "persistentvolumeclaims", "persistentvolumeclaim":
		return runGetPVCs(ctx, cs, namespace, output)

	case "helmreleases", "helmrelease", "hr":
		restCfg, err := mgr.RestConfigFor(activeCtx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return 1
		}
		dc, err := dynamic.NewForConfig(restCfg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: dynamic client: %v\n", err)
			return 1
		}
		gvr := k8s.DiscoverHelmReleaseGVR(restCfg)
		return runGetHelmReleases(ctx, dc, gvr, namespace, output)

	default:
		fmt.Fprintf(os.Stderr, "error: unknown resource %q\n", resource)
		fmt.Fprintln(os.Stderr, "known: pods|po, deployments|deploy, events|ev, namespaces|ns, contexts|ctx,")
		fmt.Fprintln(os.Stderr, "       nodes|no, statefulsets|sts, pvcs|pvc, helmreleases|hr")
		return 1
	}
}

func runGetContexts(output string) int {
	mgr, err := cluster.New()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	contexts := k8s.QueryContexts(mgr)
	active := mgr.ActiveContext()

	if output == "json" {
		type ctxEntry struct {
			Name    string `json:"name"`
			Current bool   `json:"current"`
		}
		entries := make([]ctxEntry, len(contexts))
		for i, c := range contexts {
			entries[i] = ctxEntry{Name: c, Current: c == active}
		}
		b, _ := k8s.MarshalPretty(entries)
		fmt.Println(string(b))
		return 0
	}

	fmt.Println("CONTEXT")
	for _, c := range contexts {
		if c == active {
			fmt.Printf("* %s\n", c)
		} else {
			fmt.Printf("  %s\n", c)
		}
	}
	return 0
}

func runGetNamespaces(ctx context.Context, cs *kubernetes.Clientset, output string) int {
	namespaces, err := k8s.QueryNamespaces(ctx, cs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	if output == "json" {
		for i := range namespaces {
			k8s.StripMeta(&namespaces[i])
		}
		b, _ := k8s.MarshalPretty(namespaces)
		fmt.Println(string(b))
		return 0
	}
	fmt.Fprintf(os.Stdout, "%-40s %s\n", "NAME", "AGE")
	for _, ns := range namespaces {
		fmt.Fprintf(os.Stdout, "%-40s %s\n", ns.Name, k8s.FmtAge(ns.CreationTimestamp.Time))
	}
	return 0
}

func runGetPods(ctx context.Context, cs *kubernetes.Clientset, namespace, selector, output string) int {
	pods, err := k8s.QueryPods(ctx, cs, namespace, selector)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	if output == "json" {
		for i := range pods {
			k8s.StripMeta(&pods[i])
		}
		b, _ := k8s.MarshalPretty(pods)
		fmt.Println(string(b))
		return 0
	}
	k8s.PrintPodsTable(os.Stdout, pods)
	return 0
}

func runDescribePod(ctx context.Context, cs *kubernetes.Clientset, namespace, name string) int {
	pod, err := cs.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	k8s.StripMeta(pod)
	b, _ := k8s.MarshalPretty(pod)
	fmt.Println(string(b))
	return 0
}

func runGetDeployments(ctx context.Context, cs *kubernetes.Clientset, namespace, selector, output string) int {
	deps, err := k8s.QueryDeployments(ctx, cs, namespace, selector)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	if output == "json" {
		for i := range deps {
			k8s.StripMeta(&deps[i])
		}
		b, _ := k8s.MarshalPretty(deps)
		fmt.Println(string(b))
		return 0
	}
	k8s.PrintDeploymentsTable(os.Stdout, deps)
	return 0
}

func runDescribeDeployment(ctx context.Context, cs *kubernetes.Clientset, namespace, name string) int {
	dep, err := cs.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	k8s.StripMeta(dep)
	b, _ := k8s.MarshalPretty(dep)
	fmt.Println(string(b))
	return 0
}

func runGetEvents(ctx context.Context, cs *kubernetes.Clientset, namespace, fieldSel string, since time.Duration, output string) int {
	events, err := k8s.QueryEvents(ctx, cs, namespace, fieldSel, since)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	if output == "json" {
		for i := range events {
			k8s.StripMeta(&events[i])
		}
		b, _ := k8s.MarshalPretty(events)
		fmt.Println(string(b))
		return 0
	}
	k8s.PrintEventsTable(os.Stdout, events)
	return 0
}

func runGetNodes(ctx context.Context, cs *kubernetes.Clientset, output string) int {
	nodes, err := k8s.QueryNodes(ctx, cs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	if output == "json" {
		for i := range nodes {
			k8s.StripMeta(&nodes[i])
		}
		b, _ := k8s.MarshalPretty(nodes)
		fmt.Println(string(b))
		return 0
	}
	k8s.PrintNodesTable(os.Stdout, nodes)
	return 0
}

func runGetStatefulSets(ctx context.Context, cs *kubernetes.Clientset, namespace, selector, output string) int {
	sets, err := k8s.QueryStatefulSets(ctx, cs, namespace, selector)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	if output == "json" {
		for i := range sets {
			k8s.StripMeta(&sets[i])
		}
		b, _ := k8s.MarshalPretty(sets)
		fmt.Println(string(b))
		return 0
	}
	k8s.PrintStatefulSetsTable(os.Stdout, sets)
	return 0
}

func runGetPVCs(ctx context.Context, cs *kubernetes.Clientset, namespace, output string) int {
	pvcs, err := k8s.QueryPVCs(ctx, cs, namespace)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	if output == "json" {
		for i := range pvcs {
			k8s.StripMeta(&pvcs[i])
		}
		b, _ := k8s.MarshalPretty(pvcs)
		fmt.Println(string(b))
		return 0
	}
	k8s.PrintPVCsTable(os.Stdout, pvcs)
	return 0
}

func runGetHelmReleases(ctx context.Context, dc dynamic.Interface, gvr schema.GroupVersionResource, namespace, output string) int {
	items, err := k8s.QueryHelmReleases(ctx, dc, gvr, namespace)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	if output == "json" {
		for i := range items {
			k8s.StripMeta(&items[i])
		}
		b, _ := k8s.MarshalPretty(items)
		fmt.Println(string(b))
		return 0
	}
	// Table output for HelmReleases
	fmt.Fprintf(os.Stdout, "%-50s %-20s %-10s %s\n", "NAME", "NAMESPACE", "READY", "AGE")
	for _, u := range items {
		status := helmReleaseStatus(u.Object)
		fmt.Fprintf(os.Stdout, "%-50s %-20s %-10s %s\n",
			u.GetName(), u.GetNamespace(), status,
			k8s.FmtAge(u.GetCreationTimestamp().Time),
		)
	}
	return 0
}

func helmReleaseStatus(obj map[string]interface{}) string {
	conditions, ok := nestedSlice(obj, "status", "conditions")
	if !ok {
		return "Unknown"
	}
	for _, c := range conditions {
		cm, ok := c.(map[string]interface{})
		if !ok {
			continue
		}
		if cm["type"] == "Ready" {
			if cm["status"] == "True" {
				return "Ready"
			}
			if reason, ok := cm["reason"].(string); ok && reason != "" {
				return reason
			}
			return "NotReady"
		}
	}
	return "Unknown"
}

func nestedSlice(obj map[string]interface{}, keys ...string) ([]interface{}, bool) {
	cur := obj
	for i, k := range keys {
		if i == len(keys)-1 {
			v, ok := cur[k]
			if !ok {
				return nil, false
			}
			s, ok := v.([]interface{})
			return s, ok
		}
		next, ok := cur[k].(map[string]interface{})
		if !ok {
			return nil, false
		}
		cur = next
	}
	return nil, false
}

// buildEventFieldSelector converts "pod/my-pod" to a Kubernetes field selector.
func buildEventFieldSelector(forObj string) string {
	if forObj == "" {
		return ""
	}
	parts := strings.SplitN(forObj, "/", 2)
	if len(parts) != 2 {
		return ""
	}
	kind := strings.ToUpper(parts[0][:1]) + strings.ToLower(parts[0][1:])
	return fmt.Sprintf("involvedObject.kind=%s,involvedObject.name=%s", kind, parts[1])
}
