# syntax=docker/dockerfile:1.4

# Stage 1: Development dependencies
FROM golang:1.24-alpine AS dev-deps
WORKDIR /app
RUN apk add --no-cache git make build-base

# Stage 2: Build dependencies
FROM dev-deps AS build-deps
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

# Stage 3: Build the application
FROM build-deps AS builder
COPY . .
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    make build

# Stage 4: Security scan
FROM aquasec/trivy:latest AS security-scan
COPY --from=builder /app /app
RUN trivy filesystem --no-progress --ignore-unfixed --severity HIGH,CRITICAL /app

# Stage 5: Final runtime
FROM alpine:latest AS runtime

# Install runtime dependencies
RUN apk add --no-cache \
    ca-certificates \
    tzdata \
    && rm -rf /var/cache/apk/*

# Create /app directory
RUN mkdir -p /app

# Set /app as the working directory
WORKDIR /app

# Set the HOME environment variable to /app
ENV HOME=/app

# Copy binary from builder
COPY --from=builder /app/bin/cloudsift /usr/local/bin/cloudsift

# Set environment variables
ENV AWS_SDK_LOAD_CONFIG=1
ENV TZ=UTC

# Verify binary
RUN cloudsift version

# Default command
ENTRYPOINT ["cloudsift"]
CMD ["--help"]

# Labels for metadata
LABEL org.opencontainers.image.title="CloudSift"
LABEL org.opencontainers.image.description="AWS resource scanner and analyzer"
LABEL org.opencontainers.image.source="https://github.com/emptyset-io/cloudsift"
LABEL org.opencontainers.image.vendor="EmptySet.io"
LABEL org.opencontainers.image.licenses="MIT"
