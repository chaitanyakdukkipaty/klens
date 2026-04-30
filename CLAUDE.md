# klens — CLAUDE.md

Lightweight Kubernetes TUI combining k9s keyboard navigation with OpenLens-class GUI features. Pure terminal — no Electron, no WebView.

## Module

```
github.com/chaitanyak/klens
```

## Tech Stack

| Layer | Library |
|---|---|
| TUI framework | `charmbracelet/bubbletea` (Elm architecture) |
| Styling | `charmbracelet/lipgloss` |
| Widgets | `charmbracelet/bubbles` (viewport, textarea, spinner, etc.) |
| Kubernetes client | `k8s.io/client-go` v0.36.0 |
| Syntax highlight | `alecthomas/chroma/v2` |
| Charts | `guptarohit/asciigraph` |
| Diff | `pmezard/go-difflib` |

## Build & Run

```bash
go build ./...                        # check compilation
go run ./cmd/klens/               # run TUI (requires kubeconfig)
go run ./cmd/klens/ --readonly    # run TUI in read-only mode
go run ./cmd/klens/ get pods      # CLI: list pods
go run ./cmd/klens/ logs pod/name # CLI: fetch logs
go run ./cmd/klens/ setup         # CLI: install Claude Code skills
go test ./...                         # run tests
```

## Architecture

```
cmd/klens/
  main.go           → dispatches CLI subcommands (get/logs/setup/help) before TUI launch;
                      auto-wraps in tmux when not already in a session;
                      tea.NewProgram(app.New(readOnly), WithAltScreen, WithMouseCellMotion)
  get_cmd.go        → `klens get` subcommand; queries pods/deployments/events/nodes/etc.
  logs_cmd.go       → `klens logs` subcommand; streams pod or deployment logs (NDJSON or text)
  setup_cmd.go      → `klens setup` subcommand; copies embedded skill .md files to ~/.claude/commands/
  util.go           → shared CLI helpers (reorderArgs)

internal/app/model.go         → root Bubbletea model; routes all msgs; delegates to child panels
internal/cluster/manager.go   → multi-cluster kubeconfig; lazy clientsets per context
internal/k8s/
  informers.go                → SharedIndexInformer factory; sends ResourceUpdatedMsg to app
  resources.go                → ResourceDescriptor registry (26 types + aliases)
  query.go                    → read-only query functions used by CLI subcommands (QueryPods, QueryEvents, etc.)
  output.go                   → table-printing and JSON helpers for CLI output (PrintPodsTable, MarshalPretty, etc.)
  logs.go                     → multi-pod log fan-in via goroutine channels; LogGroup type; Start/StartGrouped; LogLine carries Group field for tab routing
  metrics.go                  → metrics-server REST polling; MetricsUpdatedMsg
  topology.go                 → ownerReference traversal; TreeNode builder
  operations.go               → delete/scale/rollout/drain/cordon
  portforward.go              → client-go SPDY port-forward (no subprocess)
  exec.go                     → remotecommand SPDY exec (pod attach)
internal/config/config.go     → persisted user preferences (namespace lists, last active namespace per cluster,
                                read_only flag); stored at ~/.config/klens/config.json
internal/skills/
  skills.go                   → embed.FS that packages commands/*.md into the binary
  commands/                   → Claude Code slash command skill files (installed by `klens setup`)
internal/ui/
  layout/layout.go            → panel sizing from terminal dimensions
  panels/                     → header, status_bar, nav_panel, resource_table, yaml_viewer, yaml_editor,
                                log_viewer, topology_panel, metrics_panel
  widgets/                    → sparkline, diff_viewer, tree_renderer, confirm_dialog, scale_dialog,
                                namespace_picker, cluster_picker
  styles/styles.go            → Lipgloss style definitions (Kubernetes blue theme)
```

## Key Conventions

- **Bubbletea message flow**: Informers run in background goroutines; they send to `msgCh chan tea.Msg`; `WatchCmd` relays these to the Bubbletea loop. Never mutate model state outside `Update()`.
- **Lazy clientsets**: `cluster.Manager` creates a `*kubernetes.Clientset` on first use per context. On cluster switch, stop old `WatcherFactory` with `wf.Stop()` before creating a new one.
- **Content modes**: `app.Model.mode` (ModeTable, ModeYAML, ModeEditor, ModeLogs, ModeTopology, ModeMetrics) controls which panel `contentView()` renders.
- **Focus vs action keys**: `app.Model.focus` (FocusNav / FocusContent) controls which panel `↑↓/jk` navigate. Action keys (`y`, `e`, `l`, `t`, `m`, `d`, `a`, `s`) work regardless of focus — they always operate on the selected table row.
- **Read-only mode**: `--readonly` CLI flag or `read_only: true` in config.json blocks all mutating operations (delete, scale, edit-apply, attach). The flag overrides the config but never forces it off.
- **Metrics degradation**: if metrics-server not installed (404 on metrics API), show "n/a" — never block resource browsing.
- **lipgloss constraint**: `MarginLeft()` breaks `Width()` in lipgloss v1.1.0 — use `PaddingLeft()` for all indented panel elements.
- **YAML editor modal states**: `internal/ui/panels/yaml_editor.go` implements vim-style Normal/Insert/DiffConfirm/Applying states. In Normal mode both `hjkl` and arrow keys navigate; `i/a/A/o/O` enter Insert mode; `ctrl+s` opens diff preview. In Insert mode all input goes directly to the `textarea` widget.
- **Log viewer states**: `internal/ui/panels/log_viewer.go` has layered state — filterOn (/ input), searchOn (ctrl+f input), podFilter (1-9 solo), tabGroups (multi-group). `HasActiveState()` and `HandleEsc()` let the root model peel one layer per `esc` instead of exiting log mode immediately. `LogGroup` / `StartGrouped()` in `logs.go` carry the group name through `LogLine.Group` so the viewer can route lines to tabs.
- **JSON colorization**: `tryColorizeJSON` in `log_viewer.go` Chroma-highlights lines that are valid JSON (dracula theme, terminal256 formatter); colorCache is a parallel slice to `lines` so re-colorizing on `J` toggle only re-renders lines, not restreams data.
- **CLI subcommand dispatch**: `main.go` checks `os.Args[1]` before the tmux auto-wrap block so `klens get`/`logs`/`setup` are never wrapped in a tmux session.
- **klog suppression**: klog is silenced at startup via `klog.SetOutput(io.Discard)` — suppress before any client-go initialization to avoid noisy stderr.

