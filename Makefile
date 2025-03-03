GO ?= go
GOFMT ?= gofmt
BUILD_DIR := bin
BINARY_NAME := cloudsift

# Build information
VERSION := $(shell git describe --tags 2>/dev/null || echo "v0.0.0")
COMMIT := $(shell git rev-parse HEAD)
BUILD_TIME := $(shell date -u '+%Y-%m-%d %H:%M:%S')
GO_VERSION := $(shell $(GO) version | cut -d ' ' -f 3)

# Release type validation
RELEASE_TYPE ?= patch
VALID_RELEASE_TYPES := major minor patch
RELEASE_TYPE_VALID := $(filter $(RELEASE_TYPE),$(VALID_RELEASE_TYPES))

# Build flags
LDFLAGS := -X 'cloudsift/internal/version.Version=$(VERSION)' \
           -X 'cloudsift/internal/version.GitCommit=$(COMMIT)' \
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
	$(GO) mod tidy

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
build: deps
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

.PHONY: validate-release-type
validate-release-type:
	@if [ -z "$(RELEASE_TYPE_VALID)" ]; then \
		echo "Error: Invalid release type '$(RELEASE_TYPE)'. Must be one of: $(VALID_RELEASE_TYPES)"; \
		exit 1; \
	fi

.PHONY: version-bump-major version-bump-minor version-bump-patch
version-bump-major version-bump-minor version-bump-patch: version-bump-%:
	@current_version=$$(git describe --tags 2>/dev/null | sed 's/^v//' || echo "0.0.0"); \
	if [ "$$current_version" = "" ]; then \
		current_version="0.0.0"; \
	fi; \
	major=$$(echo $$current_version | cut -d. -f1); \
	minor=$$(echo $$current_version | cut -d. -f2); \
	patch=$$(echo $$current_version | cut -d. -f3); \
	case "$*" in \
		major) new_version=$$((major + 1)).0.0 ;; \
		minor) new_version=$$major.$$((minor + 1)).0 ;; \
		patch) new_version=$$major.$$minor.$$((patch + 1)) ;; \
	esac; \
	echo "Bumping version from v$$current_version to v$$new_version"; \
	git tag -a "v$$new_version" -m "Release v$$new_version"; \
	echo "Created new tag v$$new_version."

.PHONY: version
version:
	@echo "Current version: $(VERSION)"

.PHONY: pre-release-checks
pre-release-checks:
	@echo "Running pre-release checks..."
	@if [ -n "$$(git status --porcelain)" ]; then \
		echo "Error: Working directory is not clean. Please commit or stash changes."; \
		exit 1; \
	fi
	@if [ -n "$$(git log @{u}.. 2>/dev/null)" ]; then \
		echo "Error: You have unpushed commits. Please push them first."; \
		exit 1; \
	fi

.PHONY: release
release: validate-release-type pre-release-checks lint test
	@echo "Starting release process..."
	@echo "Running tests and checks..."
	$(MAKE) version-bump-$(RELEASE_TYPE)
	@echo "Building and verifying..."
	$(MAKE) build-all
	@echo "Pushing changes..."
	git push
	git push --tags
	@echo "Running goreleaser..."
	goreleaser release --clean
	@new_version=$$(git describe --tags); \
	echo "Release $$new_version completed successfully!"
	@echo "Release checklist:"
	@echo "  ✓ Version bumped"
	@echo "  ✓ Code tested and linted"
	@echo "  ✓ Changes built and verified"
	@echo "  ✓ Changes pushed to remote"
	@echo "  ✓ Release created with goreleaser"
	@echo ""
	@echo "Next steps:"
	@echo "1. Check the release page for the new version"
	@echo "2. Verify the release artifacts"
	@echo "3. Update the changelog if needed"

.PHONY: release-major release-minor release-patch
release-major release-minor release-patch: release-%:
	$(MAKE) release RELEASE_TYPE=$*

.PHONY: example-report
example-report:
	@echo "Generating sample JSON data..."
	@go run examples/jsongen/main.go
	@echo "Generating HTML report..."
	@go run examples/htmlgen/main.go
	@echo "Report generated at examples/output/sample_report.html"
