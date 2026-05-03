VERSION ?= dev
GOVULNCHECK_VERSION ?= v1.2.0
GOSEC_VERSION ?= v2.25.0
GOSEC_FLAGS ?= -quiet -exclude=G304,G703
LDFLAGS = -ldflags="-s -w -X main.version=$(VERSION)"
BUILD = CGO_ENABLED=0 go build -trimpath $(LDFLAGS)
DIST = dist

build:
	$(BUILD) -o $(DIST)/pre ./cmd/pre

tag:
	sh scripts/tag.sh

release:
	goreleaser release --clean

snapshot:
	goreleaser release --snapshot --clean --skip=sign

clean:
	rm -rf $(DIST)

test:
	go test ./...

e2e:
	go test -tags e2e ./tests/e2e/

fmt:
	gofmt -w .

fmt-check:
	@test -z "$$(gofmt -l .)" || (echo "run 'make fmt' to fix formatting"; exit 1)

lint: fmt-check
	go vet ./...

vuln:
	go run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...

gosec:
	go run github.com/securego/gosec/v2/cmd/gosec@$(GOSEC_VERSION) $(GOSEC_FLAGS) ./...

security: vuln gosec

integration:
	go test -tags integration ./tests/integration/

script-test:
	sh tests/scripts/install_test.sh
	sh tests/scripts/setup_test.sh
	sh tests/scripts/tag_test.sh

screenshots:
	go run ./cmd/pre screenshots dist/screenshots

docker-build:
	docker build -f opts/Dockerfile -t pre-demo .

demo: docker-build
	docker run -it pre-demo

secrets:
	gh secret set HOMEBREW_TAP_TOKEN --body "$$HOMEBREW_TAP_TOKEN"
	gh secret set CODECOV_TOKEN      --body "$$CODECOV_TOKEN"

setup:
	sh scripts/setup.sh
