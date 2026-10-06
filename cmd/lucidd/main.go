// Command lucidd is the Lucidbench daemon: HTTP API plus the embedded web UI.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/ChinmayGit8765/lucidbench/internal/cluster"
	"github.com/ChinmayGit8765/lucidbench/internal/config"
	"github.com/ChinmayGit8765/lucidbench/internal/jobs"
	"github.com/ChinmayGit8765/lucidbench/internal/power"
	"github.com/ChinmayGit8765/lucidbench/internal/runner"
	"github.com/ChinmayGit8765/lucidbench/internal/server"
	"github.com/ChinmayGit8765/lucidbench/internal/usage"
	"github.com/ChinmayGit8765/lucidbench/internal/version"
)

func main() {
	cfg, warns, err := config.Load()
	for _, w := range warns {
		log.Print("warning: ", w)
	}
	if err != nil {
		log.Fatal(err)
	}
	cluster.Name = cfg.Cluster.Name
	runner.Image = cfg.Agent.Image
	addr := cfg.Server.Addr
	// Reading the CLI logs the first time takes a while on a busy machine;
	// do it now so Overview's usage tile and the Usage page open warm.
	// Summary holds the service's lock, so an early request waits for this
	// scan instead of starting a second one.
	use := usage.NewService(cfg, server.DataDir())
	go func() {
		start := time.Now()
		use.Summary(7)
		log.Printf("usage: warmed the cache in %s", time.Since(start).Round(time.Millisecond))
	}()
	// On-demand infrastructure: a job wakes a sleeping cluster, and idle
	// on-demand things are stopped on every check.
	pwr := power.New(cfg, server.DataDir())
	jobs.EnsureCluster = pwr.EnsureCluster
	go pwr.Run(context.Background())
	srv := &http.Server{
		Addr:              addr,
		Handler:           server.NewWith(cfg, server.Deps{Usage: use, Power: pwr}),
		ReadHeaderTimeout: 10 * time.Second,
	}
	if cluster.InContainer() {
		go cluster.KeepJoined(30 * time.Second)
	}
	log.Printf("lucidd %s listening on %s", version.Version, addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
