GO ?= go
GOFMT ?= gofmt
BUILD_DIR := bin
BINARY_NAME := cloudsift

# Build information
# Improved version detection that handles non-standard git describe output
VERSION := $(shell git fetch --tags 2>/dev/null || true; \
	raw_version=$$(git describe --tags 2>/dev/null || echo "v0.0.0"); \
	if [ "$$raw_version" = "v0.0.0" ]; then \
		raw_version=$$(git tag -l 'v*' | sort -V | tail -n 1 || echo "v0.0.0"); \
	fi; \
	echo "$$raw_version" | grep -oE '^v?[0-9]+\.[0-9]+\.[0-9]+' || echo "v0.0.0")
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

# Check if goreleaser is installed
GORELEASER := $(shell command -v goreleaser 2> /dev/null)
GORELEASER_PATH := $(GOPATH)/bin/goreleaser

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

.PHONY: install-goreleaser
install-goreleaser:
	@if [ -z "$(GORELEASER)" ]; then \
		echo "Installing goreleaser..."; \
		go install github.com/goreleaser/goreleaser/v2@latest; \
		echo "goreleaser installed to $(GORELEASER_PATH)"; \
	else \
		echo "goreleaser is already installed"; \
	fi

.PHONY: lint
lint: install-lint
	$(GOLANGCI_LINT_PATH) run

.PHONY: test
test:
	$(GO) test -v -coverprofile=coverage.out ./...
	

.PHONY: test-html
test-html: test
	$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated at coverage.html"

.PHONY: build
build: deps
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p $(BUILD_DIR)
	@$(GO) build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME)
	@echo "Build complete: $(BUILD_DIR)/$(BINARY_NAME)"

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
	@echo "Determining current version..."
	@git fetch --tags 2>/dev/null || true
	@current_version=$$(git describe --tags 2>/dev/null | grep -oE '^v?[0-9]+\.[0-9]+\.[0-9]+' || echo "0.0.0"); \
	if [ -z "$$current_version" ]; then \
		# Try to get the latest tag as a fallback \
		current_version=$$(git tag -l 'v*' | sort -V | tail -n 1 | grep -oE '^v?[0-9]+\.[0-9]+\.[0-9]+' || echo "0.0.0"); \
		if [ -z "$$current_version" ]; then \
			echo "No version tag found, starting from 0.0.0"; \
			current_version="0.0.0"; \
		else \
			echo "Found latest tag: $$current_version"; \
		fi; \
	else \
		echo "Found version tag: $$current_version"; \
	fi; \
	# Remove 'v' prefix if present \
	current_version=$${current_version#v}; \
	echo "Parsed version: $$current_version"; \
	major=$$(echo $$current_version | cut -d. -f1); \
	minor=$$(echo $$current_version | cut -d. -f2); \
	patch=$$(echo $$current_version | cut -d. -f3); \
	echo "Current version components: major=$$major, minor=$$minor, patch=$$patch"; \
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
	@git fetch --tags 2>/dev/null || true
	@raw_version=$$(git describe --tags 2>/dev/null || echo "v0.0.0"); \
	if [ "$$raw_version" = "v0.0.0" ]; then \
		# Try to get the latest tag as a fallback \
		raw_version=$$(git tag -l 'v*' | sort -V | tail -n 1 || echo "v0.0.0"); \
	fi; \
	semantic_version=$$(echo "$$raw_version" | grep -oE '^v?[0-9]+\.[0-9]+\.[0-9]+' || echo "v0.0.0"); \
	echo "Current version: $$raw_version (semantic: $$semantic_version)"

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
release: validate-release-type pre-release-checks lint test install-goreleaser
	@echo "Starting release process..."
	@echo "Running tests and checks..."
	$(MAKE) version-bump-$(RELEASE_TYPE)
	@echo "Pushing changes..."
	git push
	git push --tags
	@echo "Running goreleaser..."
	$(GORELEASER_PATH) release --clean
	@new_version=$$(git describe --tags); \
	echo "Release $$new_version completed successfully!"
	@echo "Release checklist:"
	@echo "  ✓ Version bumped"
	@echo "  ✓ Code tested and linted"
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
