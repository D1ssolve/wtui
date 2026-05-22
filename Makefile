# wtui Makefile

BINARY   := wtui
BIN_DIR  := bin
CMD_PATH := ./cmd/wtui

VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -ldflags "-X main.Version=$(VERSION)"

export CGO_ENABLED=0

.PHONY: build test lint install clean test-integration release-snapshot

build:
	@mkdir -p $(BIN_DIR)
	go build $(LDFLAGS) -o $(BIN_DIR)/$(BINARY) $(CMD_PATH)

test:
	go test ./...

lint:
	go vet ./...

install: build
	go install $(LDFLAGS) $(CMD_PATH)

clean:
	rm -rf $(BIN_DIR)

test-integration:
	go test -tags integration ./...

release-snapshot: ## Build local GoReleaser snapshot artifacts without publishing
	goreleaser release --snapshot --clean --skip=publish