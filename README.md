# MCP Exec Server

An MCP (Model Context Protocol) server written in Go that allows execution of arbitrary
shell commands and utilities with configurable security features, and edits text files with
the standard line-based `edit_file` tool. Implements protocol version `2025-06-18`.

## Features

- **Multiple Transport Modes**: HTTP, HTTPS, Unix domain socket, or stdio
- **Flexible Configuration**: INI format configuration file with environment variable
  overrides and a `--config` command-line flag (precedence: flag > env > file > defaults)
- **Per-Tool Switches** (`[tools]` section): every tool can be switched on or off; a
  tool that is switched off is not announced through `tools/list` and a direct call to
  it is refused (`MCP_EXEC_DISABLED_TOOLS` is the env override for the section)
- **Security Features**:
  - API key authentication (constant-time comparison)
  - Optional chroot isolation for command execution (Linux and the BSDs —
    FreeBSD/DragonFlyBSD/OpenBSD/NetBSD; requires privileges)
  - Allowed-directory roots for the file tool (`allowed_dirs`) — confinement is checked
    after resolving symlinks, so a symlink cannot be used to escape a root
  - Configurable request/response size limits (oversize request → HTTP 413)
  - POST-only endpoint, OPTIONS preflight supported
  - Working-directory path-traversal guard (`..` cannot escape the root under chroot)
- **File Editing (`edit_file`)**: line-based edits applied from the bottom upwards,
  the git-style diff of the change in the reply, `dryRun` preview, atomic write that keeps
  the original permissions
- **Multiple Logging Options**: stdout, file, or syslog (syslog is not supported on Windows)
- **Daemon Mode**: re-exec + setsid; graceful shutdown on SIGINT/SIGTERM (+SIGHUP on unix)
- **Portability**: builds and runs natively on Linux, FreeBSD, DragonFly BSD, OpenBSD,
  NetBSD, macOS and Windows (see the platform matrix)
- **Minimal Dependencies**: Pure Go implementation with standard library only

## Project Layout

```
cmd/mcp-exec/       - binary entrypoint (package main, flag parsing)
src/                - sources, package mcpxec (config, handlers, transports, edit_file, platform files)
tests/              - black-box tests (package mcpxec_test)
docker/             - Dockerfile, docker-compose.yml, .env.example
mcp-exec.ini        - default INI configuration (runtime config, stays at root)
mcp-exec.default    - /etc/default/mcp-exec template (runtime config)
mcp-exec.service    - systemd unit (runtime config)
Makefile, build.sh, install.sh, LICENSE, AUDIT.md
```

`go.mod` stays at the repository root (module root). Go requires the module root to hold
`go.mod`, and tests of a package live in `tests/` as a separate external test package,
so the binary entrypoint moved to `cmd/mcp-exec/`.

## Quick Start

### Build from Source

```bash
# Using Makefile — builds for the platform you run on (nothing to specify)
make build

# Or using build script — same auto-detected default target
./build.sh

# Or manually
go build -o mcp-exec ./cmd/mcp-exec
```

### Basic Usage

```bash
# Start with default settings (http on 0.0.0.0:8080, ./mcp-exec.ini if present)
./mcp-exec

# Explicit config file
./mcp-exec --config /etc/mcp-exec/mcp-exec.ini

# Print version / usage
./mcp-exec --version
./mcp-exec --help

# Start with stdio transport (for direct integration)
MCP_EXEC_TRANSPORT=stdio ./mcp-exec

# Start with custom configuration
MCP_EXEC_ADDR=127.0.0.1 MCP_EXEC_PORT=9090 ./mcp-exec
```

## Configuration

### Configuration File (INI Format)

Create a `mcp-exec.ini` file with the following sections:

