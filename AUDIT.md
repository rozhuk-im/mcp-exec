# Code audit of the `mcp-exec` project

Audit date: 2026-11-16 (21:31, +02:00)
Status: **fixes applied 2026-11-16**; structure rebuilt (see section 9);
BSD support added 2026-11-18 (see section 10); auto-detection of the build platform
added 2026-11-18 (see section 11); stdout/stderr are always returned — 2026-11-20 (see section 12);
`edit_file` added 2026-11-21 (see section 13); a process start failure is returned
through MCP — 2026-11-22 (see section 14); per-tool switches, the `[tools]` section
— 2026-11-22 (see section 15); `initialize` reports the host it runs on — 2026-11-24
(see section 16)

Files audited (current structure): `src/server.go`, `src/edit_file.go`, `src/platform_linux.go`,
`src/platform_darwin.go`, `src/platform_windows.go`, `src/platform_other.go`,
`src/platform_freebsd.go`, `src/platform_netbsd.go`, `src/platform_openbsd.go`, `src/platform_dragonfly.go`, `src/syslog_unix.go`, `src/syslog_other.go`, `src/syslog_windows_stub.go`,
`cmd/mcp-exec/main.go`, `tests/main_test.go`, `tests/edit_file_test.go`, `tests/platform_test.go`,
`tests/platform_bsd_test.go`, `tests/platform_linux_test.go`,
`tests/platform_darwin_test.go`, `tests/platform_windows_test.go`,
`tests/platform_unix_test.go`, `tests/tools_config_test.go`, `tests/server_info_test.go`, `go.mod`, `Makefile`, `build.sh`, `install.sh`, `README.md`,
`mcp-exec.ini`, `mcp-exec.default`, `mcp-exec.service`, `docker/Dockerfile`,
`docker/docker-compose.yml`, `docker/.env.example`, `.gitignore`.

---

## 1. Requirements compliance

| Requirement | Status | Comment |
|---|---|---|
| MCP server in Go, runs any utilities with any arguments | ✅ | Tools `exec` and `shell`, arguments are passed as-is |
| Project name `mcp-exec` | ✅ | Matches |
| Settings in an INI file | ✅ | Own mini INI parser, no external dependencies |
| daemon/foreground | ✅ | Re-exec + setsid, the stdout log is lost in daemon mode (documented) |
| Logging to file/syslog/stdout | ✅ | Works through the shared logger; syslog is closed at shutdown |
| Address and port for incoming requests | ✅ | `addr`/`port`; for domain, addr = socket path |
| Authorization keys | ✅ | `auth_keys`, Bearer + X-API-Key, constant-time comparison |
| Transport https/http/domain/stdio | ✅ | domain = unix socket; https requires certificates (no silent downgrade) |
| Maximum request size | ✅ | HTTP: `MaxBytesReader` + 413; stdio: explicit limit check |
| Maximum response size | ✅ | Checked in HTTP and stdio |
| chroot path | ✅ | Linux + BSD (FreeBSD/DragonFlyBSD/OpenBSD/NetBSD); privileges documented; `dir` is resolved inside the chroot; on macOS/Windows — fatal startup error |
| FreeBSD/DragonFlyBSD/OpenBSD/NetBSD support | ✅ | See section 10 (2026-11-18) |
| stdout/stderr are always returned | ✅ | See section 12 (2026-11-20) — stderr is not tied to the exit code |
| readme.md with documentation | ✅ | Updated to match the actual behaviour |
| Build scripts | ✅ | `Makefile`, `build.sh`, `install.sh`; cross-builds compile; the target OS/architecture is auto-detected (see section 11) |
| Minimizing dependencies | ✅ | stdlib only, `go.mod` without external modules |
| `edit_file` tool (the standard tool of the reference filesystem server) | ✅ | See section 13 (2026-11-21) — line-based edits, git-style diff, dryRun, allowed_dirs |
| Per-tool switches (`[tools]` section) | ✅ | See section 15 (2026-11-22) — a switched-off tool is not announced and a direct call is refused |

---

## 2. Critical findings

### CRIT-1. `main.go` did not compile: unused `net` import — ✅ FIXED
The import is now used: the domain transport via `net.Listen("unix", ...)` in `src/server.go`.

### CRIT-2. The `domain` transport was not implemented — ✅ FIXED
The domain transport listens on a unix socket (path from `addr`, fallback `/tmp/mcp-exec.sock`),
cleans the stale socket on start/stop, TLS is optional. README updated.

### CRIT-3. The configured logging was not used — ✅ FIXED
All messages go through the shared `logPrintf`/`logFatalf` (fatals — through the logger, with a
fallback to stderr before it is initialized); `syslogWriter` is closed on exit.

