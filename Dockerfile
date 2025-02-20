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
FROM alpine:3.19 AS runtime

# Add non-root user
RUN addgroup -S cloudsift && \
    adduser -S cloudsift -G cloudsift

# Install runtime dependencies
RUN apk add --no-cache \
    ca-certificates \
    tzdata \
    && rm -rf /var/cache/apk/*

# Copy binary from builder
COPY --from=builder /app/bin/cloudsift /usr/local/bin/cloudsift

# Set up configuration directory
RUN mkdir -p /etc/cloudsift && \
    chown -R cloudsift:cloudsift /etc/cloudsift

# Switch to non-root user
USER cloudsift
WORKDIR /home/cloudsift

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
