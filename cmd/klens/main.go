package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/chaitanyak/klens/internal/app"
	"k8s.io/klog/v2"
)

func main() {
	// Suppress klog before anything else — applies to both CLI and TUI paths.
	klogFlags := flag.NewFlagSet("klog", flag.ContinueOnError)
	klog.InitFlags(klogFlags)
	_ = klogFlags.Set("logtostderr", "false")
	klog.SetOutput(io.Discard)

	// Dispatch CLI subcommands before the tmux block so batch calls are never
	// wrapped in a tmux session.
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "get":
			os.Exit(RunGet(os.Args[2:]))
		case "logs":
			os.Exit(RunLogs(os.Args[2:]))
		case "setup":
			os.Exit(RunSetup(os.Args[2:]))
		case "--help", "-h", "help":
			fmt.Fprintln(os.Stderr, "usage: klens [subcommand] [flags]")
			fmt.Fprintln(os.Stderr, "")
			fmt.Fprintln(os.Stderr, "subcommands:")
			fmt.Fprintln(os.Stderr, "  get <resource> [name] [flags]  list or describe resources (-o json for LLM use)")
			fmt.Fprintln(os.Stderr, "  logs <pod|deploy/name> [flags] fetch or stream logs (-f for follow mode)")
			fmt.Fprintln(os.Stderr, "  setup [flags]                  install Claude Code slash command skills")
			fmt.Fprintln(os.Stderr, "")
			fmt.Fprintln(os.Stderr, "with no subcommand: launches the interactive TUI")
			fmt.Fprintln(os.Stderr, "")
			fmt.Fprintln(os.Stderr, "TUI flags:")
			fmt.Fprintln(os.Stderr, "  --readonly   run in read-only mode")
			os.Exit(0)
		}
	}

	var readOnly bool
	flag.BoolVar(&readOnly, "readonly", false, "run in read-only mode (overrides config read_only setting)")
	flag.Parse()

	// Auto-wrap in tmux when not already inside a session. syscall.Exec
	// replaces the current process so there is no parent to clean up.
	if os.Getenv("TMUX") == "" {
		if tmuxPath, err := exec.LookPath("tmux"); err == nil {
			if self, err := os.Executable(); err == nil {
				args := append([]string{"tmux", "new-session", "--"}, self)
				args = append(args, os.Args[1:]...)
				_ = syscall.Exec(tmuxPath, args, os.Environ())
				// Only reaches here if Exec fails; fall through to run normally.
			}
		}
	}

	m := app.New(readOnly)
	p := tea.NewProgram(
		m,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