### CRIT-4. stdio ignored the limits / silently died on long lines — ✅ FIXED
Line-by-line reading via `bufio.Reader` (no 64 KiB limit), explicit
`max_request_size`/`max_response_size` checks. Notifications get no response (HTTP 204 / stdio — nothing).

### CRIT-5. Tests did not compile (unused `net/http`) — ✅ FIXED
The import is used; tests added for notifications, 405/204/413, json.Number ID, resolveDir.

---

## 3. High priority

### HIGH-1. Security hot zones — ✅ PARTIAL (accepted as a design constraint)
- RCE-by-design documented in README; Docker/Compose: the healthcheck is now POST with authorization;
  inside the container the config is loaded by default through the `--config` flag (see MED-1).
- `CheckAuth` — `crypto/subtle.ConstantTimeCompare` ✅.
- CORS `*` + preflight OPTIONS ✅.

### HIGH-2. daemonize — ✅ FIXED
Re-exec + `Setsid` (unix platforms), the parent exits after Start, `MCP_EXEC_DAEMONIZED=1`
guards against a fork loop. SIGINT/SIGTERM handled (+SIGHUP on unix) — graceful shutdown of the
server. Documented: the stdout log is lost in daemon mode.

### HIGH-3. Cross-builds did not compile — ✅ FIXED
`chroot`/`Setpgid`/`Setsid`/`Kill(-pid)` moved into platform files with build tags
(`src/platform_{linux,darwin,windows,other}.go`; since 2026-11-18 — see section 10 —
`src/platform_{freebsd,netbsd,openbsd,dragonfly}.go`). On platforms without chroot
`chroot_path` is a fatal startup error. `log/syslog` compiles everywhere (the Windows
runtime limitation is documented).

### HIGH-4. `-X main.version` was a dead setting — ✅ FIXED
`var version` exists in `cmd/mcp-exec/main.go`, is passed into `Run()`, is used in
initialize; `-version` prints it.

### HIGH-5. chroot privileges/path semantics — ✅ FIXED
`dir` under chroot is resolved inside the new root (`filepath.Clean("/"+dir)` — `..` does not
escape the root); without chroot the host path is used as-is. Systemd unit: `AmbientCapabilities=CAP_CHROOT`,
`User=mcp-exec` (install.sh creates the user). Requirements on the chroot content — in README.

---

## 4. Medium priority

### MED-1. Docker: configuration was not hooked up — ✅ FIXED
The `--config` flag was added (priority: flag > env > file > defaults); the Dockerfile CMD works;
install.sh puts `mcp-exec.default` into `/etc/default/mcp-exec` (as the file header and the unit's
EnvironmentFile promise it), creates the service user, double copying and `chown /var/log` removed.

### MED-2. HTTPS without certificates silently degraded — ✅ FIXED
`https` without `TLS_CERT/TLS_KEY` — fatal startup error (no silent downgrade).

### MED-3. `io.LimitReader` → silent truncation — ✅ FIXED
HTTP: `http.MaxBytesReader`, overflow → 413 + a JSON-RPC error; stdio: explicit check.

### MED-4. No method check / OPTIONS — ✅ FIXED
POST-only (other methods → 405), OPTIONS → 204 with CORS; authorization errors — JSON-RPC body + 401.

### MED-5. Race on timeout — ✅ FIXED
After kill the process completion is awaited (up to 2 s), the buffers are read after the reap;
processes are started in their own process group (unix) — the whole group is killed.

### MED-6. Dead code — ✅ FIXED
`mu` protects the logger; dead `ini:`-tags removed (the parser is manual — comment in the
struct); unreachable code eliminated.

### MED-7. Inconsistency between systemd/README/install.sh — ✅ FIXED
Unit: `User=mcp-exec`, `LimitNOFILE=65535`, `CAP_CHROOT`; the README example matches;
install.sh: user, `/etc/default/mcp-exec`, no double copying and no chown of the whole /var/log.

---

## 5. Low priority

1. protocolVersion → `2025-06-18` ✅
2. `resources/list`/`prompts/list`/`resources/templates/list` → empty lists ✅
3. Notifications get no response ✅
4. LICENSE (MIT) added ✅
5. `w.Write` errors are logged ✅
6. Non-standard code `-32604` → custom range `-32001/-32002` ✅
7. ID > 2^53: decode through `json.Number` (JSON) ✅
8. Ratio of the server timeouts to exec-timeout — documented
9. Daemon+stdio: stdin=/dev/null — documented
10. Path-traversal protection for `dir` ✅ (ResolveDir)

