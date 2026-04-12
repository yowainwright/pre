VERSION ?= dev
LDFLAGS = -ldflags="-s -w -X main.version=$(VERSION)"
DIST = dist

build:
	go build $(LDFLAGS) -o $(DIST)/pre ./cmd/pre

release: clean
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o $(DIST)/pre-darwin-arm64 ./cmd/pre
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o $(DIST)/pre-darwin-amd64 ./cmd/pre
	GOOS=linux  GOARCH=amd64 go build $(LDFLAGS) -o $(DIST)/pre-linux-amd64  ./cmd/pre
	@echo "SHA256 checksums:"
	@shasum -a 256 $(DIST)/pre-darwin-arm64 $(DIST)/pre-darwin-amd64

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

integration:
	go test -tags integration ./tests/integration/

script-test:
	sh tests/scripts/install_test.sh

docker-build:
	docker build -f opts/Dockerfile -t pre-demo .

demo: docker-build
	docker run -it pre-demo
