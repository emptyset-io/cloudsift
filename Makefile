GO ?= go
GOFMT ?= gofmt
BUILD_DIR := bin
BINARY_NAME := cloudsift

# Build information
VERSION := $(shell git describe --tags --always --dirty)
COMMIT := $(shell git rev-parse HEAD)
BUILD_TIME := $(shell date -u '+%Y-%m-%d %H:%M:%S')
GO_VERSION := $(shell $(GO) version | cut -d ' ' -f 3)

# Build flags
LDFLAGS := -X 'cloudsift/internal/version.GitCommit=$(COMMIT)' \
           -X 'cloudsift/internal/version.BuildTime=$(BUILD_TIME)' \
           -X 'cloudsift/internal/version.GoVersion=$(GO_VERSION)'

.PHONY: all
all: clean deps fmt build

.PHONY: deps
deps:
	$(GO) mod download

.PHONY: fmt
fmt:
	$(GOFMT) -w .

.PHONY: lint
lint:
	golangci-lint run

.PHONY: test
test:
	$(GO) test -v ./...

.PHONY: build
build:
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p $(BUILD_DIR)
	@$(GO) build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME)
	@echo "Build complete: $(BUILD_DIR)/$(BINARY_NAME)"

.PHONY: build-all
build-all:
	@echo "Building for all platforms..."
	@mkdir -p $(BUILD_DIR)
	@GOOS=linux GOARCH=amd64 $(GO) build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64
	@GOOS=darwin GOARCH=arm64 $(GO) build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-arm64
	@GOOS=darwin GOARCH=amd64 $(GO) build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-amd64
	@GOOS=windows GOARCH=amd64 $(GO) build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME)-windows-amd64.exe
	@echo "Cross-platform build complete."

.PHONY: clean
clean:
	rm -rf $(BUILD_DIR)

.PHONY: run
run: build
	./$(BUILD_DIR)/$(BINARY_NAME)

.PHONY: release
release: lint test build-all