---

## 6. What was done well (no changes)

- Zero external dependencies (stdlib) — a direct fulfilment of the minimization requirement.
- Own compact INI parser + override through env variables.
- Structure: Makefile + build.sh + install.sh + Docker + compose + .env.example + systemd.
- JSON-RPC codes are correct; command timeouts; Bearer/X-API-Key authorization; tests.

## 7. Summary list of priority fixes (TL;DR) — all done

1. Unused imports → compilation ✅
2. domain → unix socket ✅
3. Logging through the shared logger ✅
4. stdio: limits + correct handling of long lines and notifications ✅
5. Docker/`--config`/install.sh ✅
6. daemonize: setsid + shutdown signal ✅
7. HTTPS without certificates → fatal error ✅
8. MaxBytesReader/413, POST/OPTIONS, JSON errors ✅
9. Build tags for chroot/platform parts ✅
10. `var version`, `crypto/subtle`, LICENSE, `resources/prompts`, `CAP_CHROOT` in the unit ✅

## 8. Status after the fixes (2026-11-16)

Changed: `src/server.go`, `src/platform_linux.go`, `src/platform_darwin.go`,
`src/platform_windows.go`, `cmd/mcp-exec/main.go`, `tests/main_test.go`, `README.md`,
`mcp-exec.ini`, `mcp-exec.default`, `mcp-exec.service`, `Makefile`, `build.sh`,
`install.sh`, `docker/Dockerfile`, `docker/docker-compose.yml`, `docker/.env.example`,
`.gitignore`, `LICENSE`.

Static check (build/run unavailable in the audit environment — no Go toolchain):
compilation (imports, build tags, platform function signatures, `go 1.21` compatibility —
`http.MaxBytesError` since Go 1.17), test imports, tests matching the code behaviour.

Accepted limitations (documented in README):
- exec/shell = RCE by design; model — trusted caller or chroot isolation;
- chroot: Linux + the BSD row (since 2026-11-18, see section 10) + privileges, executables must be
  inside the chroot under the same paths; on macOS — deliberately disabled;
- syslog: unavailable on Windows; daemon mode: stdout logging loses its output;
- the stdio transport is not authenticated (local channel).

## 9. Structure reorganization (2026-11-16)

Per the requirement, sources were moved to `src/`, tests to `tests/`, Docker files to `docker/`.
Taking Go limitations into account:

- `src/` — the `mcpxec` package (library): `server.go` (former main.go, `main()` replaced with
  `Run(version, configPath)`), platform files `platform_{linux,darwin,windows,other}.go` and
  (since 2026-11-18) `platform_{freebsd,netbsd,openbsd,dragonfly}.go`;
- `cmd/mcp-exec/` — a thin `package main` (flag parsing, calling `Run`), because a `main`
  package cannot be imported or tested from a separate directory;
- `tests/` — the external test package `mcpxec_test`, imports `github.com/mcp-exec/src`; API
  export (`Run`, `Cfg`, `LoadConfig`, `SetupLogger`, `CheckAuth`, `HandleRequest`,
  `HandleMCP`, `DecodeRequest`, `FormatResult`, `ResolveDir`, `ParseBool`) — the minimum
  needed for black-box tests; since 2026-11-18 `ChrootSupported`, `ProcAttr`, `DaemonAttr`,
  `KillChild`, `PlatformSignals`, `SyslogLogInfo` were added here as well (they were already
  used inside the package, they simply became exported for the sake of the matrix tests); since
  2026-11-21 (see section 13) — `ExecuteEditFile`, `ApplyEdits`, `GenerateGitDiff`,
  `ResolveEditPath`, `DefaultMaxFileSize` and the types `EditParams`/`LineEdit`/`LineContext`/
  `EditResult`/`ToolAnnotations`;
- `docker/` — `Dockerfile` (context = the repository root, `COPY src/`, `COPY cmd/`),
  `docker/docker-compose.yml` (context: `..`, dockerfile: `docker/Dockerfile`, volumes `../logs`),
  `.env.example` (next to the compose file, as compose requires);
- `go.mod` stayed in the root — it is the module root (`go build ./cmd/mcp-exec`, `go test ./...`);
- runtime configs `mcp-exec.ini`, `mcp-exec.default`, `mcp-exec.service` kept in the root;
- Makefile/build.sh/install.sh updated to the new build paths; `.gitignore` extended
  (`cmd/*/mcp-exec`, `docker/.env`).

The old built binary was moved to `bin/mcp-exec.old` (rebuild with `make build`).

