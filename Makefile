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

# Check if golangci-lint is installed
GOLANGCI_LINT := $(shell command -v golangci-lint 2> /dev/null)
GOPATH := $(shell go env GOPATH)
GOLANGCI_LINT_PATH := $(GOPATH)/bin/golangci-lint

.PHONY: all
all: clean deps fmt build

.PHONY: deps
deps:
	$(GO) mod download

.PHONY: fmt
fmt:
	$(GOFMT) -w .

.PHONY: install-lint
install-lint:
	@if [ ! -f "$(GOLANGCI_LINT_PATH)" ]; then \
		echo "Installing golangci-lint..."; \
		curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(GOPATH)/bin; \
	fi

.PHONY: lint
lint: install-lint
	$(GOLANGCI_LINT_PATH) run

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
	@echo "Cleaning..."
	@rm -rf $(BUILD_DIR)
	@echo "Clean complete."

.PHONY: run
run: build
	./$(BUILD_DIR)/$(BINARY_NAME)

.PHONY: release
release: lint test
	@echo "Running version command to capture the version..."
	$(eval GH_VERSION=$(shell ./bin/cloudsift-linux-amd64 version | awk '{print $$2}'))
	@echo "Captured version: $(GH_VERSION)"
	@echo "Creating Git release $(GH_VERSION)..."
	git tag -a v$(GH_VERSION) -m "Automated Release - $(GH_VERSION)"
	git push origin v$(GH_VERSION)
	goreleaser release
	@echo "Release v$(GH_VERSION) created successfully."

.PHONY: example-report
example-report:
	@echo "Generating sample JSON data..."
	@go run examples/jsongen/main.go
	@echo "Generating HTML report..."
	@go run examples/htmlgen/main.go
	@echo "Report generated at examples/output/sample_report.html"
