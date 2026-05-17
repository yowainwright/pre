# pre≋≈~∿

Security proxy for package managers. Sits between your shell and `npm`, `pip`, `brew`, and friends — checks packages against the [OSV vulnerability database](https://osv.dev) before anything installs.

[![CI](https://github.com/yowainwright/pre/actions/workflows/test.yml/badge.svg)](https://github.com/yowainwright/pre/actions/workflows/test.yml)
[![Release](https://img.shields.io/github/v/release/yowainwright/pre)](https://github.com/yowainwright/pre/releases)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

Zero config. Zero dependencies. One binary. macOS · Linux · zsh · bash

## Why

Supply chain attacks don't announce themselves. By the time a malicious or vulnerable package surfaces, it may already be in a lockfile, a CI image, or production. `pre` intercepts every install — before it happens — and checks it against [OSV.dev](https://osv.dev), the same open database behind GitHub's dependency alerts.

No dashboard. No CI step to add. It just runs when you do.

## Install

```sh
# Homebrew
brew install yowainwright/tap/pre

# or curl (macOS + Linux)
curl -fsSL https://raw.githubusercontent.com/yowainwright/pre/main/install.sh | sh
```

Every release ships with SHA256 checksums and a cosign signature. The install script verifies the checksum automatically; cosign verification runs if `cosign` is on your PATH.

## Setup

```sh
pre setup    # adds shell hooks to ~/.zshrc or ~/.bashrc
pre teardown # removes them
pre status   # shows install state, cache, managers, and scan status
```

After setup, every `npm install`, `pip install`, `brew install`, etc. goes through `pre` automatically — no extra commands needed.

## What you'll see

| Situation | Output |
|-----------|--------|
| Everything cached and clean | Silent — install proceeds |
| New packages, no issues | `scanning 12 packages... all clean` |
| Low/medium CVE | Warning printed, install proceeds |
| High/critical CVE | CVE detail box shown, Y/N prompt |

## Package Manager

```sh
pre manage
# or
pre m
```

The manager opens a full-screen, keyboard-driven terminal UI for installed packages from available managers. It supports themed rows, arrow or `j`/`k` navigation, live `/` search with no enter-to-apply step, `m` manager toggles, `enter`/`o` action dialogs, `x`/`esc` dialog close, and `q` or `ctrl+c` exit. The default theme uses Catppuccin Mocha truecolor values; set `PRE_MANAGE_THEME=contrast` for a brighter theme or `PRE_MANAGE_THEME=mono` for no color. Package actions run back through `pre <manager> ...`, so install and downgrade flows still use the vulnerability scan before the package manager runs.

Non-interactive package commands are available too:

```sh
pre installed                    # package inventory
pre manage --package react --manager npm --upgrade
pre manage --package react --manager npm --downgrade 18.2.0
pre manage --package ripgrep --uninstall
pre install npm react
pre update npm react
pre downgrade pip urllib3 1.24.1
pre uninstall brew ripgrep
```

## Demo

```sh
make demo
```

Requires Docker. Builds a container with `pre` installed and shell hooks active, then plays through real scans across npm and pip — clean installs, CVE detection, and blocked installs. Colors render fully via the TTY allocated by `docker run -it`.

## Lockfile-first scanning

`pre` reads lockfiles for exact pinned versions (including transitive deps) before falling back to manifests:

| Manager | Lockfiles |
|---------|-----------|
| npm / bun / pnpm | `package-lock.json` → `bun.lock` → `pnpm-lock.yaml` |
| go | `go.sum` |
| pip / uv / poetry | `uv.lock` → `poetry.lock` → `Pipfile.lock` |
| brew | `Brewfile.lock.json` |

Supported managers: `brew`, `npm`, `pnpm`, `bun`, `go`, `pip`, `pip3`, `uv`, `poetry`

## Commands

```sh
pre setup                     # inject shell hooks
pre teardown                  # remove shell hooks
pre status                    # pre install state, managers, cache size, last system scan
pre manage                    # package manager TUI
pre m                         # short alias for pre manage
pre installed                 # package inventory
pre manage --package <pkg> --manager <mgr> --upgrade [version]
pre manage --package <pkg> --manager <mgr> --downgrade <version>
pre manage --package <pkg> --manager <mgr> --uninstall
pre install <mgr> <pkg>       # install a package through pre
pre update <mgr> [pkg]        # update a package, or all where supported
pre downgrade <mgr> <pkg> <v> # install an older package version
pre uninstall <mgr> <pkg>     # remove a package
pre config                    # show current config
pre config set <key> <value>  # update a config value
pre scan system               # scan all cached packages now
pre self update               # update the pre binary
pre self uninstall [--purge]  # remove pre itself
```

## Configuration

`~/.config/pre/config.json` — edit directly or use `pre config set`.

| Key | Default | What it does |
|-----|---------|--------------|
| `api.endpoint` | `https://api.osv.dev/v1/query` | OSV-compatible API to query |
| `cache.ttl` | `24h` | How long a clean result is trusted |
| `systemScan` | `false` | Weekly background scan of cached packages |
| `systemTTL` | `168h` | How often the background scan runs |
| `managers` | — | Add or override managers |

**Quick examples:**

```sh
pre config set cache.ttl 12h
pre config set systemScan true
PRE_CACHE_TTL=0s npm install   # bypass cache for one install
```

**Custom manager** (add to `managers` array in config):

```json
{
  "name": "composer",
  "ecosystem": "Packagist",
  "installCmds": ["install", "require"]
}
```

Entries matching a built-in `name` replace it; new names extend the list.

## Security model

- Queries [OSV.dev](https://osv.dev) — Google-operated, free, open
- Only package name + version leave your machine — no code uploaded
- Lockfile-first ensures transitive deps are checked, not just top-level
- Binaries signed with cosign (sigstore keyless) on every release
- SHA256 checksums for all platforms

## FAQ

**Does this slow down my installs?**
Rarely. Results are cached for 24h by default, so repeat installs of the same packages add ~0ms. First-time scans hit the OSV API in parallel — typically under a second for most lockfiles.

**Does it work offline?**
The cache covers most cases. If the OSV API is unreachable and there's no cached result, `pre` warns and proceeds — it won't block your install.

**Is Windows supported?**
Not yet. `pre` supports macOS and Linux with zsh or bash. Windows/fish support is not planned, but PRs are welcome.

**Can I use a private or self-hosted vulnerability database?**
Yes — set `api.endpoint` to any OSV-compatible API endpoint.

**How do I skip `pre` for one install?**
Use `command npm install` (or `command pip install`, etc.) — the `command` builtin bypasses the shell function hook and calls the real binary directly.

## Update pre

```sh
pre self update
```

Homebrew installs run `brew upgrade pre`. Curl/manual installs rerun the checksum-verifying installer into the current binary directory.

## Uninstall pre

```sh
pre self uninstall
pre self uninstall --purge # also removes config/cache data
```

Homebrew installs run `brew uninstall pre`. Manual installs remove the current `pre` binary after removing shell hooks.

## Development

```sh
make setup       # install deps, verify secrets, install git hooks
make test        # unit tests
make e2e         # end-to-end (requires npm)
make integration # live API calls (requires network)
make lint        # format check + vet
make gosec       # static security checks (requires Go 1.26+)
make vuln        # govulncheck scan (requires network)
make security    # govulncheck + gosec
make screenshots # generate TUI SVG screenshots in dist/screenshots
make snapshot    # local release dry-run (all 4 binaries, no publish)
make demo        # run in Docker
```

## License

MIT