Recommendation: run `gofmt -l .`, `go vet ./...`, `go test -v ./...` on a machine with Go — a static audit does not replace a build.

---

## 10. BSD support: FreeBSD / DragonFlyBSD / OpenBSD / NetBSD (2026-11-18)

The platforms got their own files with build tags (one file per OS, so that cross-builds isolate
compilation errors): `src/platform_freebsd.go`, `src/platform_netbsd.go`,
`src/platform_openbsd.go`, `src/platform_dragonfly.go`. `platform_other.go` now
excludes BSD in its build tag and remains the fallback for the remaining systems
(aix/solaris/z/OS/plan9 — not checked, this is marked by a comment in the file itself).

The BSD row gets the full unix set instead of a fallback stub:
- **chroot**: Go performs `chroot()` in the child on Linux and BSD (the `Chroot`/`Credential`
  fields exist in `syscall.SysProcAttr` of these GOOS) — `ChrootSupported() = true`, a privilege is needed;
- **daemon**: `Setsid: true` (previously BSD used the `Setpgid` fallback);
- **kill groups**: `Kill(-pid, SIGKILL)` — the whole group is killed, as on Linux/darwin;
- **signals**: SIGHUP added to SIGINT/SIGTERM.

syslog on BSD already worked through `syslog_unix.go` (`//go:build unix`) — the documentation was
clarified: device `/dev/log` on Linux and BSD, `/var/run/log` on macOS, Windows — stub.

Export of what is minimally needed for black-box tests (call-sites in `src/server.go`
updated): `ChrootSupported`, `ProcAttr`, `DaemonAttr`, `KillChild`, `PlatformSignals`,
`SyslogLogInfo`. The behaviour of the checked OSes has not changed — only the names changed.

The matrix tests (`tests/`): a common `platform_test.go` without a build tag (helper + checks
that are safe everywhere), then one file per matrix row — `platform_bsd_test.go`
(`freebsd || netbsd || openbsd || dragonfly`), `platform_linux_test.go`,
`platform_darwin_test.go`, `platform_windows_test.go`, plus `platform_unix_test.go`
(`//go:build unix`) with the kill-group check. Split by tags because the
`Chroot`/`Setpgid`/`Setsid` fields exist only where `syscall.SysProcAttr` has them.
`go test ./...` on each OS checks its own row.

Makefile: added `build-freebsd-amd64/-arm64`, `build-netbsd-amd64`,
`build-openbsd-amd64`, `build-dragonfly-amd64` (DragonFly — amd64 only), `build-all` and
`help` updated. build.sh: the list of supported `TARGET_OS`.

README: the matrix extended to 7 columns, an init-system row and footnotes added (chroot
privileges, syslog device, systemd on Linux only); the Features/Daemon/Security sections
brought in line.

install.sh: service user creation through `pw` (FreeBSD/DragonFly),
`groupadd`/`useradd` (Linux/NetBSD) or an `adduser` hint on OpenBSD; the systemd unit is
installed on Linux only (on the other OSes rc.d recommendations are printed).

The main assumption that a build must confirm: the `Chroot` field exists in
`syscall.SysProcAttr` on all four BSDs — each OS build is isolated, so
`make build-freebsd-amd64` and friends either pass or fail only for that specific OS.

Limitation: build and tests were not run in this environment (no Go toolchain) —
`make build-all`, `go vet ./...`, `go test -v ./...` must be run on a machine with Go,
and the matrix tests — on each of the OSes.

---

## 11. Auto-detection of the build target platform (2026-11-18)

Previously the default was `linux/amd64` (hard-coded in `Makefile` and `build.sh`): a build for
another OS/architecture required an explicit specification, and `make build` on a non-Linux host
did not actually build "for the current platform", as help promises.

Now the default target platform = the one you build on:

- `Makefile`: `GOOS ?= ` and `GOARCH ?= ` default to **empty** — an empty GOOS/GOARCH is exactly
  Go's auto-detection: `go build` itself takes the platform of the machine it is run on
  (`make build` on an Apple-Silicon Mac = darwin/arm64). Explicit variants are untouched:
  `make build GOOS=windows GOARCH=arm64` and the `make build-<os>-<arch>` targets (they pass
  GOOS/GOARCH on the command line) work as before; `help` updated.
  Important: the `Makefile` **does not use** the GNU extensions `$(shell ...)` and `$(if ...)` —
  the `make` toolchain on which the project is checked in this environment does not understand
  them (`warning: Invalid character " " in variable name "shell go env GOOS ..."`) — that is why
  auto-detection is done through Go's own default, not through `go env`/`uname` inside the
  Makefile.
