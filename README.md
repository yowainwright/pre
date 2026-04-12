# pre

Security proxy for package managers. Intercepts `install` commands, checks packages against the [OSV vulnerability database](https://osv.dev), and lets you decide before anything is installed.

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/yowainwright/pre/main/install.sh | sh
```

## Setup

Run once to inject shell hooks:

```sh
pre setup
```

This adds function wrappers to your `~/.zshrc` or `~/.bashrc` so that `npm install`, `brew install`, etc. automatically go through `pre`.

## How it works

```
npm install react
  → pre npm install react
  → checks react@latest against OSV
  → clean: proceeds to real npm install
  → vulns found: shows report, prompts to continue
```

Supported managers: `brew`, `npm`, `pnpm`, `bun`, `go`, `pip`, `pip3`, `uv`, `poetry`

## Configuration

Config file: `~/.config/pre/config.json`

```json
{
  "api": {
    "endpoint": "https://api.osv.dev/v1/query"
  },
  "cache": {
    "ttl": "24h"
  },
  "managers": [
    {
      "name": "yarn",
      "ecosystem": "npm",
      "installCmds": ["add"]
    }
  ]
}
```

| Field | Default | Description |
|-------|---------|-------------|
| `api.endpoint` | `https://api.osv.dev/v1/query` | Vulnerability API endpoint (must accept OSV request format) |
| `cache.ttl` | `24h` | How long a clean result is cached per package version. Set to `0s` to always check. |
| `managers` | — | Add new managers or override built-in ones by name |

The `managers` array merges with built-ins. An entry with the same `name` as a built-in replaces it; a new name extends the list.

### Environment variables

`PRE_CACHE_TTL` overrides `cache.ttl` from the config file (e.g. `PRE_CACHE_TTL=0s npm install react`).

## Development

```sh
make test        # unit tests
make e2e         # end-to-end tests (requires npm)
make integration # integration tests (requires network)
make lint        # format check + go vet
make demo        # run in Docker
```
