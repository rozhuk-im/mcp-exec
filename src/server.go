package mcpxec

import (
	"bufio"
	"bytes"
	"context"
	"crypto/subtle"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// This package is the library core of mcp-exec; the binary entrypoint lives in cmd/mcp-exec.

// Config represents the application configuration
// (parsed by the manual INI reader in setConfigValue, not by struct tags)
type Config struct {
	Daemon          bool     // [server] daemon
	LogMode         string   // [logging] mode — file, syslog, stdout
	LogFile         string   // [logging] file
	SyslogTag       string   // [logging] syslog_tag
	SyslogNetwork   string   // [logging] syslog_network — tcp, udp, unix
	SyslogAddress   string   // [logging] syslog_address
	Addr            string   // [server] addr — for transport=domain this is the unix socket path
	Port            int      // [server] port
	AuthKeys        string   // [server] auth_keys — comma-separated list
	Transport       string   // [server] transport — https, http, domain, stdio
	MaxReqSize      int64    // [server] max_request_size
	MaxRespSize     int64    // [server] max_response_size
	ChrootPath      string   // [security] chroot_path — Linux and the BSDs, requires privileges
	AllowedDirs     []string // [security] allowed_dirs — roots the file tool (edit_file) is confined to, comma-separated
	MaxFileSize     int64    // [security] max_file_size — the largest file a file tool will read (default: DefaultMaxFileSize)

	Tools map[string]bool // [tools] per-tool switches — <tool name> = on/off; a tool the section does not name keeps the default (enabled)
}

// MCPRequest represents an incoming MCP request
type MCPRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// MCPResponse represents an MCP response
type MCPResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id"`
	Result  interface{} `json:"result,omitempty"`
	Error   *MCPError   `json:"error,omitempty"`
}

// MCPError represents an MCP error
type MCPError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// ExecParams represents parameters for exec tool
type ExecParams struct {
	Command string   `json:"command"`
	Args    []string `json:"args,omitempty"`
	Env     []string `json:"env,omitempty"`
	Dir     string   `json:"dir,omitempty"`
	Timeout int      `json:"timeout,omitempty"`
}

// ExecResult represents the result of command execution
type ExecResult struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exit_code"`
	Executed string `json:"executed"`
}

// ListToolsResponse represents the list of available tools
type ListToolsResponse struct {
	Tools []Tool `json:"tools"`
}

// Tool describes an MCP tool — name, description, inputSchema, plus the optional
// annotations block protocol 2025-06-18 added
type Tool struct {
	Name        string           `json:"name"`
	Description string           `json:"description"`
	InputSchema InputSchema      `json:"inputSchema"`
	Annotations *ToolAnnotations `json:"annotations,omitempty"`
}

// ToolAnnotations is the protocol-2025-06-18 hint block a client may rely on.
// A nil hint means "use the protocol default" (readOnlyHint false,
// destructiveHint true, idempotentHint false, openWorldHint true); toolHints in
// edit_file.go spells all four out.
type ToolAnnotations struct {
	Title           string `json:"title,omitempty"`
	ReadOnlyHint    *bool  `json:"readOnlyHint,omitempty"`
	DestructiveHint *bool  `json:"destructiveHint,omitempty"`
	IdempotentHint  *bool  `json:"idempotentHint,omitempty"`
	OpenWorldHint   *bool  `json:"openWorldHint,omitempty"`
}

// InputSchema describes tool input schema
type InputSchema struct {
	Type       string         `json:"type"`
	Properties map[string]any `json:"properties"`
	Required   []string       `json:"required"`
}

// Cfg is the process-wide configuration used by handlers and transport code.
var Cfg Config

var (
	logger       *log.Logger
	syslogWriter io.WriteCloser
	mu           sync.RWMutex // guards logger/syslogWriter
	appVersion   = "1.0.0"
)

const (
	DefaultPort        = 8080
	DefaultMaxReqSize  = 1024 * 1024      // 1MB
	DefaultMaxRespSize = 10 * 1024 * 1024 // 10MB
	DefaultMaxFileSize = 10 * 1024 * 1024 // 10MB — the cap for reading a file (edit_file)

	// JSON-RPC error codes (custom range -32000..-32099 is reserved for apps)
	errCodeExecution = -32000
	errCodeTooLarge  = -32001
	errCodeUnauth    = -32002
)