```ini
[server]
; Network address to bind to (empty = all interfaces)
; For transport=domain this is the Unix socket path, e.g. /tmp/mcp-exec.sock
addr = 0.0.0.0

; Port to listen on (ignored for domain/stdio transports)
port = 8080

; Transport type: http, https, domain, stdio
transport = http

; Maximum request size in bytes (default: 1048576 = 1MB)
max_request_size = 1048576

; Maximum response size in bytes (default: 10485760 = 10MB)
max_response_size = 10485760

; Authorization keys (comma-separated list of API keys)
; Leave empty to disable authentication
auth_keys = your-secret-api-key-here

; Run as daemon (true/false). Daemonized via re-exec + setsid;
; log mode "stdout" loses output in daemon mode, use file/syslog.
daemon = false

[logging]
; Log mode: stdout, file, syslog
mode = stdout

; Log file path (used when mode = file)
file = /var/log/mcp-exec.log

; Syslog settings (used when mode = syslog; not supported on Windows)
syslog_tag = mcp-exec
syslog_network = udp
syslog_address = /dev/log

[security]
; Chroot path for command execution
; If set, all commands will be executed within this chroot.
; Supported on Linux and the BSDs (FreeBSD, DragonFlyBSD, OpenBSD, NetBSD);
; requires root or CAP_CHROOT. The executable, shell and any
; working directory must exist INSIDE the chroot at the same absolute paths.
chroot_path =

; Roots the file tool (edit_file) is confined to, comma-separated, e.g.
;   allowed_dirs = /srv/mcp-exec/files, /etc/mcp-exec
; With no roots set the file tool falls back to chroot_path as its root
; (chroot() is applied to the child processes only, so the server itself sees
; the host filesystem); with neither set it is unconfined, like exec/shell.
; Absolute paths are required when allowed_dirs is set, and symlinks cannot
; take the tool outside a root.
allowed_dirs =

; Largest file the file tool will read, in bytes (default: 10485760 = 10MB)
max_file_size = 10485760

[tools]
; Per-tool switches — the key is the tool name, the value is parsed like the
; other INI booleans (true/1/yes/on are on, anything else is off). A tool that
; is switched off is not announced through tools/list, and a direct call to it
; is refused; a tool the section does not name keeps the default (enabled).
exec = true
shell = true
edit_file = true
```

### Command-Line Flags

| Flag | Description |
|------|-------------|
| `--config <path>` | Path to INI configuration file (overrides env and default path) |
| `--version` | Print version and exit |
| `--help` | Print usage and exit |

### Environment Variables

All configuration options can be overridden via environment variables
(they override the config file but are overridden by the `--config` flag):

| Variable | Description |
|----------|-------------|
| `MCP_EXEC_CONFIG` | Path to configuration file (default: `mcp-exec.ini`) |
| `MCP_EXEC_ADDR` | Network address; for `domain` transport — unix socket path |
| `MCP_EXEC_PORT` | Port to listen on |
| `MCP_EXEC_TRANSPORT` | Transport type: `http`, `https`, `domain`, `stdio` |
| `MCP_EXEC_AUTH_KEYS` | Comma-separated list of API keys |
| `MCP_EXEC_MAX_REQ_SIZE` | Maximum request size in bytes |
| `MCP_EXEC_MAX_RESP_SIZE` | Maximum response size in bytes |
| `MCP_EXEC_CHROOT` | Chroot path for command execution (Linux and the BSDs) |
| `MCP_EXEC_ALLOWED_DIRS` | Roots the `edit_file` tool is confined to (comma-separated) |
| `MCP_EXEC_MAX_FILE_SIZE` | Largest file `edit_file` will read, in bytes |
| `MCP_EXEC_DISABLED_TOOLS` | Tools to switch off (comma-separated names), overrides the `[tools]` section |
| `MCP_EXEC_DAEMON` | Run as daemon (true/false/1/0/yes/no/on/off) |
| `MCP_EXEC_LOG_MODE` | Log mode: `stdout`, `file`, `syslog` |
| `MCP_EXEC_LOG_FILE` | Log file path |
| `MCP_EXEC_SYSLOG_TAG` | Syslog tag |
| `MCP_EXEC_SYSLOG_NETWORK` | Syslog network (tcp, udp, unix) |
| `MCP_EXEC_SYSLOG_ADDRESS` | Syslog address |
| `TLS_CERT` | TLS certificate file path (required for `https`, optional for `domain`) |
| `TLS_KEY` | TLS private key file path (required for `https`, optional for `domain`) |

## API Reference

### HTTP Transport

The server accepts JSON-RPC 2.0 requests at the `/mcp` endpoint.

