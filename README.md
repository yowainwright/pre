# pre

Security proxy for package managers. Intercepts `install` commands, checks packages against the [OSV vulnerability database](https://osv.dev), and blocks installs of known vulnerabilities.

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/yowainwright/pre/main/install.sh | sh
```

## Setup

```sh
pre setup
```

Injects shell hooks into `~/.zshrc` or `~/.bashrc` so that `npm install`, `bun add`, `brew install`, etc. automatically go through `pre`. Optionally enables weekly background system scans.

```sh
pre teardown   # remove hooks
```

## How it works

```
bun add lodash
  → reads bun.lock (exact versions, including transitive deps)
  → checks each package against OSV in parallel
  → all clean: proceeds silently
  → new packages scanned: prints one-line summary
  → critical/high CVE found: shows detail box, prompts before installing
```

Reads lockfiles for exact versions before falling back to manifests:

| Manager | Lockfiles checked |
|---------|------------------|
| npm / bun / pnpm | `package-lock.json` → `bun.lock` → `pnpm-lock.yaml` |
| go | `go.sum` |
| pip / uv / poetry | `uv.lock` → `poetry.lock` → `Pipfile.lock` |
| brew | `Brewfile.lock.json` |

Supported managers: `brew`, `npm`, `pnpm`, `bun`, `go`, `pip`, `pip3`, `uv`, `poetry`

## Commands

```sh
pre setup                    # inject shell hooks, optionally enable system scan
pre teardown                 # remove shell hooks
pre status                   # show managers, cache size, last system scan
pre config                   # show current config
pre config set <key> <value> # update a config value
pre scan system              # run a full system scan now (checks all cached packages)
```

## Configuration

Config file: `~/.config/pre/config.json` — edit directly or use `pre config set`.

```json
{
  "api": {
    "endpoint": "https://api.osv.dev/v1/query"
  },
  "cache": {
    "ttl": "24h"
  },
  "systemScan": true,
  "systemTTL": "168h",
  "managers": [
    {
      "name": "composer",
      "ecosystem": "Packagist",
      "installCmds": ["install", "require"]
    }
  ]
}
```

| Field | Default | Description |
|-------|---------|-------------|
| `api.endpoint` | `https://api.osv.dev/v1/query` | OSV-compatible API endpoint |
| `cache.ttl` | `24h` | How long a clean result is cached per package version |
| `systemScan` | `false` | Enable weekly background scan of all cached packages |
| `systemTTL` | `168h` | How often the system scan runs (default: weekly) |
| `managers` | — | Add or override managers by name |

`PRE_CACHE_TTL` env var overrides `cache.ttl` (e.g. `PRE_CACHE_TTL=0s bun add react`).

The `managers` array merges with built-ins. An entry matching a built-in `name` replaces it; a new name extends the list.

## Development

```sh
make test        # unit tests
make e2e         # end-to-end tests (requires npm)
make integration # integration tests (requires network)
make lint        # format check + go vet
make demo        # run in Docker
```