// Run loads configuration, sets up logging, daemonizes if needed and serves
// requests over the configured transport. configPath is the --config flag value
// (empty = fall back to MCP_EXEC_CONFIG, then ./mcp-exec.ini). It returns on
// graceful shutdown (SIGINT/SIGTERM, plus SIGHUP on unix platforms).
func Run(version, configPath string) error {
	if version != "" {
		appVersion = version
	}

	if err := LoadConfig(configPath); err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	if Cfg.ChrootPath != "" && !ChrootSupported() {
		return errors.New("chroot_path is supported only on Linux and the BSDs (FreeBSD, DragonFlyBSD, OpenBSD, NetBSD)")
	}

	// Daemonize before opening log files, so the child owns fresh descriptors
	if Cfg.Daemon {
		if err := daemonize(); err != nil {
			return err
		}
	}

	if err := SetupLogger(); err != nil {
		return fmt.Errorf("failed to setup logger: %w", err)
	}

	logPrintf("mcp-exec %s starting...", appVersion)
	logPrintf("Transport: %s", Cfg.Transport)

	// Graceful shutdown on SIGINT/SIGTERM (SIGHUP additionally on unix)
	ctx, stop := signal.NotifyContext(context.Background(), PlatformSignals()...)
	defer stop()

	switch Cfg.Transport {
	case "stdio":
		if err := runStdio(); err != nil {
			return fmt.Errorf("stdio: %w", err)
		}

	case "domain":
		socketPath := Cfg.Addr
		if socketPath == "" {
			socketPath = "/tmp/mcp-exec.sock"
		}
		logPrintf("Domain socket: %s", socketPath)
		// Remove stale socket file from previous run
		os.Remove(socketPath)
		listener, err := net.Listen("unix", socketPath)
		if err != nil {
			return fmt.Errorf("failed to listen on %s: %w", socketPath, err)
		}
		defer listener.Close()
		defer os.Remove(socketPath)
		if err := serveWithShutdown(ctx, newServer(), listener, optionalTLSConfig()); err != nil {
			return err
		}

	case "https":
		certFile := os.Getenv("TLS_CERT")
		keyFile := os.Getenv("TLS_KEY")
		if certFile == "" || keyFile == "" {
			return errors.New("https transport requires TLS_CERT and TLS_KEY")
		}
		cert, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			return fmt.Errorf("failed to load TLS certificate: %w", err)
		}
		ln, err := tcpListener()
		if err != nil {
			return err
		}
		if err := serveWithShutdown(ctx, newServer(), ln, tlsConfigFromCert(cert)); err != nil {
			return err
		}

	default: // http
		ln, err := tcpListener()
		if err != nil {
			return err
		}
		if err := serveWithShutdown(ctx, newServer(), ln, nil); err != nil {
			return err
		}
	}

	logPrintf("mcp-exec stopped")
	closeLogging()
	return nil
}

func tlsConfigFromCert(cert tls.Certificate) *tls.Config {
	return &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{cert},
	}
}

func newServer() *http.Server {
	return &http.Server{
		Addr:         fmt.Sprintf("%s:%d", Cfg.Addr, Cfg.Port),
		Handler:      newMux(),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 120 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
}

func newMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", HandleMCP)
	return mux
}

func tcpListener() (net.Listener, error) {
	addr := fmt.Sprintf("%s:%d", Cfg.Addr, Cfg.Port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("failed to listen on %s: %w", addr, err)
	}
	logPrintf("Listening on %s", addr)
	return ln, nil
}

// optionalTLSConfig returns TLS config for transports where TLS is optional
// (unix socket: TLS is off unless TLS_CERT/TLS_KEY are explicitly set)
func optionalTLSConfig() *tls.Config {
	certFile := os.Getenv("TLS_CERT")
	keyFile := os.Getenv("TLS_KEY")
	if certFile == "" || keyFile == "" {
		return nil
	}
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		// TLS is optional for domain sockets — degrade with a warning
		logPrintf("TLS certificate load failed, serving without TLS: %v", err)
		return nil
	}
	return tlsConfigFromCert(cert)
}

// serveWithShutdown serves until a shutdown signal arrives, then gracefully closes
func serveWithShutdown(ctx context.Context, srv *http.Server, ln net.Listener, tlsConfig *tls.Config) error {
	errCh := make(chan error, 1)

	go func() {
		var err error
		if tlsConfig != nil {
			srv.TLSConfig = tlsConfig
			err = srv.ServeTLS(ln, "", "")
		} else {
			err = srv.Serve(ln)
		}
		errCh <- err
	}()

	select {
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			return fmt.Errorf("server error: %w", err)
		}
		return nil
	case <-ctx.Done():
		logPrintf("Shutdown signal received, closing server...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown error: %w", err)
		}
		<-errCh // Serve returns http.ErrServerClosed
		return nil
	}
}

