package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ChinmayGit8765/lucidbench/internal/config"
)

const configUsage = "usage: lucid config <path|init [--force]|show>"

// runConfig implements `lucid config path|init|show`.
func runConfig(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, configUsage)
		return 2
	}
	switch args[0] {
	case "path":
		return configPathCmd()
	case "init":
		return configInit(args[1:])
	case "show":
		return configShow()
	}
	fmt.Fprintln(os.Stderr, configUsage)
	return 2
}

func configPathCmd() int {
	cp, err := config.ConfigPath()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	dd, err := config.DataDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	fmt.Printf("config: %s\ndata:   %s\n", cp, dd)
	return 0
}

func configInit(args []string) int {
	fs := flag.NewFlagSet("config init", flag.ContinueOnError)
	force := fs.Bool("force", false, "overwrite an existing config file")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	path, err := config.ConfigPath()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	if _, err := os.Stat(path); err == nil && !*force {
		fmt.Fprintf(os.Stderr, "error: %s already exists; use --force to overwrite\n", path)
		return 1
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	if err := os.WriteFile(path, []byte(config.Template), 0o600); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	fmt.Println("wrote", path)
	return 0
}

// configShow prints the effective config with the source of each value.
// Config holds references, never secret values, and none are resolved here.
func configShow() int {
	c, warns, err := config.Load()
	for _, w := range warns {
		fmt.Fprintln(os.Stderr, "warning:", w)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	state := "not found, using defaults"
	if c.FileFound {
		state = "loaded"
	}
	fmt.Printf("config file: %s (%s)\n\n", c.File, state)
	rows := c.Rows()
	w := len("KEY")
	v := len("VALUE")
	for _, r := range rows {
		w, v = max(w, len(r[0])), max(v, len(r[1]))
	}
	fmt.Printf("%-*s  %-*s  %s\n", w, "KEY", v, "VALUE", "SOURCE")
	for _, r := range rows {
		fmt.Printf("%-*s  %-*s  %s\n", w, r[0], v, r[1], r[2])
	}
	return 0
}
