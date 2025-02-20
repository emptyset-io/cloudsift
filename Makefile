# Makefile for building CloudSift CLI tool

# Project Variables
BINARY_NAME=cloudsift
VERSION=0.1.0
BUILD_DIR=bin
GO_VERSION=1.24.0
GOFILES=$(shell find . -name '*.go' | grep -v vendor)
LDFLAGS=-X "main.version=$(VERSION)"

# Detect OS
OS := $(shell uname -s | tr '[:upper:]' '[:lower:]')
ARCH := $(shell uname -m)

# Install Go using g (Go Version Manager)
install-go:
	@echo "Checking for 'g' package manager..."
	@if ! command -v g &> /dev/null; then \
		echo "'g' is not installed. Installing now..."; \
		curl -sSL https://git.io/g-install | bash; \
		exec $$SHELL; \
	fi
	@echo "Installing Go $(GO_VERSION)..."
	@g install $(GO_VERSION)
	@g use $(GO_VERSION)
	@g default $(GO_VERSION)
	@echo "Go $(GO_VERSION) installed and set as default."


# Install dependencies
deps:
	@echo "Downloading dependencies..."
	@go mod tidy
	@go mod vendor

# Format code
fmt:
	@echo "Formatting code..."
	@go fmt ./...

# Run static analysis (linting)
lint:
	@echo "Running lint checks..."
	@if ! command -v golangci-lint &> /dev/null; then \
		echo "Installing golangci-lint..."; \
		if [ "$(shell uname)" = "Darwin" ]; then \
			brew install golangci-lint; \
		else \
			go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest; \
		fi \
	fi
	@golangci-lint run

# Run tests
test:
	@echo "Running tests..."
	@go test -v ./...

# Build binary
build:
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p $(BUILD_DIR)
	@go build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME) main.go
	@echo "Build complete: $(BUILD_DIR)/$(BINARY_NAME)"

# Build cross-platform binaries
build-all:
	@echo "Building for all platforms..."
	@mkdir -p $(BUILD_DIR)
	@GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64 main.go
	@GOOS=darwin GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-arm64 main.go
	@GOOS=darwin GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-amd64 main.go
	@GOOS=windows GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME)-windows-amd64.exe main.go
	@echo "Cross-platform build complete."

# Clean build artifacts
clean:
	@echo "Cleaning build artifacts..."
	@rm -rf $(BUILD_DIR)

# Run the application
run:
	@go run main.go

# Release version
release: build-all
	@echo "Packaging release..."
	@tar -czvf $(BUILD_DIR)/$(BINARY_NAME)-$(VERSION)-$(OS)-$(ARCH).tar.gz -C $(BUILD_DIR) $(BINARY_NAME)-*
	@echo "Release package created: $(BUILD_DIR)/$(BINARY_NAME)-$(VERSION)-$(OS)-$(ARCH).tar.gz"

# Default target
.PHONY: install-go deps fmt lint test build build-all clean run release