// LoadConfig loads the INI configuration file (if present) and applies
// MCP_EXEC_* environment variable overrides. explicitPath comes from the
// --config flag and takes precedence over MCP_EXEC_CONFIG and the default path.
func LoadConfig(explicitPath string) error {
	// Default values
	Cfg.Port = DefaultPort
	Cfg.MaxReqSize = DefaultMaxReqSize
	Cfg.MaxRespSize = DefaultMaxRespSize
	Cfg.MaxFileSize = DefaultMaxFileSize
	Cfg.Transport = "http"
	Cfg.LogMode = "stdout"
	// Cfg.Tools stays nil — every tool is enabled unless the [tools] section or the
	// env override at the end of LoadConfig says otherwise

	// Config file: flag > env > default path
	configPath := explicitPath
	if configPath == "" {
		configPath = os.Getenv("MCP_EXEC_CONFIG")
	}
	if configPath == "" {
		configPath = "mcp-exec.ini"
	}

	if _, err := os.Stat(configPath); err == nil {
		file, err := os.Open(configPath)
		if err != nil {
			return fmt.Errorf("failed to open config file: %w", err)
		}
		defer file.Close()

		scanner := bufio.NewScanner(file)
		var currentSection string

		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())

			// Skip empty lines and comments
			if line == "" || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "#") {
				continue
			}

			// Section header
			if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
				currentSection = strings.ToLower(strings.TrimSuffix(strings.TrimPrefix(line, "["), "]"))
				continue
			}

			// Key-value pair
			parts := strings.SplitN(line, "=", 2)
			if len(parts) != 2 {
				continue
			}

			key := strings.ToLower(strings.TrimSpace(parts[0]))
			value := strings.TrimSpace(parts[1])

			// Strip inline comments (" ; ") from unquoted values
			if !strings.HasPrefix(value, "\"") && !strings.HasPrefix(value, "'") {
				if idx := strings.Index(value, " ; "); idx >= 0 {
					value = strings.TrimSpace(value[:idx])
				}
			}

			// Remove quotes if present
			value = strings.Trim(value, "\"'")

			setConfigValue(currentSection, key, value)
		}

		if err := scanner.Err(); err != nil {
			return fmt.Errorf("error reading config file: %w", err)
		}
	}

	// Override with environment variables
	if addr := os.Getenv("MCP_EXEC_ADDR"); addr != "" {
		Cfg.Addr = addr
	}
	if port := os.Getenv("MCP_EXEC_PORT"); port != "" {
		if p, err := strconv.Atoi(port); err == nil {
			Cfg.Port = p
		}
	}
	if authKeys := os.Getenv("MCP_EXEC_AUTH_KEYS"); authKeys != "" {
		Cfg.AuthKeys = authKeys
	}
	if transport := os.Getenv("MCP_EXEC_TRANSPORT"); transport != "" {
		Cfg.Transport = transport
	}
	if maxReq := os.Getenv("MCP_EXEC_MAX_REQ_SIZE"); maxReq != "" {
		if s, err := strconv.ParseInt(maxReq, 10, 64); err == nil {
			Cfg.MaxReqSize = s
		}
	}
	if maxResp := os.Getenv("MCP_EXEC_MAX_RESP_SIZE"); maxResp != "" {
		if s, err := strconv.ParseInt(maxResp, 10, 64); err == nil {
			Cfg.MaxRespSize = s
		}
	}
	if chroot := os.Getenv("MCP_EXEC_CHROOT"); chroot != "" {
		Cfg.ChrootPath = chroot
	}
	if allowedDirs := os.Getenv("MCP_EXEC_ALLOWED_DIRS"); allowedDirs != "" {
		Cfg.AllowedDirs = splitAllowedDirs(allowedDirs)
	}
	if maxFile := os.Getenv("MCP_EXEC_MAX_FILE_SIZE"); maxFile != "" {
		if n, err := strconv.ParseInt(maxFile, 10, 64); err == nil {
			Cfg.MaxFileSize = n
		}
	}
	if daemon := os.Getenv("MCP_EXEC_DAEMON"); daemon != "" {
		Cfg.Daemon = ParseBool(daemon)
	}
	if logMode := os.Getenv("MCP_EXEC_LOG_MODE"); logMode != "" {
		Cfg.LogMode = logMode
	}
	if logFile := os.Getenv("MCP_EXEC_LOG_FILE"); logFile != "" {
		Cfg.LogFile = logFile
	}
	if syslogTag := os.Getenv("MCP_EXEC_SYSLOG_TAG"); syslogTag != "" {
		Cfg.SyslogTag = syslogTag
	}
	if syslogNetwork := os.Getenv("MCP_EXEC_SYSLOG_NETWORK"); syslogNetwork != "" {
		Cfg.SyslogNetwork = syslogNetwork
	}
	if syslogAddress := os.Getenv("MCP_EXEC_SYSLOG_ADDRESS"); syslogAddress != "" {
		Cfg.SyslogAddress = syslogAddress
	}
	if disabledTools := os.Getenv("MCP_EXEC_DISABLED_TOOLS"); disabledTools != "" {
		// the env override for the [tools] section: a comma-separated list of tools to switch off
		if Cfg.Tools == nil {
			Cfg.Tools = make(map[string]bool)
		}
		for _, name := range strings.Split(disabledTools, ",") {
			name = strings.ToLower(strings.TrimSpace(name))
			if name != "" {
				Cfg.Tools[name] = false
			}
		}
	}

	return nil
}