- `build.sh`: `TARGET_OS`/`TARGET_ARCH` default to the values from `detect_goos()`/`detect_goarch()`
  (first `go env GOOS`/`go env GOARCH`, if there is no Go toolchain — `uname -s`/`uname -m` with
  mapping to Go names: Darwin→darwin, x86_64→amd64, aarch64→arm64, i386→386,
  MINGW/MSYS/CYGWIN→windows); an explicit `TARGET_OS=freebsd TARGET_ARCH=amd64 ./build.sh`
  still wins, a note about auto-detection is printed.
- `docker/Dockerfile`: instead of a hard `GOARCH=amd64` — `ARG TARGETOS` (default `linux`) and
  `ARG TARGETARCH`: BuildKit substitutes the target platform values for them, and if they are
  absent — fallback to `go env` of the builder container itself (= the host architecture), so a
  native build on an arm64 machine is no longer cross-compiled into amd64. `TARGETOS` stays
  `linux` by default: the image lives in a linux container, no base-image for another OS is
  picked here.
- `docker/docker-compose.yml`: a comment that `TARGETOS`/`TARGETARCH` are auto-detected
  (BuildKit passes them itself); the args themselves are not added, so as not to pin a default.
- `install.sh` already detected the OS through `uname -s` (the systemd unit is installed on Linux
  only) — unchanged, the build there is native (`go build` without GOOS/GOARCH).
- `README.md`: the build sections (Makefile/build.sh/Docker) brought in line.

Check: `make build` in this environment passed **without warnings** (it used to warn on
`$(shell ...)`/`$(if ...)`) — but the Go toolchain itself is not available here, so the actual
build for each OS (and `make build-all`, `go vet ./...`, `go test -v ./...`) must be run on a
machine with Go; ideally on at least one non-Linux host, to make sure the default really gives
darwin/arm64 (or a BSD row).

---

## 12. stdout/stderr are always returned (2026-11-20)

Previously the `--- STDERR ---` block in the result text was printed only when the stderr buffer
was non-empty: stderr reached the client "on facts" — only if there was something to print, and
visually it looked like "stderr is shown together with the error code". The client could not tell
"there was nothing in stderr" from "stderr was lost", and warnings/diagnostics that utilities
write to stderr on a successful run were lost. Now the stdout/stderr buffers are collected on any
run and both sections are part of the result regardless of the exit code.

Changed:

- `src/server.go` — `FormatResult`: the `--- STDERR ---` section is printed always (empty if the
  process wrote nothing to stderr); the comments in `executeCommand`/`executeShell`/
  `runWithTimeout` pin this as a contract. In `ExecResult` the `stdout`/`stderr` fields have
  json tags without `omitempty`, both strings are always present in the structure.
- `isError` stays `false`: for an exec server a non-zero exit code is a normal result, not a
  transport error (as was documented before).
- `tests/main_test.go` — two new tests:
  `TestFormatResultStderrWhateverExitCode` (exit code 0 with stderr, non-zero exit code with
  stderr, exit code 0 with empty stderr — the section is present in all three cases) and
  `TestStderrReturnedOnSuccessfulRun` (end-to-end through `tools/call`: the command exits 0 and
  writes to stderr — the output must be returned; the test skips if `/bin/sh` is absent — Windows).
- `README.md` — the Response Format section and the `exec`/`shell` tool descriptions: the
  behaviour is documented.

Build and tests were not run in this environment (no Go toolchain) — `gofmt -l .`,
`go vet ./...`, `go test -v ./...` must be run on a machine with Go.

---

## 13. The `edit_file` tool (2026-11-21)

The standard `edit_file` tool was added — line-based editing of a text file, with the
wording/schema/behaviour of the reference MCP filesystem server: `path` + `edits` +
`dryRun`. Each edit replaces an exact sequence of lines with new content, the response is a
git-style diff, `dryRun` shows the diff without writing the file.

Added/changed:

- `src/edit_file.go` (new, stdlib only): the tool definition `editFileTool` (schema
  `path`/`edits`/`dryRun`, `context` — an optional anchor) and the annotations `toolHints`
  (protocol `2025-06-18`), the handler `ExecuteEditFile`, line-based edits `ApplyEdits`,
  git diff `GenerateGitDiff`, path resolution `ResolveEditPath` (plus internal
  `realpathNearest`/`insideRoot`/`displayPath`/`readTextFile`/`writeTextFileAtomic`/
  `splitLines`/`joinLines`/`validateEdits`/`wholeNumber`/`diffLines`/`groupHunks`);
