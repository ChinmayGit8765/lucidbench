// Command lucidd is the Lucidbench daemon: HTTP API plus the embedded web UI.
package main

import (
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/ChinmayGit8765/lucidbench/internal/cluster"
	"github.com/ChinmayGit8765/lucidbench/internal/config"
	"github.com/ChinmayGit8765/lucidbench/internal/runner"
	"github.com/ChinmayGit8765/lucidbench/internal/server"
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
	srv := &http.Server{
		Addr:              addr,
		Handler:           server.New(cfg),
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