// ParseBool parses INI-style booleans (true/1/yes/on).
func ParseBool(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	return s == "true" || s == "1" || s == "yes" || s == "on"
}

func setConfigValue(section, key, value string) {
	switch section {
	case "", "server":
		switch key {
		case "addr":
			Cfg.Addr = value
		case "port":
			if p, err := strconv.Atoi(value); err == nil {
				Cfg.Port = p
			}
		case "transport":
			Cfg.Transport = value
		case "max_request_size":
			if s, err := strconv.ParseInt(value, 10, 64); err == nil {
				Cfg.MaxReqSize = s
			}
		case "max_response_size":
			if s, err := strconv.ParseInt(value, 10, 64); err == nil {
				Cfg.MaxRespSize = s
			}
		case "auth_keys":
			Cfg.AuthKeys = value
		case "daemon":
			Cfg.Daemon = ParseBool(value)
		}
	case "logging":
		switch key {
		case "mode":
			Cfg.LogMode = value
		case "file":
			Cfg.LogFile = value
		case "syslog_tag":
			Cfg.SyslogTag = value
		case "syslog_network":
			Cfg.SyslogNetwork = value
		case "syslog_address":
			Cfg.SyslogAddress = value
		}
	case "security":
		switch key {
		case "chroot_path":
			Cfg.ChrootPath = value
		case "allowed_dirs":
			Cfg.AllowedDirs = splitAllowedDirs(value)
		case "max_file_size":
			if n, err := strconv.ParseInt(value, 10, 64); err == nil {
				Cfg.MaxFileSize = n
			}
		}
	case "tools":
		if Cfg.Tools == nil {
			Cfg.Tools = make(map[string]bool)
		}
		Cfg.Tools[key] = ParseBool(value)
	}
}

// SetupLogger switches logging to the configured mode (stdout, file, syslog).
func SetupLogger() error {
	mu.Lock()
	defer mu.Unlock()

	switch Cfg.LogMode {
	case "syslog":
		tag := Cfg.SyslogTag
		if tag == "" {
			tag = "mcp-exec"
		}

		var err error
		network := Cfg.SyslogNetwork
		address := Cfg.SyslogAddress

		if network != "" && address != "" {
			syslogWriter, err = dialSyslog(network, address, SyslogLogInfo, tag)
		} else {
			syslogWriter, err = newSyslog(SyslogLogInfo, tag)
		}
		if err != nil {
			return fmt.Errorf("failed to connect to syslog: %w", err)
		}
		logger = log.New(syslogWriter, "", 0)
	case "file":
		if Cfg.LogFile == "" {
			Cfg.LogFile = "/var/log/mcp-exec.log"
		}
		f, err := os.OpenFile(Cfg.LogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return fmt.Errorf("failed to open log file: %w", err)
		}
		logger = log.New(f, "", log.LstdFlags|log.Lmicroseconds)
	default: // stdout
		logger = log.New(os.Stdout, "", log.LstdFlags|log.Lmicroseconds)
	}
	return nil
}

func logPrintf(format string, v ...interface{}) {
	mu.RLock()
	defer mu.RUnlock()
	if logger == nil {
		return
	}
	logger.Printf(format, v...)
}

func closeLogging() {
	mu.Lock()
	defer mu.Unlock()
	if syslogWriter != nil {
		syslogWriter.Close()
		syslogWriter = nil
	}
}