- `src/server.go`: `edit_file` in `tools/list`; a new branch in `handleToolsCall`; a new
  `case *EditResult` in `FormatResult`; helper `toolError`; config fields `AllowedDirs` and
  `MaxFileSize` (default `DefaultMaxFileSize`), INI keys `allowed_dirs`/`max_file_size` and env
  variables `MCP_EXEC_ALLOWED_DIRS`/`MCP_EXEC_MAX_FILE_SIZE`; field `Annotations` added to `Tool`
  (structure `ToolAnnotations`) — the `2025-06-18` annotations are now explicitly spelled out for
  `exec`/`shell` as well;
- `tests/edit_file_test.go` — 15 tests: tool schema (required `path`+`edits`, `dryRun`, required
  edit fields, annotations), edit + git-diff end-to-end, `dryRun` does not write the file, edits
  work bottom-up regardless of order, empty `text` deletes the range, diff header format and
  `\ No newline at end of file`, allowed_dirs restriction (+ symlink out, + fallback to chroot, +
  relative path), `-32602` error table (range, fractional number, context, empty edits/`path`,
  overlap), missing file = tool result with `isError:true`, `No modification`, CRLF preservation,
  file permissions preserved (skip on Windows), `max_file_size` limit;
- `mcp-exec.ini`, `README.md` — new keys/variables, tool description, a note about `isError:true`,
  the `edit_file` row in the platform matrix, a Security item.

Decisions taken (documented in README):

- edits are validated against the **original** file content and applied from the bottom up
  (`sort.SliceStable` by `lineStart` descending), so the order of edits in one call does not
  matter; overlapping ranges are forbidden — their result would depend on the order;
- the line number is an integer: `json.Number` field + `wholeNumber`, the schema declares `number`
  as in the reference; a fractional number → `-32602`;
- the git standard: 3 lines of context, `@@ -old +new @@` with counters, 71 "=" in the header, the
  marker `\ No newline at end of file`; the LCS table stays under the `maxDiffCells` ceiling, above
  it — one whole-file hunk (the diff stays correct, just not minimal);
- inside an allowed-root the diff header is a path relative to the root (as the reference server
  renders it), so the absolute host structure does not leak into the response;
- `context` is an anchor: the given line of the original must contain the given text, otherwise the
  call is rejected rather than rewriting the wrong lines;
- the file is re-read right before writing (a parallel writer will not be silently overwritten);
  the write is a temporary file in the same directory + rename, permissions are preserved
  (`os.Chmod` before rename; `Sync` before close);
- a CRLF file stays CRLF (the dominant style is preserved), mixed line endings are normalized to
  the dominant one — as an editor does on save;
- `allowed_dirs` empty → root = `chroot_path` (chroot() applies only to child processes, the server
  itself lives on the host filesystem); both empty → no confinement, like `exec`/`shell`
  (RCE-by-design, section 8);
- tool-execution errors follow the standard: a result with `isError:true`; `-32602` — bad data only
  (bad input); `exec`/`shell` keep their custom code `-32000`.

Limitation: build and tests were not run in this environment (no Go toolchain) — `gofmt -l .`,
`go vet ./...`, `go test -v ./...` must be run on a machine with Go. The comment on the first block
of `Config` fields is aligned "by eye" — gofmt will move the comments to its own column; that is
the only expected gofmt change.

---

## 14. A process start failure is returned through MCP (2026-11-22)

Previously `runWithTimeout` only logged such an error and returned `result, nil`, so the client
got an empty report "Command executed … Exit code: 0" — a start that never happened looked like a
success, and the error details could be seen only in the server log. Two cases from the log:
`exec: "ls -la": executable file not found in $PATH` (the whole command line was passed as
`command`, the `PATH` lookup did not find the file) and `fork/exec ls -la /tmp/mcp-exec:
no such file or directory` (a path with a separator — Go does not do a `PATH` lookup, `Start()`
fails with ENOENT).

Now such an error is both logged and returned to the caller: `runWithTimeout` returns it, and
`handleToolsCall` already knew how to render exec/shell failures as JSON-RPC `-32000` (`message`
— the "Execution error" category, `data` — the process error text); the same works on the stdio
transport, on HTTP (the error as the response body, status 200) and on the unix socket.

The boundary has not changed: a command that started and exited with a non-zero code is a normal
result (`isError: false`, the usual report), as was documented in the README. Only start failures
become errors: the program is not found in `PATH`, the given path does not exist, the working
directory is missing, the shell is not found. A timeout is still a report with `Exit code: -1` —
its behaviour was not changed.

Changed:

- `src/server.go` — `runWithTimeout`: non-`ExitError` start errors are logged and returned
  (`return nil, err`); the comment inside the block itself and the function's doc line pin the
  boundary;
