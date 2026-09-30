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

	"github.com/ChinmayGit8765/lucidbench/internal/version"
)

func daemonURL() string {
	addr := os.Getenv("LUCID_ADDR")
	if addr == "" {
		addr = ":7420"
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
		fmt.Fprintln(os.Stderr, "usage: lucid <version|health|accounts|login|cluster|job>")
	}
	flag.Parse()
	switch flag.Arg(0) {
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
	default:
		flag.Usage()
		os.Exit(2)
	}
}
