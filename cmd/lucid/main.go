// Command lucid is the Lucidbench CLI.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ChinmayGit8765/lucidbench/internal/cluster"
	"github.com/ChinmayGit8765/lucidbench/internal/config"
	"github.com/ChinmayGit8765/lucidbench/internal/runner"
	"github.com/ChinmayGit8765/lucidbench/internal/version"
)

// loadConfig loads the user config once and applies it to the packages.
func loadConfig() {
	c, warns, err := config.Load()
	for _, w := range warns {
		fmt.Fprintln(os.Stderr, "warning:", w)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	cfg = c
	cluster.Name = c.Cluster.Name
	runner.Image = c.Agent.Image
}

// cfg is the effective config, loaded once in main for commands that need it.
var cfg = config.Default()

func daemonURL() string {
	addr := cfg.Server.Addr
	// A wildcard listen address is not a dialable host.
	for _, w := range []string{"0.0.0.0:", "[::]:"} {
		addr = strings.Replace(addr, w, ":", 1)
	}
	if strings.HasPrefix(addr, ":") {
		addr = "localhost" + addr
	}
	if !strings.Contains(addr, "://") {
		addr = "http://" + addr
	}
	return addr
}

func health() error {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(daemonURL() + "/api/health")
	if err != nil {
		return fmt.Errorf("daemon unreachable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("daemon returned %s", resp.Status)
	}
	var body struct{ Status, Version string }
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return err
	}
	fmt.Printf("status: %s\nversion: %s\n", body.Status, body.Version)
	return nil
}

func main() {
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: lucid <version|health|accounts|login|run|image|cluster|job|config>")
	}
	flag.Parse()
	if cmd := flag.Arg(0); cmd != "version" && cmd != "config" {
		loadConfig()
	}
	switch flag.Arg(0) {
	case "config":
		os.Exit(runConfig(flag.Args()[1:]))
	case "version":
		fmt.Println(version.Version)
	case "health":
		if err := health(); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	case "accounts":
		os.Exit(runAccounts(flag.Args()[1:]))
	case "login":
		os.Exit(runLogin(flag.Args()[1:]))
	case "cluster":
		runCluster(flag.Args()[1:])
	case "job":
		runJob(flag.Args()[1:])
	case "run":
		os.Exit(runCmd(flag.Args()[1:]))
	case "image":
		os.Exit(imageCmd(flag.Args()[1:]))
	default:
		flag.Usage()
		os.Exit(2)
	}
}