func daemonize() error {
	// Check if already daemonized
	if os.Getenv("MCP_EXEC_DAEMONIZED") == "1" {
		return nil
	}

	// Re-exec self in a new process; parent exits.
	// Setsid (on unix platforms) detaches the child from the controlling terminal.
	cmd := exec.Command(os.Args[0], os.Args[1:]...)
	cmd.Env = append(os.Environ(), "MCP_EXEC_DAEMONIZED=1")
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.SysProcAttr = DaemonAttr()

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("daemonize: %w", err)
	}

	// Parent exits; child continues running
	os.Exit(0)

	return nil
}

// HandleMCP processes JSON-RPC requests over HTTP (POST required).
func HandleMCP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodOptions:
		setCORS(w)
		w.WriteHeader(http.StatusNoContent)
		return
	case http.MethodPost:
		// ok
	default:
		writeJSONRPC(w, makeError(nil, errCodeExecution, "Method not allowed", "Only POST is accepted"), http.StatusMethodNotAllowed)
		return
	}

	// Check authorization
	if err := CheckAuth(r); err != nil {
		writeJSONRPC(w, makeError(nil, errCodeUnauth, "Unauthorized", err.Error()), http.StatusUnauthorized)
		return
	}

	// Read request body with hard limit (oversize => explicit 413, not a parse error)
	r.Body = http.MaxBytesReader(w, r.Body, Cfg.MaxReqSize)
	req, err := DecodeRequest(r.Body)
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeJSONRPC(w, makeError(nil, errCodeTooLarge, "Request too large", "Request exceeds maximum size"), http.StatusRequestEntityTooLarge)
			return
		}
		writeJSONRPC(w, makeError(nil, -32700, "Parse error", "Invalid JSON"), http.StatusOK)
		return
	}

	// Handle the request
	resp := HandleRequest(req)

	// JSON-RPC notifications must not receive a response
	if resp == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// Limit response size
	respBytes, err := json.Marshal(resp)
	if err != nil {
		writeJSONRPC(w, makeError(resp.ID, -32603, "Internal error", "Failed to marshal response"), http.StatusOK)
		return
	}

	if int64(len(respBytes)) > Cfg.MaxRespSize {
		writeJSONRPC(w, makeError(resp.ID, errCodeTooLarge, "Response too large", "Response exceeds maximum size"), http.StatusOK)
		return
	}

	setCORS(w)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(respBytes); err != nil {
		logPrintf("HTTP write error: %v", err)
	}
}

func setCORS(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-API-Key")
}

// CheckAuth validates Bearer / X-API-Key credentials using constant-time comparison.
func CheckAuth(r *http.Request) error {
	if Cfg.AuthKeys == "" {
		return nil // No auth required
	}

	keys := strings.Split(Cfg.AuthKeys, ",")

	// Check Authorization header
	authHeader := r.Header.Get("Authorization")
	if authHeader != "" {
		got := strings.TrimPrefix(authHeader, "Bearer ")
		for _, key := range keys {
			key = strings.TrimSpace(key)
			if key != "" && subtle.ConstantTimeCompare([]byte(key), []byte(got)) == 1 {
				return nil
			}
		}
	}

	// Check X-API-Key header
	apiKey := r.Header.Get("X-API-Key")
	if apiKey != "" {
		for _, key := range keys {
			key = strings.TrimSpace(key)
			if key != "" && subtle.ConstantTimeCompare([]byte(key), []byte(apiKey)) == 1 {
				return nil
			}
		}
	}

	return errors.New("unauthorized")
}

// DecodeRequest parses a JSON-RPC request. UseNumber preserves large integer IDs.
func DecodeRequest(r io.Reader) (*MCPRequest, error) {
	d := json.NewDecoder(r)
	d.UseNumber()
	var req MCPRequest
	if err := d.Decode(&req); err != nil {
		return nil, err
	}
	return &req, nil
}

// HandleRequest returns nil for notifications (JSON-RPC: no response expected).
func HandleRequest(req *MCPRequest) *MCPResponse {
	switch req.Method {
	case "initialize":
		return handleInitialize(req)
	case "notifications/initialized", "notifications/cancelled", "initialized":
		// Notification — no response
		return nil
	case "tools/list":
		return handleListTools(req)
	case "tools/call":
		return handleToolsCall(req)
	case "resources/list", "resources/templates/list":
		return &MCPResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  map[string]interface{}{"resources": []interface{}{}},
		}
	case "prompts/list":
		return &MCPResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  map[string]interface{}{"prompts": []interface{}{}},
		}
	case "ping":
		return &MCPResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  struct{}{},
		}
	default:
		return makeError(req.ID, -32601, "Method not found", "Unknown method: "+req.Method)
	}
}