## Keyboard Shortcuts

### Global

| Key | Action |
|---|---|
| `tab` | cycle focus nav ↔ content |
| `enter` | move focus to resource table |
| `↑↓` / `jk` | navigate |
| `/` | filter |
| `y` | view YAML |
| `e` | edit YAML |
| `l` | logs (or multi-pod logs with space-selected rows) |
| `t` | topology |
| `m` | metrics |
| `d` | delete (with confirmation) |
| `a` | attach / exec into pod (tmux: new window; non-tmux: suspend TUI) |
| `s` | scale (Deployments / StatefulSets) |
| `ctrl+r` | reconnect / refresh (stops watcher, reruns full connect) |
| `:` | command palette (TODO) |
| `esc` | back to table (or peel log viewer state) |
| `q` | quit |

### Log viewer

| Key | Action |
|---|---|
| `↑↓` / `jk` | scroll |
| `g` / `G` | top / bottom (G also re-enables auto-scroll) |
| `/` | filter lines (hides non-matching) |
| `ctrl+f` | inline search (highlights matches) |
| `n` / `N` | next / prev search match |
| `1`–`9` | solo pod (single-group) or jump to tab (multi-group) |
| `0` | show all pods / return to first tab |
| `tab` | cycle tabs (multi-group mode) |
| `J` | toggle JSON pretty-print + Chroma colorization |
| `esc` | peel state: cancel input → clear search → clear pod filter → clear filter → exit logs |

## Claude Code Skills

AI-assisted cluster operations are provided as Claude Code slash commands. They are embedded in the klens binary and installed with `klens setup`.

```bash
klens setup             # install skills to ~/.claude/commands/ (global)
klens setup --project   # install to ./.claude/commands/ (this project only)
klens setup --force     # overwrite existing skill files
```

| Skill | File | Purpose |
|---|---|---|
| `/k8s-diagnose [namespace]` | `commands/k8s-diagnose.md` | Diagnose pod failures, node pressure, scheduling issues; severity-grouped output with kubectl suggestions |
| `/k8s-health [namespace] [window]` | `commands/k8s-health.md` | PASS/WARN/FAIL stability report with uptime %, restart count, OOM kills, Warning event density |
| `/klens [anything]` | `commands/klens.md` | General-purpose assistant: describes all `klens get` and `klens logs` operations; the user can ask anything about their cluster |

No Anthropic SDK, `ANTHROPIC_API_KEY`, or in-app AI state machine required. Skills run entirely in the Claude Code agent loop; klens is the data layer only.

## Adding a New Resource Type

1. Add a `ResourceDescriptor` entry to `internal/k8s/resources.go` `Registry` slice
2. Add a `List*` method to `internal/k8s/informers.go` `WatcherFactory`
3. Add query / print functions to `internal/k8s/query.go` and `internal/k8s/output.go` (for CLI use)
4. Add a `Build*Rows` function to `internal/ui/panels/resource_table.go`
5. Add a `case "Kind":` to `app.Model.listRows()` in `internal/app/model.go`
6. Add a `FetchObject` case in `internal/ui/panels/yaml_viewer.go` `fetchObject()`
7. Wire the resource into `cmd/klens/get_cmd.go` if it should be accessible via `klens get`

## Adding Topology to a Resource

1. Set `SupportsTopology: true` in the `ResourceDescriptor` (drives the `t` hint in the status bar)
2. Add `Build*Topology(obj, wf *WatcherFactory) *TreeNode` to `internal/k8s/topology.go`
   - Ingress: `Ingress → Rule (host) → Route (path → svc:port) → Service → Pod`
   - Service: `Service → Pod` (via selector matching)
   - Deployment: `Deployment → ReplicaSet → Pod` (via OwnerReferences)
3. Add a `case "Kind":` to `app.Model.buildTopology()` in `internal/app/model.go`
