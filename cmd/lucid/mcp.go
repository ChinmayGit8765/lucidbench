package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/ChinmayGit8765/lucidbench/internal/mcp"
)

// runMCP implements `lucid mcp`: which MCP servers each client can reach.
// Only names, transports and hosts are read; never env, headers or tokens.
func runMCP(args []string) int {
	fs := flag.NewFlagSet("mcp", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "print JSON")
	if err := fs.Parse(args); err != nil || fs.NArg() > 0 {
		fmt.Fprintln(os.Stderr, "usage: lucid mcp [--json]")
		return 2
	}
	m := mcp.Read(mcp.FromConfig(cfg))
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(m)
		return 0
	}
	fmt.Print(mcp.Table(m))
	return 0
}
