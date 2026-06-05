package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"

	tea "charm.land/bubbletea/v2"
	"github.com/chaitanyak/klens/internal/app"
	"k8s.io/klog/v2"
)

// version is set at build time via -ldflags="-X main.version=...".
var version = "dev"

const usage = `usage: klens [flags]

klens is an interactive Kubernetes TUI. With no flags it launches the UI.

flags:
  --readonly   run in read-only mode (overrides config read_only setting)
  --version    print version and exit
  --help, -h   show this help and exit
`

func main() {
	// Suppress klog before anything else.
	klogFlags := flag.NewFlagSet("klog", flag.ContinueOnError)
	klog.InitFlags(klogFlags)
	_ = klogFlags.Set("logtostderr", "false")
	klog.SetOutput(io.Discard)

	var (
		readOnly    bool
		showVersion bool
		showHelp    bool
	)
	flag.BoolVar(&readOnly, "readonly", false, "run in read-only mode (overrides config read_only setting)")
	flag.BoolVar(&showVersion, "version", false, "print version and exit")
	flag.BoolVar(&showHelp, "help", false, "show help and exit")
	flag.BoolVar(&showHelp, "h", false, "show help and exit")
	flag.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	flag.Parse()

	if showHelp {
		fmt.Fprint(os.Stdout, usage)
		return
	}
	if showVersion {
		fmt.Println(version)
		return
	}

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
	// Drop no-op mouse events before they cost an Update + full View render.
	// Returning nil from a tea.WithFilter skips both, so event storms drain
	// instantly instead of piling up a backlog of no-op renders that make the
	// TUI feel unresponsive. Two cases:
	//   - wheel events at the boundary they would scroll toward (trackpad
	//     momentum scroll at an edge);
	//   - hover motion (AllMotion mode) whose hover target set is unchanged —
	//     the overwhelming majority of motion events.
	mouseFilter := func(model tea.Model, msg tea.Msg) tea.Msg {
		am, ok := model.(app.Model)
		if !ok {
			return msg
		}
		switch ev := msg.(type) {
		case tea.MouseWheelMsg:
			if am.WheelAtBoundary(ev.Button) {
				return nil
			}
		case tea.MouseMotionMsg:
			if mp := ev.Mouse(); mp.Button == tea.MouseNone && !am.HoverChanged(mp.X, mp.Y) {
				return nil
			}
		}
		return msg
	}
	p := tea.NewProgram(m, tea.WithFilter(mouseFilter))
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
