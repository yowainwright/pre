# Contributing

## Setup

```sh
git clone https://github.com/yowainwright/pre.git
cd pre
go build ./...
```

## Workflow

1. Branch off `main`
2. Make changes
3. Run `make test` (unit) and `make lint`
4. Run `make script-test` if touching installer, setup, or release scripts
5. Run `make e2e` if touching intercept or setup logic (requires npm)
6. Run `make security` before release-sensitive changes (requires network)
7. Open a PR

## Testing

```sh
make test        # unit tests
make e2e         # end-to-end tests (requires npm)
make integration # live API calls (requires network)
make script-test # shell script tests
make lint        # gofmt check + go vet
make gosec       # static security checks (requires Go 1.26+)
make vuln        # govulncheck (requires network)
make security    # govulncheck + gosec
make screenshots # TUI SVG screenshots for PRs
```

## Code Style

- No comments unless logic is non-obvious
- No external runtime dependencies without a clear security and maintenance justification
- All new behavior covered by tests

## Project layout

```mermaid
graph TD
    CMD["cmd/pre\nentry point"] --> PROXY

    subgraph PROXY["internal/proxy"]
        I["intercept.go\ncore loop"]
        SC["scan.go\nbackground scans"]
        ST["setup.go\nshell hooks"]
        SS["stats.go\nscan scheduling"]
        R["render.go\nterminal output"]
    end

    subgraph MGR["internal/manager"]
        REG["registry.go\nbuilt-in managers"]
        LF["lockfile.go\nlockfile readers"]
        MF["manifest.go\nmanifest readers"]
        PA["parse.go\nspec parsing"]
        VR["version.go\nversion resolution"]
    end

    subgraph SEC["internal/security"]
        OSV["osv.go\nOSV API client"]
        CV["cvss.go\nseverity scoring"]
    end

    CACHE["internal/cache\n~/.cache/pre/cache.json"]
    CONFIG["internal/config\n~/.config/pre/config.json"]
    DISPLAY["internal/display\nterminal helpers"]

    I --> MGR
    I --> SEC
    I --> CACHE
    I --> DISPLAY
    CMD --> CONFIG
```
