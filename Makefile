# Makefile for mcp-exec
# Layout: cmd/mcp-exec (binary entrypoint), src/ (package mcpxec), tests/ (package mcpxec_test)

.PHONY: all build clean test install \
	build-linux-amd64 build-linux-arm64 \
	build-darwin-amd64 build-darwin-arm64 \
	build-windows-amd64 \
	build-freebsd-amd64 build-freebsd-arm64 \
	build-netbsd-amd64 build-openbsd-amd64 build-dragonfly-amd64 \
	build-all deps help

# Default values
VERSION ?= 1.0.0
BUILD_DIR ?= ./bin
# Target platform — auto-detected by the Go toolchain: an empty GOOS/GOARCH means
# "the machine running the build", so `make build` builds for the current platform
# without extra flags (that is exactly what plain `go build` does). Set them to
# cross-build: make build GOOS=windows GOARCH=arm64  (the build-<os>-<arch> targets below
# simply pass GOOS/GOARCH on the command line, so they are unaffected)
GOOS ?= 
GOARCH ?= 
# main.version lives in cmd/mcp-exec/main.go, so -X stamps it
LDFLAGS ?= -X main.version=$(VERSION) -s -w

all: build

build:
	@echo "Building mcp-exec..."
	@mkdir -p $(BUILD_DIR)
	GOOS=$(GOOS) GOARCH=$(GOARCH) go build \
		-ldflags="$(LDFLAGS)" \
		-o $(BUILD_DIR)/mcp-exec ./cmd/mcp-exec
	@chmod +x $(BUILD_DIR)/mcp-exec
	@echo "Build complete: $(BUILD_DIR)/mcp-exec"

build-linux-amd64:
	@$(MAKE) build GOOS=linux GOARCH=amd64

build-linux-arm64:
	@$(MAKE) build GOOS=linux GOARCH=arm64

build-darwin-amd64:
	@$(MAKE) build GOOS=darwin GOARCH=amd64

build-darwin-arm64:
	@$(MAKE) build GOOS=darwin GOARCH=arm64

build-windows-amd64:
	@$(MAKE) build GOOS=windows GOARCH=amd64

# BSDs (Go toolchain support: freebsd/netbsd/openbsd — amd64/arm64, dragonfly — amd64 only)
build-freebsd-amd64:
	@$(MAKE) build GOOS=freebsd GOARCH=amd64

build-freebsd-arm64:
	@$(MAKE) build GOOS=freebsd GOARCH=arm64

build-netbsd-amd64:
	@$(MAKE) build GOOS=netbsd GOARCH=amd64

build-openbsd-amd64:
	@$(MAKE) build GOOS=openbsd GOARCH=amd64

build-dragonfly-amd64:
	@$(MAKE) build GOOS=dragonfly GOARCH=amd64

build-all: build-linux-amd64 build-linux-arm64 build-darwin-amd64 build-darwin-arm64 build-windows-amd64 \
	build-freebsd-amd64 build-freebsd-arm64 build-netbsd-amd64 build-openbsd-amd64 build-dragonfly-amd64
	@echo "All builds complete!"
	@echo "Note: chroot needs privileges on Linux and the BSDs, syslog is unavailable on Windows,"
	@echo "      the systemd unit is for Linux only (BSDs use rc.d) — see the README matrix"

clean:
	@rm -rf $(BUILD_DIR)
	@rm -f mcp-exec cmd/mcp-exec/mcp-exec
	@go clean
	@echo "Clean complete"

test:
	@go test -v ./...

install: build
	@echo "Installing mcp-exec..."
	@cp $(BUILD_DIR)/mcp-exec /usr/local/bin/
	@chmod +x /usr/local/bin/mcp-exec
	@echo "Installed to /usr/local/bin/mcp-exec"

# Dependencies
deps:
	@go mod tidy
	@go mod verify

# Help
help:
	@echo "MCP Exec Build System"
	@echo ""
	@echo "Targets:"
	@echo "  make build              - Build for the current platform (GOOS/GOARCH left empty"
	@echo "                          = auto-detected by the Go toolchain)"
	@echo "  make build-linux-amd64  - Build for Linux AMD64"
	@echo "  make build-linux-arm64  - Build for Linux ARM64"
	@echo "  make build-darwin-amd64 - Build for macOS AMD64"
	@echo "  make build-darwin-arm64 - Build for macOS ARM64"
	@echo "  make build-windows-amd64- Build for Windows AMD64"
	@echo "  make build-freebsd-amd64- Build for FreeBSD AMD64"
	@echo "  make build-freebsd-arm64- Build for FreeBSD ARM64"
	@echo "  make build-netbsd-amd64 - Build for NetBSD AMD64"
	@echo "  make build-openbsd-amd64- Build for OpenBSD AMD64"
	@echo "  make build-dragonfly-amd64- Build for DragonFly BSD AMD64"
	@echo "  make build-all          - Build for all platforms"
	@echo "  make clean              - Remove build artifacts"
	@echo "  make test               - Run tests"
	@echo "  make install            - Install to /usr/local/bin"
	@echo "  make deps               - Manage dependencies"
	@echo "  make help               - Show this help"
	@echo ""
	@echo "Variables:"
	@echo "  VERSION   - Version string (default: 1.0.0)"
	@echo "  BUILD_DIR - Output directory (default: ./bin)"
	@echo "  GOOS      - Target OS (empty default = the platform running the build;"
	@echo "              e.g. make build GOOS=windows)"
	@echo "  GOARCH    - Target architecture (empty default = the platform running the build;"
	@echo "              e.g. make build GOARCH=arm64)"
