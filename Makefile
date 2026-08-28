ifndef VERSION
VERSION := dev
endif
ifndef GOVULNCHECK_VERSION
GOVULNCHECK_VERSION := v1.2.0
endif
ifndef GOSEC_VERSION
GOSEC_VERSION := v2.25.0
endif
ifndef GOSEC_FLAGS
GOSEC_FLAGS := -quiet -exclude=G304,G703
endif
GOSEC := go run github.com/securego/gosec/v2/cmd/gosec@$(GOSEC_VERSION)
ifndef DIST
DIST := dist
endif
ifndef E2E_IMAGE
E2E_IMAGE := pre-e2e
endif
ifndef E2E_TEST
E2E_TEST := npm
endif
LDFLAGS := -ldflags="-s -w -X main.version=$(VERSION)"
BUILD := CGO_ENABLED=0 go build -trimpath $(LDFLAGS)
BINARY := $(DIST)/pre
CASK_PATH := $(DIST)/homebrew/Casks/pre.rb
CASK_VERSION_PATTERN := version "[^"]+"
CASK_SHA_PATTERN := sha256 "[0-9a-f]{64}"
CASK_BINARY_PATTERN := binary "pre-[^"]+", target: "pre"
CASK_QUARANTINE_READ_PATTERN := args: ["-p", "com.apple.quarantine", binary],
CASK_MUST_SUCCEED_PATTERN := must_succeed: false,
CASK_STDERR_PATTERN := print_stderr: false
CASK_QUARANTINE_DELETE_PATTERN := args: ["-d", "com.apple.quarantine",
CASK_QUARANTINE_DELETE_PATTERN += binary] if quarantine.success?
CASK_PLATFORM_COUNT := 4
E2E_ROOT := tests/e2e
E2E_DOCKERFILE := $(E2E_ROOT)/Dockerfile
E2E_RUNNER := $(E2E_ROOT)/package_manager_test.sh
E2E_TEST_SCRIPTS := $(wildcard $(E2E_ROOT)/*_test.sh)
E2E_TEST_SCRIPTS := $(filter-out $(E2E_RUNNER),$(E2E_TEST_SCRIPTS))
E2E_TEST_NAMES := $(basename $(notdir $(E2E_TEST_SCRIPTS)))
E2E_TEST_NAMES := $(patsubst %_test,%,$(E2E_TEST_NAMES))
E2E_TEST_SCRIPT := $(E2E_ROOT)/$(E2E_TEST)_test.sh
HOST_OS := $(shell uname -s)

.PHONY: build clean
.PHONY: e2e fmt fmt-check gosec integration lint lint-agent lint-agent-all lint-all lint-legibility-setup
.PHONY: release release-check release-preview
.PHONY: screenshots script-test secrets security setup snapshot tag
.PHONY: test test-e2e test-e2e-build test-e2e-docker test-e2e-list
.PHONY: test-integration test-race test-scripts
.PHONY: verify-cask-install verify-e2e verify-e2e-test verify-snapshot vuln

build:
	$(BUILD) -o $(BINARY) ./cmd/pre

tag: release

release:
	mise exec -- sh scripts/tag.sh

snapshot:
	goreleaser release --snapshot --clean --skip=sign

release-check:
	goreleaser check

verify-snapshot:
	test -s $(CASK_PATH)
	ruby -c $(CASK_PATH)
	grep -Eq '$(CASK_VERSION_PATTERN)' $(CASK_PATH)
	test "$$(grep -Ec '$(CASK_SHA_PATTERN)' $(CASK_PATH))" -eq $(CASK_PLATFORM_COUNT)
	test "$$(grep -Ec '$(CASK_BINARY_PATTERN)' $(CASK_PATH))" -eq $(CASK_PLATFORM_COUNT)
	grep -Fq '$(CASK_QUARANTINE_READ_PATTERN)' $(CASK_PATH)
	grep -Fq '$(CASK_MUST_SUCCEED_PATTERN)' $(CASK_PATH)
	grep -Fq '$(CASK_STDERR_PATTERN)' $(CASK_PATH)
	grep -Fq '$(CASK_QUARANTINE_DELETE_PATTERN)' $(CASK_PATH)

verify-cask-install: verify-snapshot
	sh tests/scripts/homebrew_cask_test.sh $(DIST)

release-preview:
	$(MAKE) lint
	$(MAKE) test-race
	$(MAKE) test-scripts
	$(MAKE) test-integration
	$(MAKE) test-e2e
	$(MAKE) security
	$(MAKE) release-check
	$(MAKE) snapshot
	$(MAKE) verify-snapshot
ifeq ($(HOST_OS),Darwin)
	$(MAKE) verify-cask-install
endif

clean:
	rm -rf $(DIST)

test:
	go test ./...

test-race:
	go test -race ./...

test-e2e: verify-e2e
	go test -tags e2e ./tests/e2e/

e2e: test-e2e

fmt:
	gofmt -w .

fmt-check:
	@test -z "$$(gofmt -l .)" || (echo "run 'make fmt' to fix formatting"; exit 1)

lint:
	sh scripts/lint.sh

lint-agent:
	sh scripts/lint.sh --agent

lint-all:
	sh scripts/lint.sh --all

lint-agent-all:
	sh scripts/lint.sh --agent --all

lint-legibility-setup:
	sh scripts/lint.sh --setup-only

vuln:
	go run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...

gosec:
	$(GOSEC) $(GOSEC_FLAGS) ./...

security: vuln gosec

test-integration:
	go test -tags integration ./tests/integration/

integration: test-integration

test-scripts:
	sh tests/scripts/install_test.sh
	sh tests/scripts/lint_test.sh
	sh tests/scripts/setup_test.sh
	sh tests/scripts/tag_test.sh

script-test: test-scripts

screenshots:
	go run ./cmd/pre screenshots dist/screenshots

verify-e2e:
	test "$(words $(E2E_TEST_SCRIPTS))" -gt 1
	bash -n $(E2E_RUNNER) $(E2E_TEST_SCRIPTS)
	@for script in $(E2E_RUNNER) $(E2E_TEST_SCRIPTS); do test -x "$$script"; done

verify-e2e-test: verify-e2e
	printf '%s\n' $(E2E_TEST_NAMES) | grep -Fxq "$(E2E_TEST)"
	test -f "$(E2E_TEST_SCRIPT)"

test-e2e-list:
	@printf '%s\n' $(E2E_TEST_NAMES)

test-e2e-build: verify-e2e
	docker build -f $(E2E_DOCKERFILE) -t $(E2E_IMAGE) .

test-e2e-docker: verify-e2e-test test-e2e-build
	docker run --rm -it $(E2E_IMAGE) "$(E2E_TEST)"

secrets:
	@test -n "$$HOMEBREW_TAP_TOKEN"
	@printf '%s' "$$HOMEBREW_TAP_TOKEN" | gh secret set HOMEBREW_TAP_TOKEN

setup:
	mise install
	mise exec -- sh scripts/setup.sh
