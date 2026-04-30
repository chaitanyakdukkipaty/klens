# klens

Kubernetes TUI with k9s-style keyboard navigation and OpenLens-class visual richness — topology trees, metrics sparklines, multi-pod log streaming, a YAML editor with diff preview, and Claude Code slash commands for AI-assisted cluster operations. Pure terminal. No Electron. No WebView.

## Features

- **Resource browser** — 26 Kubernetes resource types with filterable, sortable tables
- **YAML viewer & editor** — syntax-highlighted viewer; vim-style editor with diff preview before applying (`ctrl+s`)
- **Log streaming** — multi-pod fan-in; space-select multiple pods and stream all logs at once; tab mode groups resources into named tabs; JSON lines are Chroma-highlighted with pretty-print toggle (`J`); inline search (`ctrl+f`) with `n`/`N` navigation; pod solo filter (`1`–`9`); live / paused scroll indicator
- **Topology trees** — visual ownerReference traversal: `Ingress → Service → Pod`, `Deployment → ReplicaSet → Pod`
- **Metrics panel** — ASCII sparklines for CPU and memory via metrics-server (degrades gracefully if not installed)
- **Pod attach** — exec into a pod shell (`a`); opens a new tmux window when running inside tmux
- **Scale** — scale Deployments and StatefulSets interactively (`s`)
- **Read-only mode** — `--readonly` flag or `read_only: true` in config locks out all mutating operations
- **AI skills** — Claude Code slash commands for cluster diagnostics and Q&A; installed with `klens setup`
- **Multi-cluster** — lazy clientset per kubeconfig context; namespace preferences persisted per cluster

## Install

```bash
curl -sSL https://raw.githubusercontent.com/chaitanyakdukkipaty/klens/main/install.sh | bash
```

## Uninstall

```bash
curl -sSL https://raw.githubusercontent.com/chaitanyakdukkipaty/klens/main/uninstall.sh | bash
```

To keep your saved preferences (`~/.config/klens`):

```bash
curl -sSL https://raw.githubusercontent.com/chaitanyakdukkipaty/klens/main/uninstall.sh | KEEP_DATA=true bash
```

**Build from source** (requires Go):

```bash
git clone git@github.com:chaitanyakdukkipaty/klens.git
cd klens
go build -o klens ./cmd/klens/
sudo mv klens /usr/local/bin/
```

## Usage

```bash
klens                  # launch TUI
klens --readonly       # launch TUI in read-only mode (blocks delete/scale/edit/attach)
```

Requires a valid `~/.kube/config`. Switches clusters with the cluster picker; `ctrl+r` reconnects.

### CLI subcommands

klens also exposes a read-only CLI for querying your cluster — used by the AI skills and useful in scripts:

```bash
# List resources
klens get pods -n default
klens get deployments -n my-app -o json
klens get events --since 30m --for pod/my-pod
klens get nodes
klens get helmreleases -n flux-system

# Supported resources: pods|po, deployments|deploy, statefulsets|sts,
#                      events|ev, nodes|no, namespaces|ns, contexts|ctx,
#                      pvcs|pvc, helmreleases|hr

# Fetch logs
klens logs pod/my-pod -n default --tail 50
klens logs deployment/api -n default -f
klens logs pod/my-pod --since 5m -o json
```

### tmux

klens works without tmux, but running inside tmux unlocks two extra behaviours:

- **Pod attach in a separate window** — pressing `a` on a pod opens a new tmux window instead of suspending the TUI. Switch back instantly (`ctrl+b p`) without exiting the shell.
- **Session persistence** — if your SSH connection drops or you close the terminal, the klens session keeps running. Reattach with `tmux attach`.

klens auto-wraps itself in a new tmux session on launch if tmux is installed and you are not already inside one.

### AI skills (optional)

klens ships Claude Code slash commands for AI-assisted cluster operations. Install them once:

```bash
klens setup             # install to ~/.claude/commands/ (global, all projects)
klens setup --project   # install to ./.claude/commands/ (this project only)
```

Then use them inside any Claude Code session:

| Skill | Description |
|---|---|
| `/klens [question]` | Ask anything about your cluster — lists resources, explains status, diagnoses issues |
| `/k8s-diagnose [namespace]` | Diagnose pod failures, node pressure, and scheduling issues |
| `/k8s-health [namespace]` | PASS/WARN/FAIL stability report with restart counts and OOM kill data |

No Anthropic API key required. Skills call `klens get` and `klens logs` as a read-only data layer and suggest `kubectl` commands for write operations.

## Keyboard shortcuts

| Key | Action |
|---|---|
| `tab` | cycle focus: nav ↔ content |
| `enter` | focus resource table |
| `↑↓` / `jk` | navigate |
| `/` | filter |
| `y` | view YAML |
| `e` | edit YAML |
| `l` | stream logs (multi-pod with `space`-select) |
| `t` | topology tree |
| `m` | metrics |
| `d` | delete (with confirmation) |
| `a` | attach / exec into pod |
| `s` | scale (Deployments / StatefulSets) |
| `ctrl+r` | reconnect / refresh |
| `esc` | back to table (or peel log viewer state) |
| `q` | quit |

### Log viewer

| Key | Action |
|---|---|
| `↑↓` / `jk` | scroll |
| `g` / `G` | top / bottom (G re-enables live auto-scroll) |
| `/` | filter lines (hides non-matching) |
| `ctrl+f` | inline search with match highlighting |
| `n` / `N` | next / prev search match |
| `1`–`9` | solo pod (single stream) or jump to tab (multi-resource) |
| `0` | show all pods / return to first tab |
| `tab` | cycle tabs in multi-resource mode |
| `J` | toggle JSON pretty-print + syntax highlighting |
| `esc` | peel state one layer at a time, then exit logs |

## Configuration

Preferences are stored at `~/.config/klens/config.json`:

| Field | Description |
|---|---|
| `read_only` | When `true`, blocks all mutating operations (same as `--readonly` flag) |
| `namespaces` | Manually saved namespace lists per cluster context |
| `last_namespace` | Last active namespace per cluster context (auto-updated) |

## Tech stack

| Layer | Library |
|---|---|
| TUI | [charmbracelet/bubbletea](https://github.com/charmbracelet/bubbletea) |
| Styling | [charmbracelet/lipgloss](https://github.com/charmbracelet/lipgloss) |
| Kubernetes | [k8s.io/client-go](https://github.com/kubernetes/client-go) v0.36.0 |
| Syntax highlight | [alecthomas/chroma](https://github.com/alecthomas/chroma) |
| Charts | [guptarohit/asciigraph](https://github.com/guptarohit/asciigraph) |

## License

MIT