func handleInitialize(req *MCPRequest) *MCPResponse {
	return &MCPResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]interface{}{
			"protocolVersion": "2025-06-18",
			"capabilities": map[string]interface{}{
				"tools": map[string]interface{}{
					"listChanged": false,
				},
			},
			"serverInfo": map[string]interface{}{
				"name":    "mcp-exec",
				"version": appVersion,
			},
		},
	}
}

// execTool and shellTool are the tool definitions, worded the way the standard tools
// are worded; they mirror editFileTool() in edit_file.go, so every tool the server
// defines is a function of its own.
func execTool() Tool {
	return Tool{
		Name:        "exec",
		Description: "Execute a program directly with arguments",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]any{
				"command": map[string]any{
					"type":        "string",
					"description": "Program to execute",
				},
				"args": map[string]any{
					"type":        "array",
					"description": "Program arguments",
					"items":       map[string]any{"type": "string"},
				},
				"env": map[string]any{
					"type":        "array",
					"description": "Environment variables as KEY=VALUE pairs",
					"items":       map[string]any{"type": "string"},
				},
				"dir": map[string]any{
					"type":        "string",
					"description": "Working directory for the command",
				},
				"timeout": map[string]any{
					"type":        "integer",
					"description": "Timeout in seconds",
				},
			},
			Required: []string{"command"},
		},
		Annotations: toolHints(false, true, false, true),
	}
}

func shellTool() Tool {
	return Tool{
		Name:        "shell",
		Description: "Execute a command through shell interpreter",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]any{
				"command": map[string]any{
					"type":        "string",
					"description": "Shell command to execute",
				},
				"shell": map[string]any{
					"type":        "string",
					"description": "Shell to use (default: /bin/sh)",
				},
				"env": map[string]any{
					"type":        "array",
					"description": "Environment variables",
					"items":       map[string]any{"type": "string"},
				},
				"dir": map[string]any{
					"type":        "string",
					"description": "Working directory",
				},
				"timeout": map[string]any{
					"type":        "integer",
					"description": "Timeout in seconds",
				},
			},
			Required: []string{"command"},
		},
		Annotations: toolHints(false, true, false, true),
	}
}

// toolEntry pairs a tool definition with its handler. The registry below is the one
// place where the server's tools live: the definition, what tools/list announces,
// what tools/call dispatches to and the [tools] switches all read it.
type toolEntry struct {
	def     Tool
	handler func(json.RawMessage) (interface{}, error)
}

// toolRegistry lists every tool the server defines, in announcement order.
var toolRegistry = []toolEntry{
	{execTool(), executeCommand},
	{shellTool(), executeShell},
	{editFileTool(), ExecuteEditFile},
}

// lookupTool finds a tool in the registry by name. ok=false means the name is not a
// tool of this server at all — the [tools] switches apply to defined tools only, an
// undefined name stays the plain unknown tool.
func lookupTool(name string) (toolEntry, bool) {
	for _, entry := range toolRegistry {
		if entry.def.Name == name {
			return entry, true
		}
	}
	return toolEntry{}, false
}

// ToolEnabled reports whether a tool is switched on in the [tools] section (the key is
// the tool name, the value is parsed like the other INI booleans). A tool the section
// does not name keeps the default — enabled — so adding the section changes nothing
// until a tool is explicitly switched off. The map is written while the config loads,
// before serving, and is read-only while handlers run.
func ToolEnabled(name string) bool {
	if enabled, ok := Cfg.Tools[name]; ok {
		return enabled
	}
	return true
}

func handleListTools(req *MCPRequest) *MCPResponse {
	// the [tools] section decides which tools are announced; a tool that is switched
	// off is not in the list, and tools/call will refuse it too (see handleToolsCall)
	tools := ListToolsResponse{Tools: []Tool{}}
	for _, entry := range toolRegistry {
		if ToolEnabled(entry.def.Name) {
			tools.Tools = append(tools.Tools, entry.def)
		}
	}

	return &MCPResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  tools,
	}
}

