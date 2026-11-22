# Build stage (run from the repository root: docker/Dockerfile, context = root)
FROM golang:1.21-alpine AS builder

WORKDIR /build

# Copy go.mod for caching (no external dependencies, go.sum not needed)
COPY go.mod .
RUN go mod download

# Copy source code (library core + cmd entrypoint)
COPY src/ src/
COPY cmd/ cmd/

# Build
ARG VERSION=1.0.0
# Platform is auto-detected: BuildKit fills TARGETOS/TARGETARCH for the target
# platform, and `go env` gives the platform of the builder container itself
# (the host machine for a native build). TARGETOS defaults to linux because the
# image runs on a linux container OS — cross-build for another OS needs a base
# image for it, so `go env` is the right default here.
ARG TARGETOS=linux
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS="${TARGETOS:-linux}" GOARCH="${TARGETARCH:-$(go env GOARCH)}" go build \
    -ldflags="-X main.version=${VERSION} -s -w" \
    -o /build/mcp-exec ./cmd/mcp-exec

# Runtime stage
FROM alpine:3.19

# Install runtime dependencies
RUN apk add --no-cache ca-certificates tzdata

# Create non-root user
RUN addgroup -g 1000 -S mcp-exec && \
    adduser -u 1000 -S mcp-exec -G mcp-exec

# Copy binary from builder
COPY --from=builder /build/mcp-exec /usr/local/bin/mcp-exec

# Copy default configuration (loaded via --config flag below)
COPY mcp-exec.ini /etc/mcp-exec.ini

# Create log directory
RUN mkdir -p /var/log && chown mcp-exec:mcp-exec /var/log

# Switch to non-root user
USER mcp-exec

WORKDIR /home/mcp-exec

# Expose port
EXPOSE 8080

# Health check: POST-only endpoint, must authenticate.
# Key comes from MCP_EXEC_AUTH_KEYS (set by compose) or the default config file.
# NOTE: running as non-root means chroot_path is unavailable without
# --privileged / CAP_CHROOT, and log_mode=syslog has no /dev/log here.
HEALTHCHECK --interval=30s --timeout=10s --start-period=10s --retries=3 \
    CMD wget -q -O /dev/null --post-data='{"jsonrpc":"2.0","id":1,"method":"ping"}' --header="Authorization: Bearer ${MCP_EXEC_AUTH_KEYS:-your-secret-api-key-here}" http://127.0.0.1:8080/mcp || exit 1

# Default command (--config is a real flag of the binary)
ENTRYPOINT ["/usr/local/bin/mcp-exec"]
CMD ["--config", "/etc/mcp-exec.ini"]
