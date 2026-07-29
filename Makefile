BINARY_NAME ?= mysql-mcp-server
GOLANGCI_LINT ?= golangci-lint
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT ?= $(shell git rev-parse --verify --quiet HEAD 2>/dev/null || echo unknown)
DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -X github.com/futuretea/mysql-mcp-server/pkg/core/version.Version=$(VERSION) \
	-X github.com/futuretea/mysql-mcp-server/pkg/core/version.Commit=$(COMMIT) \
	-X github.com/futuretea/mysql-mcp-server/pkg/core/version.Date=$(DATE)



.PHONY: build test lint format tidy ci clean docker coverage

build:
	go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY_NAME) ./cmd/mcp-server

test:
	go test ./...

lint:
	go vet ./...
	test -z "$$(gofmt -l cmd internal pkg)"
	@if command -v "$(GOLANGCI_LINT)" >/dev/null 2>&1; then \
		$(GOLANGCI_LINT) run ./...; \
	else \
		echo "$(GOLANGCI_LINT) not installed, skipping"; \
	fi

format:
	gofmt -w $$(find cmd internal pkg -name '*.go')

tidy:
	go mod tidy

coverage:
	go test ./pkg/... -coverprofile=coverage.out
	go tool cover -func=coverage.out | tail -1

ci: lint test build

DOCKER_TAG ?= dev

docker:
	docker build \
		--build-arg VERSION=$(VERSION) \
		--build-arg GIT_COMMIT=$(COMMIT) \
		--build-arg BUILD_DATE=$(DATE) \
		-t ghcr.io/futuretea/mysql-mcp-server:$(DOCKER_TAG) \
		-t ghcr.io/futuretea/mysql-mcp-server:$(VERSION) .

clean:
	rm -rf bin coverage.out coverage.txt