func handleToolsCall(req *MCPRequest) *MCPResponse {
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return makeError(req.ID, -32602, "Invalid params", "Failed to parse parameters")
	}

	// the [tools] section decides what is reachable: a tool that is switched off is
	// not announced through tools/list, and a direct call does not reach its handler
	// either — it is refused the way an unknown tool is
	entry, defined := lookupTool(params.Name)
	if !defined {
		return makeError(req.ID, -32602, "Invalid params", "Unknown tool: "+params.Name)
	}
	if !ToolEnabled(params.Name) {
		return makeError(req.ID, -32602, "Invalid params", "Tool disabled: "+params.Name)
	}

	result, execErr := entry.handler(params.Arguments)
	if execErr != nil {
		if entry.def.Name == "edit_file" {
			// the file tool follows the standard: bad input is -32602, a failure to
			// do the job is a tool result with isError:true (see toolError)
			return toolError(req.ID, execErr)
		}
		return makeError(req.ID, errCodeExecution, "Execution error", execErr.Error())
	}

	return &MCPResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]interface{}{
			"content": []map[string]interface{}{
				{
					"type": "text",
					"text": FormatResult(result),
				},
			},
			"isError": false,
		},
	}
}

// toolError renders a failed tools/call for the file tool. Input problems
// (invalid parameters, a path outside the allowed roots) keep the JSON-RPC code
// -32602, and — exactly as the reference server throws it — the explanation of
// the problem goes into the message a client reads, with the JSON-RPC category
// kept as the additional data; a failure to actually do the job is reported the
// way the protocol prescribes for tools — a result with isError:true, not an
// error object.
func toolError(id interface{}, err error) *MCPResponse {
	var ip *invalidParamsError
	if errors.As(err, &ip) {
		return makeError(id, -32602, ip.Error(), "Invalid params")
	}

	return &MCPResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result: map[string]interface{}{
			"content": []map[string]interface{}{
				{
					"type": "text",
					"text": err.Error(),
				},
			},
			"isError": true,
		},
	}
}

// ResolveDir computes the working directory, guarding against path traversal.
// With chroot the path is resolved INSIDE the new root (Go performs chroot()
// in the child before applying cmd.Dir). Without chroot the path is a host path.
func ResolveDir(dir string) string {
	if dir == "" {
		return ""
	}
	// Prefixing with "/" makes Clean collapse ".." without escaping the root
	clean := filepath.Clean("/" + dir)
	if Cfg.ChrootPath != "" {
		return clean
	}
	return dir
}

func applyProcSettings(cmd *exec.Cmd, dir string) {
	if dir != "" {
		cmd.Dir = ResolveDir(dir)
	}
	// Setpgid lets us kill the whole process group on timeout
	cmd.SysProcAttr = ProcAttr(Cfg.ChrootPath, true)
}

func executeCommand(params json.RawMessage) (interface{}, error) {
	var p ExecParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid parameters: %w", err)
	}

	if p.Command == "" {
		return nil, errors.New("command is required")
	}

	// Build command
	cmd := exec.Command(p.Command, p.Args...)

	// Set environment
	if len(p.Env) > 0 {
		cmd.Env = p.Env
	} else {
		cmd.Env = os.Environ()
	}

	// Setup output buffers — stdout and stderr are always captured, whatever
	// the exit code turns out to be, so both are always reportable
	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	// Working directory, chroot, process group
	applyProcSettings(cmd, p.Dir)

	return runWithTimeout(cmd, p.Command, p.Timeout, &stdoutBuf, &stderrBuf)
}

