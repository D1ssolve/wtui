# wtui Makefile

BINARY      := wtui
BIN_DIR     := bin
CMD_PATH    := ./cmd/wtui
INSTALL_DIR := /Users/diss0x/go/bin

VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -ldflags "-X main.Version=$(VERSION)"

export CGO_ENABLED=0

.PHONY: build test lint install clean \
        build-linux-amd64 build-linux-arm64 \
        build-darwin-amd64 build-darwin-arm64 \
        test-integration

build:
	@mkdir -p $(BIN_DIR)
	go build $(LDFLAGS) -o $(BIN_DIR)/$(BINARY) $(CMD_PATH)

test:
	go test ./...

lint:
	go vet ./...

install: build
	cp $(BIN_DIR)/$(BINARY) $(INSTALL_DIR)/$(BINARY)

clean:
	rm -rf $(BIN_DIR)

test-integration:
	go test -tags integration ./...

build-linux-amd64: ## Build for Linux/amd64
	@mkdir -p $(BIN_DIR)
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o $(BIN_DIR)/$(BINARY)-linux-amd64 $(CMD_PATH)

build-linux-arm64: ## Build for Linux/arm64
	@mkdir -p $(BIN_DIR)
	GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o $(BIN_DIR)/$(BINARY)-linux-arm64 $(CMD_PATH)

build-darwin-amd64: ## Build for macOS/amd64
	@mkdir -p $(BIN_DIR)
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o $(BIN_DIR)/$(BINARY)-darwin-amd64 $(CMD_PATH)

build-darwin-arm64: ## Build for macOS/arm64 (Apple Silicon)
	@mkdir -p $(BIN_DIR)
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o $(BIN_DIR)/$(BINARY)-darwin-arm64 $(CMD_PATH)
