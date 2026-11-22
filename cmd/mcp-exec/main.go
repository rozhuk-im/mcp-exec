package main

import (
	"flag"
	"fmt"
	"os"

	mcpxec "github.com/mcp-exec/src"
)

// version is stamped by -ldflags at build time (-X main.version=...)
var version = "1.0.0"

func main() {
	// Command line flags take precedence over env vars and the config file
	configPath := flag.String("config", "", "Path to INI configuration file")
	showVersion := flag.Bool("version", false, "Print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println("mcp-exec", version)
		return
	}

	if err := mcpxec.Run(version, *configPath); err != nil {
		fmt.Fprintln(os.Stderr, "mcp-exec:", err)
		os.Exit(1)
	}
}
