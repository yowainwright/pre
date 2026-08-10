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
4. Run `make test-scripts` if touching installer, setup, or release scripts
5. Run `make test-e2e` if touching intercept or setup logic (requires npm)
6. Run `make security` before release-sensitive changes (requires network)
7. Open a PR

## Testing

```sh
make test             # unit tests
make test-race        # unit tests with the race detector
make test-e2e         # end-to-end tests (requires npm)
make test-integration # live API calls (requires network)
make test-scripts     # shell script tests
make lint             # gofmt check + go vet
make gosec            # static security checks (requires Go 1.26+)
make vuln             # govulncheck (requires network)
make security         # govulncheck + gosec
make screenshots      # TUI SVG screenshots for PRs
make test-e2e-list    # list Docker E2E tests
make test-e2e-docker E2E_TEST=npm # run one Docker E2E test
make release-preview  # full release validation without publishing
make release          # prompt for a version, validate, tag, push, and watch CI
```

Run `mise install` once to install the pinned `svu` version. `make release` also requires GoReleaser, a clean synchronized `main` checkout, and an authenticated GitHub CLI. It creates an annotated tag; GitHub Actions publishes the release.

## Code Style

- No comments unless logic is non-obvious
- No external runtime dependencies without a clear security and maintenance justification
- All new behavior covered by tests
- Keep `cmd/pre/main.go` focused on command dispatch
- Name ecosystem-specific files `<capability>_<ecosystem>.go`
- Match non-trivial source files with `<source>_test.go`
- Keep unit tests beside source; reserve `tests/` for integration, end-to-end, and shell tests
- Do not add ownerless `helpers.go`, `utils.go`, or `misc.go` files
