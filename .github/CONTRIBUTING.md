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
4. Run `make e2e` if touching intercept or setup logic (requires npm)
5. Open a PR

## Testing

```sh
make test       # unit tests
make e2e        # end-to-end tests (requires npm)
make lint       # go vet
```

## Code Style

- No comments unless logic is non-obvious
- No external dependencies — stdlib only
- All new behavior covered by tests