- **Method**: `POST` only (`GET`/`PUT`/... → `405`, `OPTIONS` → `204` with CORS headers)
- **Limits**: oversized request body → `413` (JSON-RPC error body); oversized response →
  JSON-RPC error. All limits apply on HTTP and stdio transports.
- **Notifications** (`notifications/initialized`, `notifications/cancelled`, legacy
  `initialized`) are consumed without a response (HTTP `204` / nothing on stdio).
- **Statuses**: auth failure → `401`, large request → `413`, JSON-RPC errors in the body
  are returned with `200`.

Supported methods: `initialize`, `notifications/initialized`, `notifications/cancelled`,
`ping`, `tools/list`, `tools/call`, `resources/list`, `prompts/list` (empty lists).

#### Request Format

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "tools/call",
  "params": {
    "name": "exec",
    "arguments": {
      "command": "ls",
      "args": ["-la"],
      "dir": "/tmp",
      "timeout": 30
    }
  }
}
```

#### Response Format

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "content": [
      {
        "type": "text",
        "text": "Command executed: ls\nExit code: 0\n\n--- STDOUT ---\ntotal 8\n..."
      }
    ],
    "isError": false
  }
}
```

**stdout and stderr are always returned, whatever the exit code was.** A run that exits
`0` but writes warnings, progress or diagnostics to stderr still gets that stderr back, and
the `--- STDERR ---` section is emitted even when nothing was written there — so a client
can tell "nothing on stderr" from "stderr was dropped". `isError` stays `false`: a non-zero
exit code is a normal outcome for an exec server, not a transport error.

