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