func executeShell(params json.RawMessage) (interface{}, error) {
	var p struct {
		Command string   `json:"command"`
		Shell   string   `json:"shell"`
		Env     []string `json:"env"`
		Dir     string   `json:"dir"`
		Timeout int      `json:"timeout"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid parameters: %w", err)
	}

	if p.Command == "" {
		return nil, errors.New("command is required")
	}

	if p.Shell == "" {
		p.Shell = "/bin/sh"
	}

	cmd := exec.Command(p.Shell, "-c", p.Command)

	// Set environment
	if len(p.Env) > 0 {
		cmd.Env = p.Env
	} else {
		cmd.Env = os.Environ()
	}

	// Setup output buffers — stdout and stderr are always captured, whatever
	// the exit code turns out to be, so both are always reportable
	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	// Working directory, chroot, process group
	applyProcSettings(cmd, p.Dir)

	return runWithTimeout(cmd, p.Command, p.Timeout, &stdoutBuf, &stderrBuf)
}

// runWithTimeout runs the command and waits for completion or timeout. A command
// that ran is reported whatever its exit code was; a command that could not be
// started (program not found, missing path/working directory) comes back as an error.
func runWithTimeout(cmd *exec.Cmd, executed string, timeoutSec int, stdoutBuf, stderrBuf *bytes.Buffer) (interface{}, error) {
	var timeout time.Duration
	if timeoutSec > 0 {
		timeout = time.Duration(timeoutSec) * time.Second
	} else {
		timeout = 60 * time.Second
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Run()
	}()

	select {
	case err := <-done:
		// stdout/stderr are reported whatever the exit code was — a successful
		// run that wrote to stderr is not an error, and stderr must not be lost
		result := ExecResult{
			Stdout:   stdoutBuf.String(),
			Stderr:   stderrBuf.String(),
			Executed: executed,
		}
		if cmd.ProcessState != nil {
			result.ExitCode = cmd.ProcessState.ExitCode()
		}
		if err != nil {
			var exitErr *exec.ExitError
			// Non-zero exit code is a normal outcome — it is reported as the usual
			// process report. Anything else is a real failure: the program is not in
			// $PATH, the given path does not exist, the working directory is missing,
			// the process never started. Such an error is logged AND returned — a
			// failure that stops at the log never reaches the client, which would
			// otherwise read an empty "exit code 0" report for a run that happened
			// nowhere (the caller sees it as the JSON-RPC error -32000 in handleToolsCall)
			if !errors.As(err, &exitErr) {
				logPrintf("exec error for %q: %v", executed, err)
				return nil, err
			}
		}
		return result, nil
	case <-time.After(timeout):
		if cmd.Process != nil {
			if kerr := KillChild(cmd); kerr != nil {
				logPrintf("kill error for %q: %v", executed, kerr)
			}
		}
		// Give the child a moment to die so buffers are complete before we read them
		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
		return ExecResult{
			Stdout:   stdoutBuf.String(),
			Stderr:   stderrBuf.String(),
			ExitCode: -1,
			Executed: executed + " (timeout)",
		}, nil
	}
}

// FormatResult renders an execution result as the MCP text content block.
// Both stdout and stderr are always part of the result — stderr is reported
// whatever the exit code was, so a successful run that wrote warnings or
// diagnostics to stderr still gets them back to the caller.
func FormatResult(result interface{}) string {
	switch r := result.(type) {
	case ExecResult:
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("Command executed: %s\n", r.Executed))
		sb.WriteString(fmt.Sprintf("Exit code: %d\n", r.ExitCode))
		if r.Stdout != "" {
			sb.WriteString(fmt.Sprintf("\n--- STDOUT ---\n%s", r.Stdout))
		}
		// The section is always emitted, even when stderr is empty, so the caller
		// can tell "nothing on stderr" from "stderr was dropped"
		sb.WriteString("\n--- STDERR ---\n")
		sb.WriteString(r.Stderr)
		return sb.String()
	case *EditResult:
		// edit_file returns exactly the git-style diff the standard tool puts into its
		// text block ("No modification" when the edits turned out to be a no-op)
		return r.Diff
	default:
		b, _ := json.MarshalIndent(result, "", "  ")
		return string(b)
	}
}

func makeError(id interface{}, code int, message string, data interface{}) *MCPResponse {
	return &MCPResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error: &MCPError{
			Code:    code,
			Message: message,
			Data:    data,
		},
	}
}

// writeJSONRPC sends a JSON-RPC object as an HTTP response with the given status
func writeJSONRPC(w http.ResponseWriter, resp *MCPResponse, status int) {
	respBytes, err := json.Marshal(resp)
	if err != nil {
		logPrintf("marshal error: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	setCORS(w)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if _, err := w.Write(respBytes); err != nil {
		logPrintf("HTTP write error: %v", err)
	}
}

func runStdio() error {
	reader := bufio.NewReader(os.Stdin)

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Enforce request size limit
		if int64(len(line)) > Cfg.MaxReqSize {
			writeLine(makeError(nil, errCodeTooLarge, "Request too large", "Request exceeds maximum size"))
			continue
		}

		// Parse JSON-RPC request
		req, err := DecodeRequest(bytes.NewBufferString(line))
		if err != nil {
			writeLine(makeError(nil, -32700, "Parse error", "Invalid JSON"))
			continue
		}

		// Handle the request
		resp := HandleRequest(req)

		// Notifications must not receive a response
		if resp == nil {
			continue
		}

		writeLine(resp)
	}
}

func writeLine(resp *MCPResponse) {
	respBytes, err := json.Marshal(resp)
	if err != nil {
		logPrintf("marshal error: %v", err)
		return
	}
	// Enforce response size limit
	if int64(len(respBytes)) > Cfg.MaxRespSize {
		alt := makeError(resp.ID, errCodeTooLarge, "Response too large", "Response exceeds maximum size")
		b, err := json.Marshal(alt)
		if err != nil {
			return
		}
		respBytes = b
	}
	// stdout is the transport channel in stdio mode, never the logger
	fmt.Fprintln(os.Stdout, string(respBytes))
}