**A tool that cannot do its job answers with `isError: true`.** For the file tool this
follows the protocol: an execution failure (the file is missing, a read or a write failed,
the file changed under the caller) is a tool *result* whose `content` carries the message
and whose `isError` is `true`; only bad input — unparsable arguments, out-of-range line
numbers, a path outside the allowed directories — travels the JSON-RPC error channel with
code `-32602`. The `exec`/`shell` tools keep reporting execution errors as JSON-RPC
`-32000` (the server's own custom range, `-32001`/`-32002` are used the same way).
A process that never started — the program is not in `PATH`, the path given as
`command` does not exist, the working directory is missing — is exactly such an
error: it reaches the caller as `-32000`, not as an empty "exit code 0" report.
The failure is written to the log as well; logging and answering are not alternatives.

### Authentication

Authentication can be provided via:

1. **Authorization Header**: `Authorization: Bearer <api-key>`
2. **X-API-Key Header**: `X-API-Key: <api-key>`

Key comparisons use `crypto/subtle` constant-time comparison. If `auth_keys` is empty,
authentication is disabled. Stdio transport is unauthenticated by design (local pipe).

### Available Tools

Every tool carries the `2025-06-18` annotation hints (`readOnlyHint`, `destructiveHint`,
`idempotentHint`, `openWorldHint`), so a client can see what a call is allowed to do before
making it.

Every tool can be switched on or off in the `[tools]` section (the key is the tool name,
the value is parsed like the other INI booleans). A tool that is switched off is not
announced through `tools/list`, and a direct `tools/call` does not reach its handler
either — it is refused the way an unknown tool is.

#### `exec` - Execute a command directly

Execute a command with arguments.

**Parameters:**
- `command` (required): Command to execute
- `args` (optional): Command arguments as array
- `env` (optional): Environment variables as KEY=VALUE array
- `dir` (optional): Working directory (under chroot — path inside the chroot; `..` cannot
  escape the new root)
- `timeout` (optional): Timeout in seconds (default: 60)

**Returns:** the process report — `Command executed`, `Exit code`, `--- STDOUT ---`,
`--- STDERR ---`; stdout and stderr are always included, whatever the exit code was.
A process that could not be started — the program is not in `PATH`, the path in
`command` does not exist, the working directory is missing — comes back as the
JSON-RPC error `-32000` with the process error in `data`.

**Example:**
```json
{
  "command": "echo",
  "args": ["Hello, World!"],
  "timeout": 10
}
```

#### `shell` - Execute through shell interpreter

Execute a command through a shell.

**Parameters:**
- `command` (required): Shell command to execute
- `shell` (optional): Shell to use (default: `/bin/sh`)
- `env` (optional): Environment variables
- `dir` (optional): Working directory
- `timeout` (optional): Timeout in seconds

**Returns:** the same report as `exec` — stdout and stderr are always returned, whatever
the exit code was — and the same JSON-RPC error `-32000` when the shell or the working
directory does not exist, so the process never started.

**Example:**
```json
{
  "command": "echo 'Hello, World!' && date",
  "shell": "/bin/bash"
}
```

#### `edit_file` - Make line-based edits to a text file

Make line-based edits to a text file. Each edit replaces exact line sequences with new
content. Returns a git-style diff showing the changes made. Only works within allowed
directories.

**Parameters:**
- `path` (required): the file to edit — absolute when `allowed_dirs` is set
- `edits` (required, at least one): the line-based edits, each one being `lineStart` and
  `lineEnd` (1-indexed, inclusive) plus `text`; `text` may itself be several lines long and
  an empty `text` deletes the range. An optional `context` (`line` + `text`) anchors an edit
  to the original file — if that line does not hold that text, the call is refused instead
  of quietly rewriting the wrong lines
- `dryRun` (optional, default `false`): preview the change as a git-style diff without
  writing the file

**Behaviour:**
- edits are applied from the bottom of the file upwards, so every edit's line numbers stay
  valid whatever order they arrive in; overlapping ranges are rejected, since their result
  would depend on the order they are applied in
- the reply is the git-style diff of the change — `Index:` / `===` / `--- ` / `+++ ` headers,
  `@@ -old +new @@` hunk headers, git's three lines of context, and the
  `\ No newline at end of file` marker where git would put it; a call that turns out to
  change nothing answers `No modification`
- inside an allowed root the diff header shows the path relative to that root (as the
  reference server does), so the absolute layout of the server host is not leaked
- a file that predominantly uses CRLF stays CRLF; mixed line endings are normalised to the
  file's dominant style, which is what an editor does on save
- the file is written atomically — a temporary file in the target's own directory, then a
  rename — and keeps its original permissions; it is re-read immediately before writing so a
  concurrent writer is not clobbered silently
- failures to touch the file are reported as a tool result with `isError: true`; bad input
  is a JSON-RPC error `-32602`

**Example:**
```json
{
  "path": "/srv/mcp-exec/files/hosts",
  "edits": [
    {
      "lineStart": 3,
      "lineEnd": 4,
      "text": "beta\tupdated line",
      "context": { "line": 1, "text": "alpha" }
    }
  ],
  "dryRun": true
}
```

## Build Options

### Using Makefile

`make build` needs no flags to target the machine it runs on: `GOOS`/`GOARCH` are left
empty by default, and an empty GOOS/GOARCH is Go's own auto-detection — plain
`go build` builds for the host platform. Set them to cross-build; the
`build-<os>-<arch>` targets below do exactly that.

```bash
# Build for the current platform (output: ./bin/mcp-exec) —
# on an Apple-Silicon Mac this builds darwin/arm64
make build

# Build for specific platform
make build-linux-amd64
make build-linux-arm64
make build-darwin-amd64
make build-darwin-arm64
make build-windows-amd64
make build-freebsd-amd64
make build-freebsd-arm64
make build-netbsd-amd64
make build-openbsd-amd64
make build-dragonfly-amd64

# Any other GOOS/GOARCH pair the Go toolchain supports
make build GOOS=openbsd GOARCH=arm64

# Build for all platforms
make build-all

# Run tests
make test

# Clean build artifacts
make clean

# Install to /usr/local/bin
make install
```

### Using build.sh

`TARGET_OS`/`TARGET_ARCH` default to the platform the script runs on (detected via
`go env`, with `uname` as the fallback); set them only to cross-build.

```bash
# Basic build (targets the host platform)
./build.sh

# Custom build directory
BUILD_DIR=/opt/mcp-exec ./build.sh

# Cross-compilation (also freebsd/netbsd/openbsd/dragonfly; dragonfly is amd64-only)
TARGET_OS=freebsd TARGET_ARCH=amd64 ./build.sh
TARGET_OS=darwin TARGET_ARCH=arm64 ./build.sh
```

### Using Docker

Architecture is auto-detected: BuildKit passes `TARGETARCH` for the target platform, and
`go env GOARCH` covers a plain native build, so a native build on an arm64 host stays
arm64. The container OS is Linux, so `TARGETOS` defaults to `linux`.

```bash
# From the repository root:
docker compose -f docker/docker-compose.yml up --build

# Or from the docker/ directory (uses docker/.env, see docker/.env.example):
cd docker && cp .env.example .env && docker compose up --build

# To force another architecture pass the build arg explicitly:
docker build -f docker/Dockerfile --build-arg TARGETARCH=arm64 .
```

## Transport Modes

### HTTP/HTTPS

Standard HTTP/HTTPS server. Useful for network-based MCP clients.

```bash
MCP_EXEC_TRANSPORT=http ./mcp-exec
TLS_CERT=/path/to/cert.pem TLS_KEY=/path/to/key.pem MCP_EXEC_TRANSPORT=https ./mcp-exec
```

`https` **requires** `TLS_CERT`/`TLS_KEY`; the server exits with an error if they are
missing (no silent downgrade to plain HTTP).

### Domain Socket

Unix domain socket transport. Set `MCP_EXEC_TRANSPORT=domain` and put the socket path in
`addr` (`MCP_EXEC_ADDR` / `[server] addr`). The `port` is ignored. Stale socket files from
previous runs are cleaned up on startup and on exit. TLS is optional: set `TLS_CERT`/`TLS_KEY`
to serve TLS over the socket.

```bash
MCP_EXEC_TRANSPORT=domain MCP_EXEC_ADDR=/tmp/mcp-exec.sock ./mcp-exec
```

### Stdio

Direct stdio transport. Useful for direct integration with MCP-compatible applications.
Request/response size limits are enforced on this transport as well.

```bash
MCP_EXEC_TRANSPORT=stdio ./mcp-exec
```

### Platform Support Matrix

| Feature                     | Linux     | FreeBSD | DragonFlyBSD | OpenBSD | NetBSD | macOS   | Windows |
|-----------------------------|-----------|---------|--------------|---------|--------|---------|---------|
| http / https                | ✅        | ✅      | ✅           | ✅      | ✅     | ✅      | ✅      |
| domain (unix socket)        | ✅        | ✅      | ✅           | ✅      | ✅     | ✅      | ⚠️¹     |
| stdio                       | ✅        | ✅      | ✅           | ✅      | ✅     | ✅      | ✅      |
| edit_file (file tool)       | ✅        | ✅      | ✅           | ✅      | ✅     | ✅      | ✅      |
| chroot_path                 | ✅²       | ✅²     | ✅²          | ✅²     | ✅²    | ❌³     | ❌³     |
| logging to syslog           | ✅⁴       | ✅⁴     | ✅⁴          | ✅⁴     | ✅⁴    | ✅⁴     | ❌⁵     |
| daemon (setsid)             | ✅        | ✅      | ✅           | ✅      | ✅     | ✅      | n/a⁶    |
| SIGINT/SIGTERM/SIGHUP       | ✅        | ✅      | ✅           | ✅      | ✅     | ✅      | ❌⁷     |
| init system for the service | systemd⁸  | rc.d⁸   | rc.d⁸        | rc.d⁸   | rc.d⁸  | launchd⁸ | n/a⁸   |

¹ Unix domain sockets exist on Windows but are not typically used; the transport works via
Go's `net` package.
² `chroot()` is applied in the child before exec: Go exposes `SysProcAttr.Chroot` on Linux
and on the BSDs, so exec-time chroot works natively there. It needs privileges — root, or
`CAP_CHROOT` under the systemd unit on Linux. The chroot must contain every executable,
shell and working directory at the same absolute paths.
³ `chroot_path` is not used on these platforms (on macOS `SysProcAttr.Chroot` exists but the
project keeps it disabled by design); setting it is a fatal startup error. It does not confine
the file tool either — the server process itself is never chrooted, so `allowed_dirs` is what
confines `edit_file` (the chroot directory is used as its root by default).
⁴ Syslog devices: `/dev/log` on Linux and the BSDs, `/var/run/log` on macOS — all handled by
`log/syslog` (`src/syslog_unix.go`, build tag `unix`).
⁵ `log/syslog` cannot connect on Windows; `log_mode=syslog` fails at startup.
⁶ Daemon mode forks with setsid on unix-like platforms; on Windows `daemon=true` re-execs the
child without setsid.
⁷ Graceful shutdown on Windows handles SIGINT/SIGTERM only; unix platforms also handle SIGHUP.
⁸ The shipped `mcp-exec.service` and `/etc/default/mcp-exec` are for Linux/systemd. On
FreeBSD/DragonFlyBSD/OpenBSD/NetBSD use the platform init system — an rc.d script running the
same `--config` command line; on macOS use a launchd plist. `daemon=true` in the config works
on every unix platform and is the simplest option when there is no init system. `install.sh`
skips the unit on non-Linux systems.

## Daemon Mode

`daemon=true` re-execs the process, the parent exits, and the child runs in a new session
(setsid) so it survives terminal closure. The child inherits stdin/stdout/stderr as
`/dev/null`, so `log_mode=stdout` produces no visible output in daemon mode — use
`file` or `syslog`. The `MCP_EXEC_DAEMONIZED=1` marker env var prevents a fork loop.
Works on Linux, FreeBSD, DragonFly BSD, OpenBSD, NetBSD and macOS (on Windows the child is
re-execed without setsid).

## Security Considerations

1. **Authentication**: Always use authentication in production environments.
   This server executes arbitrary commands — without keys it is an open RCE endpoint.
2. **Chroot**: available on Linux and the BSDs (FreeBSD/DragonFlyBSD/OpenBSD/NetBSD).
   `chroot()` requires root or the platform equivalent — on Linux add
   `AmbientCapabilities=CAP_CHROOT` to the systemd unit when running as a non-root user;
   on the BSDs run as root (or grant the equivalent privilege). The chroot must contain
   every executable, shell and working directory **at the same absolute paths** as on the host.
3. **`edit_file` roots**: `allowed_dirs` (and `MCP_EXEC_ALLOWED_DIRS`) confine the file
   tool, and the check is made after resolving symlinks, so a link cannot take it outside
   the roots. With no roots the tool falls back to `chroot_path` as its root; with neither
   set it is unconfined, exactly as `exec`/`shell` are. Set `max_file_size` to keep the tool
   from pulling a huge file into memory.
4. **Size Limits**: Configure appropriate request/response size limits; they are enforced
   on HTTP, domain socket and stdio transports.
5. **Network**: Bind to appropriate network interfaces; CORS is `*` by default, so any
   browser origin may POST to this endpoint.
6. **Logging**: Enable logging for audit purposes.
7. **Command injection**: The `shell` tool passes the command string to `sh -c` as is —
   that is the intended behaviour (arbitrary utilities, arbitrary arguments), so treat
   the model as a trusted caller or use chroot isolation.

## Example systemd Service

The shipped `mcp-exec.service` matches this example (create user `mcp-exec` first —
`install.sh` does it automatically):

```ini
[Unit]
Description=MCP Exec Server
Documentation=https://github.com/mcp-exec
After=network.target

[Service]
Type=simple
User=mcp-exec
Group=mcp-exec
ExecStart=/usr/local/bin/mcp-exec --config /etc/mcp-exec/mcp-exec.ini
EnvironmentFile=-/etc/default/mcp-exec
Restart=on-failure
RestartSec=5

# chroot() needs CAP_CHROOT when running as a non-root user
AmbientCapabilities=CAP_CHROOT

# Security hardening
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/log
PrivateTmp=true

# Resource limits
LimitNOFILE=65535
LimitNPROC=128

[Install]
WantedBy=multi-user.target
```

On FreeBSD, DragonFly BSD, OpenBSD and NetBSD the equivalent is an rc.d script
(`/usr/local/etc/rc.d/mcp-exec`, using `/etc/rc.subr` on FreeBSD/DragonFly, `rc.subr` on
OpenBSD/NetBSD) running the same `--config` command line; on macOS use a launchd plist.
`install.sh` installs the systemd unit only on Linux.

## License

MIT License — see `LICENSE`.

## Author

MCP Exec Server - A minimal MCP server for command execution