- `tests/main_test.go` — four new tests and the helper `callExecTool`:
  `TestExecToolStartFailureIsReturned` (program not in `PATH`, the whole command line as the
  program, path does not exist, missing shell — every case must arrive as `-32000` and name the
  file that could not be started), `TestExecToolNonZeroExitIsStillNotAnError` (the opposite
  boundary: exit code 7 is a result, not an error), `TestExecToolStartFailureIsAlsoLogged` (the
  log still gets the error; the test drives the logger through `log_mode=file`/`SetupLogger` — the
  existing exported API, nothing was added to the package for the test),
  `TestHandleMCPExecStartFailureOverHTTP` (the same thing through the transport: JSON-RPC error as
  the response body, HTTP 200);
- `README.md` — Response Format and the `exec`/`shell` tool descriptions: a process that did not
  start is a `-32000` error, not an empty report; the log and the response are not alternatives to
  each other.

Build and tests were run on this machine (go1.27.4, freebsd/amd64): `go build ./...` and
`go vet ./...` — no remarks, `go test ./...` — all tests green; `gofmt -l` still suggests only the
known change from section 13 (the comments in `Config`) — the new code is formatted. Checked
end-to-end through the stdio transport on the same two requests that were in the log: the answer
is an `error` with code `-32000`, the log line stayed.

Worth noting as a deliberate effect: previously a "failed" start at the transport level looked
like a successful `result` with `isError:false`, now it is a JSON-RPC error — a client that simply
read the response stdout will see `error` instead of `result`. For an exec server this is exactly
the right channel: "could not start what was asked" ≠ "did it and returned with a code".

---

## 15. Per-tool switches: the `[tools]` section (2026-11-22)

Every tool can now be switched on or off in a new `[tools]` section — the key is the
tool name, the value is parsed like the other INI booleans (`true/1/yes/on` are on,
anything else is off). A tool that is switched off is not announced through
`tools/list`, and a direct `tools/call` does not reach its handler either — it is
refused the way an unknown tool is (`-32602`). A tool the section does not name keeps
the default — enabled — so adding the section changes nothing until a tool is
explicitly switched off; `MCP_EXEC_DISABLED_TOOLS` (comma-separated names to switch
off) is the env override, in the same precedence order the other options use.

The handlers now read one registry: `toolRegistry` pairs every defined tool with its
handler, so the definition, what `tools/list` announces, what `tools/call` dispatches
to and the switches all read the same place. The `exec`/`shell` definitions moved out
of `handleListTools` into `execTool()`/`shellTool()`, mirroring `editFileTool()` in
`edit_file.go`; the switch dispatch became a registry lookup, which is also where the
unknown-tool check lives now — with the lookup deciding reachability, the old `switch`
`default` had nothing left to guard. `initialize` still answers `listChanged: false`:
the switches are read while the config loads, before serving, so the announced list
never changes under a running server.

Changed:

- `src/server.go` — `Tools map[string]bool` in `Config` (its own block, so the gofmt
  situation of section 13 is unchanged), the `tools` case in `setConfigValue`, the
  `MCP_EXEC_DISABLED_TOOLS` override in `LoadConfig`, `ToolEnabled`, `toolRegistry`
  with the lookup, `handleListTools`/`handleToolsCall` rebuilt on top of the registry;
- `tests/tools_config_test.go` — six new tests: the `[tools]` section parsing
  (on/off, the defaults, unknown names), `tools/list` without a disabled tool,
  `tools/list` empty when everything is off (the empty announcement marshals as `[]`,
  not `null`), a disabled tool refused in `tools/call` (the handler must not run —
  checked through a `touch` marker that stays missing), the env override beating the
  section, and the positive control: the neighbours of a disabled tool stay announced
  and reachable;
- `README.md` — the `[tools]` section in the configuration example, the env table row,
  the feature bullet, the `Available Tools` intro;
- `mcp-exec.ini`, `mcp-exec.ini.wrk`, `mcp-exec.default` — the new section and the new
  env var, all left at their defaults.

Build and tests were run on this machine (go1.27.4, freebsd/amd64): `go build ./...`,
`go vet ./...` — no remarks; `go test ./...` — all tests green; `gofmt -l` — still only
`src/server.go`, the known Config-comment change from section 13, its diff is unchanged
and the new code is formatted. Checked end-to-end through the stdio transport with
`shell` switched off: `tools/list` announces `exec` and `edit_file`, a direct call to
`shell` is refused, `exec` runs as before.

---

## 16. `initialize` reports the host it runs on (2026-11-24)

