package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/chaitanyak/klens/internal/cluster"
	"github.com/chaitanyak/klens/internal/k8s"
)

// RunLogs handles the "logs" subcommand. Returns exit code.
func RunLogs(args []string) int {
	fs := flag.NewFlagSet("logs", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: klens logs <pod/name | deployment/name> [flags]")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "examples:")
		fmt.Fprintln(os.Stderr, "  klens logs pod/my-pod -n default --tail 50")
		fmt.Fprintln(os.Stderr, "  klens logs deployment/api -n default -f -o json")
		fmt.Fprintln(os.Stderr, "  klens logs pod/my-pod --since 5m -o json")
		fmt.Fprintln(os.Stderr, "  klens logs pod/my-pod --since-time 2026-04-30T10:00:00Z")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "  --tail 0 fetches all logs (no limit)")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "flags:")
		fs.PrintDefaults()
	}

	var (
		namespace  string
		container  string
		tail       int64
		follow     bool
		sinceStr   string
		sinceTime  string
		ctxName    string
		output     string
	)
	fs.StringVar(&namespace, "n", "", "namespace")
	fs.StringVar(&namespace, "namespace", "", "namespace")
	fs.StringVar(&container, "c", "", "container name (default = first container)")
	fs.StringVar(&container, "container", "", "container name")
	fs.Int64Var(&tail, "tail", 100, "number of recent log lines to show (0 = all)")
	fs.BoolVar(&follow, "f", false, "stream logs continuously (tail -f style)")
	fs.BoolVar(&follow, "follow", false, "stream logs continuously")
	fs.StringVar(&sinceStr, "since", "", "show logs newer than duration, e.g. 5m or 1h")
	fs.StringVar(&sinceTime, "since-time", "", "show logs newer than RFC3339 timestamp, e.g. 2026-04-30T10:00:00Z")
	fs.StringVar(&ctxName, "context", "", "kubeconfig context (default = current context)")
	fs.StringVar(&output, "o", "text", "output format: text|json")
	fs.StringVar(&output, "output", "text", "output format: text|json")

	if err := fs.Parse(reorderArgs(args, map[string]bool{"f": true, "follow": true})); err != nil {
		return 1
	}

	rest := fs.Args()
	if len(rest) == 0 {
		fs.Usage()
		return 1
	}

	if sinceStr != "" && sinceTime != "" {
		fmt.Fprintln(os.Stderr, "error: --since and --since-time are mutually exclusive")
		return 1
	}

	target := rest[0]
	parts := strings.SplitN(target, "/", 2)
	if len(parts) != 2 {
		fmt.Fprintf(os.Stderr, "error: target must be pod/<name> or deployment/<name>, got %q\n", target)
		return 1
	}
	kind := strings.ToLower(parts[0])
	name := parts[1]

	if kind != "pod" && kind != "deployment" && kind != "deploy" {
		fmt.Fprintf(os.Stderr, "error: unsupported kind %q; use pod or deployment\n", kind)
		return 1
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

	// Build LogOptions from flags.
	opts := k8s.LogOptions{
		Container: container,
		TailLines: tail,
		Follow:    follow,
	}

	if sinceStr != "" {
		d, err := time.ParseDuration(sinceStr)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: invalid --since %q: %v\n", sinceStr, err)
			return 1
		}
		secs := int64(d.Seconds())
		opts.SinceSeconds = &secs
	}

	if sinceTime != "" {
		t, err := time.Parse(time.RFC3339, sinceTime)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: invalid --since-time %q (expected RFC3339, e.g. 2026-04-30T10:00:00Z): %v\n", sinceTime, err)
			return 1
		}
		mt := metav1.NewTime(t)
		opts.SinceTime = &mt
	}

	// Set up context with signal handling so -f exits cleanly on Ctrl+C.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if follow {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		go func() {
			<-sigCh
			cancel()
		}()
	}

	// Build the line writer based on output format.
	var writeLine k8s.LogLineFunc
	if output == "json" {
		writeLine = func(pod, cont, line string) {
			k8s.PrintLogNDJSON(os.Stdout, pod, cont, line)
		}
	} else {
		if kind == "pod" || kind == "" {
			// Single pod: raw lines, no prefix (matches kubectl behavior).
			writeLine = func(_, _, line string) {
				fmt.Fprintln(os.Stdout, line)
			}
		} else {
			// Multi-pod: prefix each line with [pod/container].
			writeLine = func(pod, cont, line string) {
				fmt.Fprintf(os.Stdout, "[%s/%s] %s\n", pod, cont, line)
			}
		}
	}

	switch kind {
	case "pod":
		if err := k8s.StreamPodLogs(ctx, cs, namespace, name, opts, writeLine); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return 1
		}
	case "deployment", "deploy":
		if err := k8s.StreamDeploymentLogs(ctx, cs, namespace, name, opts, writeLine); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return 1
		}
	}

	return 0
}
