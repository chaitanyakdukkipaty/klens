# Release Process

## Versioning

klens follows [Semantic Versioning](https://semver.org/):

| Change type | Example | When to use |
|---|---|---|
| Patch `vX.Y.Z+1` | `v0.2.1` | Bug fixes, doc corrections, script fixes |
| Minor `vX.Y+1.0` | `v0.3.0` | New features, new keyboard shortcuts, new resource types |
| Major `vX+1.0.0` | `v1.0.0` | Breaking changes, major rewrites |

## How a Release Works

Pushing a git tag triggers the GitHub Actions workflow at `.github/workflows/release.yml`. It:

1. Cross-compiles the binary for 4 platforms (darwin/amd64, darwin/arm64, linux/amd64, linux/arm64)
2. Tars each binary: `klens-<version>-<os>-<arch>.tar.gz`
3. Generates `checksums.txt` (SHA-256)
4. Creates a GitHub Release with all artifacts and auto-generated release notes

No manual build steps. No goreleaser. Just `git tag` + `git push`.

## How to Cut a Release

### Step 1 — Make sure main is clean and pushed

```bash
git status          # must be clean
git push            # must be up to date with origin
```

### Step 2 — Create and push the tag

```bash
git tag -a v0.X.Y -m "<one-line summary of what changed>"
git push origin v0.X.Y
```

The tag message becomes the release title on GitHub.

### Step 3 — Verify

Watch the Actions run at `https://github.com/chaitanyakdukkipaty/klens/actions`.

A successful run produces:
```
dist/klens-v0.X.Y-darwin-amd64.tar.gz
dist/klens-v0.X.Y-darwin-arm64.tar.gz
dist/klens-v0.X.Y-linux-amd64.tar.gz
dist/klens-v0.X.Y-linux-arm64.tar.gz
dist/checksums.txt
```

Check the release at `https://github.com/chaitanyakdukkipaty/klens/releases`.

## Fixing a Failed Release

If the workflow fails after the tag was pushed:

```bash
# Delete the tag locally and remotely
git tag -d v0.X.Y
git push origin :refs/tags/v0.X.Y

# Fix the issue, commit, push
git add ...
git commit -m "fix: ..."
git push

# Re-tag on the fixed commit
git tag -a v0.X.Y -m "<summary>"
git push origin v0.X.Y
```

## Release History

| Version | Date | Summary |
|---|---|---|
| v1.0.5 | 2026-06-24 | Terminal dock scrollback + select-to-copy: wheel up / `shift+pgup` freezes the view and scrolls history (`↑↓/jk`, `g/G`, `esc` resumes); left-drag selects text and release copies to the clipboard (`y`/`c` in scroll mode); a `▲ pos/total` indicator shows in the tab bar while frozen; wheel forwards to alt-screen apps (vim/less/htop). Dock chrome (maximize/minimise, switch tab, hide) now works in scrollback mode too |
| v1.0.4 | 2026-06-08 | Mouse support in the namespace & cluster pickers: wheel scrolls, hover highlights, click selects/switches (click-outside dismisses). Explicit viewport scroll state (decoupled from the cursor) so hovering a row no longer snaps the list to the top; fixed a separator/tag line-wrap that misaligned click hit-testing; long names (e.g. EKS ARNs) are middle-truncated so each row stays one line and the region + cluster name remain distinguishable |
| v1.0.2 | 2026-06-02 | Interactive resource-table sorting: click a column header to sort (click again toggles asc/desc); `shift+→`/`shift+←` cycle the sort column, `shift+↑`/`shift+↓` set ascending/descending (`>`/`<` retained as aliases); per-column `k8s.Column.SortType` (`SortString`/`SortTime`/`SortNumber`/`SortCapacity`) compared via new `k8s.CellCompare`; header shows a `▲`/`▼` arrow and the title a `· sort COL` label. Shift (not ctrl) because macOS reserves ctrl+arrows for Spaces / Mission Control |
| v1.0.1 | 2026-05-29 | Events view: `ctrl+z` faults-only filter (Type ∈ {Warning, Error}), `w` MESSAGE-column wrap, `o` jump to `InvolvedObject`; events-specific help footer; explicit "not supported on Event" feedback for `ctrl+d`/`e`/`a`; backed by new optional `FaultRowMarker` / `InvolvedObjectResolver` Kind interfaces |
| v1.0.0 | 2026-05-28 | Per-kind architecture: every resource lives in one file under `internal/k8s/kinds/` implementing `Kind` + capability interfaces (`Deleter`, `Scaler`, `Logger`, `Applier`, `Suspender`, …); action dispatch (`kinds.DeleteCmd`/`ScaleCmd`/`ApplyCmd`/`SuspendCmd`) replaces the legacy `Actions` map and `Supports*` booleans; row rendering uses per-column `Render` closures; GVR-keyed `InformerRegistry`; `Lister` seam (`CachedLister` / `FakeLister`) lets tests run against a fake clientset |
| v0.8.4 | 2026-05-27 | Generalize horizontal scroll (`k8s.Column.Scrollable`); resource table vertical scrollbar + `i/N · P%` cursor position label; help bar fills width and handles compound keys (`ctrl+c/q`, `ctrl+r`) |
| v0.6.0 | 2026-05-07 | In-app drag-to-copy in log viewer (works in tabs and split layouts); mouse capture toggle (`M`); async per-kind informer sync with 100ms event coalescing; faster pod metrics refresh; JSON flat as default in log viewer |
| v0.5.0 | 2026-05-07 | Migrate to Bubble Tea v2 (charm.land); right-click context menu; full-screen toggle (`F`); inline help panel; expanded scrollbar/log viewer/model refactor |
| v0.4.0 | 2026-05-04 | Scrollbars in log viewer and YAML editor; mouse wheel scrolling; ctrl+v paste in all inputs; unicode input fix |
| v0.3.1 | 2026-05-04 | Fix q key quit inside filter/search/edit modes; ctrl+z YAML rollback; async Helm GVR; terminal size guard |
| v0.3.0 | 2026-04-30 | Log viewer: search, tab mode, JSON colorization, pod solo filter, grouped streaming |
| v0.2.2 | 2026-04-30 | Fix filter cleared on informer refresh when filter is uncommitted |
| v0.2.1 | 2026-04-30 | Fix `KEEP_DATA` not respected in uninstall script |
| v0.2.0 | 2026-04-30 | Improve filter UX: `esc` clears filters, `enter` commits |
| v0.1.0 | 2026-04-30 | Initial release |

## Install / Uninstall (for reference)

```bash
# Install latest
curl -sSL https://raw.githubusercontent.com/chaitanyakdukkipaty/klens/main/install.sh | bash

# Uninstall (removes binary + config)
curl -sSL https://raw.githubusercontent.com/chaitanyakdukkipaty/klens/main/uninstall.sh | bash

# Uninstall but keep config
curl -sSL https://raw.githubusercontent.com/chaitanyakdukkipaty/klens/main/uninstall.sh | KEEP_DATA=true bash
```

The install script always fetches the latest GitHub Release — no manual version pinning needed.