`serverInfo` grew two fields, and its `name` stopped being a literal: `name` is now
`[server] name`, the hostname of the machine when that is empty; the `title` is
`MCP Exec Server (name)`; the `description` says what the server does and names the
host it runs on. All three carry one value, and it is the value the operator set —
`MCP_EXEC_NAME` is the env override for the section, in the same precedence order the
other options use (`--config` flag > env > file > defaults).

The name is validated as it is read, in `setConfigValue` for the section and in
`LoadConfig` for the env var: it must be a DNS name — letters, digits and hyphens
(DNS is case-insensitive, upper case is taken as it comes), dot-separated labels, a
label never starting or ending with a hyphen, 63 characters per label, 255 in all. A
trailing dot — the root label of an FQDN — is out, RFC 1123 §2.1 does not allow it in
a host name, and so is the underscore: `hostname(1)` on the BSDs takes it, DNS does
not. Digits at the start of a label are in — the same RFC, §6.1.3, admits the
all-numeric names that were already in use. `setConfigValue` learned to refuse a value
for the first time, it returns an error now and `LoadConfig` passes it on, so a name
the operator mis-typed stops the startup instead of reaching a client as a string
nothing can use; this is the first setting in the project whose value can stop the
startup, and the numeric keys keep their long-standing "unparseable keeps the default"
behaviour untouched.

A hostname that cannot be read gets a generated name rather than a fixed one: the name
of the service and 8 characters from `crypto/rand`, settled once so that a machine
without a hostname still answers one name to every client, and several such servers
tell themselves apart. A hostname that is read is reported as it is, validation not
applied to it — the machine is called what it is called, and a BSD box whose hostname
has an underscore is not a startup error.

`title` and `description` are the fields a client shows a human; a client that only
knows `name` and `version` ignores them, so the answer stays valid for it. The name
itself is no longer a fixed string, and that is the point — a machine running several
of these servers, or one whose real name is worth showing, can now say so in the
answer. It also means anything keyed on the name sees a different server on a
different machine, which is exactly what "the name is the machine" implies.

Nothing changes for what already reported the right thing: the docker container's
hostname is `mcp-exec` (`container_name` in `docker/docker-compose.yml`), so it answers
exactly as it did before.

Changed:

- `src/server.go` — `Name string` in `Config` (its own line, so the gofmt situation of
  section 13 is unchanged), `setConfigValue` returning an error and the `name` case
  using it, the `MCP_EXEC_NAME` override in `LoadConfig` validating the same way,
  `ValidDNSName`, `ServerName()` next to `ParseBool` — the section value, the hostname,
  or `RandomServerName()` behind a `sync.OnceValue` so the answer does not change from
  request to request — `handleInitialize` resolving it once so all three fields agree,
  and the startup log line;
- `tests/server_info_test.go` — eight tests: the name from the section, the fallback to
  the hostname, the env override beating the section, `ValidDNSName` over accepted and
  refused shapes, `RandomServerName` (a DNS name, two generations not the same name),
  the section refusing an underscore, the env override refusing a trailing hyphen, and
  `Run` refusing to start on both — `LoadConfig` is the first thing `Run` does, so the
  test returns before the daemon, the logger and any transport listening; each name
  test reads the answer back through the marshaled `initialize` response, the way a
  client does;
- `README.md` — `name` in the `[server]` example, the env table row, and what
  `initialize` reports, under `Supported methods`;
- `mcp-exec.ini`, `mcp-exec.default` — the new key and the new env var, both left at
  their defaults.

Build and tests were run on this machine (go1.27.4, freebsd/amd64): `go build ./...`,
`go vet ./...` — no remarks; `go test ./...` — all tests green; `gofmt -l` — still only
`src/server.go`, the known Config-comment change from section 13, its diff is unchanged
and the new code is formatted. `go vet` cross-target for windows/amd64, darwin/arm64 and
linux/amd64 — no remarks; `crypto/rand` is stdlib and `sync.OnceValue` is exactly the
1.21 floor `go.mod` asks for. `-race` cannot run on this box, which has no cgo toolchain
— it is why the generated name is settled behind `sync.OnceValue` and not in a plain
variable. Checked end-to-end through the HTTP transport: the hostname,
`[server] name = srv01.example.com`, `Srv01.Example.COM` taken as it comes,
`MCP_EXEC_NAME` beating the section — name, title and description agree in every answer
and the startup log shows the same value; `bad_name`, `MCP_EXEC_NAME=bad_name`,
`-leading` and `host.example.com.` each stop the startup, the message naming the setting
it refused, exit code 1.
