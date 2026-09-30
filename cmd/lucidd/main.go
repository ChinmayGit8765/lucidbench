// Command lucidd is the Lucidbench daemon: HTTP API plus the embedded web UI.
package main

import (
	"errors"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/ChinmayGit8765/lucidbench/internal/server"
	"github.com/ChinmayGit8765/lucidbench/internal/version"
)

func main() {
	addr := os.Getenv("LUCID_ADDR")
	if addr == "" {
		addr = ":7420"
	}
	srv := &http.Server{
		Addr:              addr,
		Handler:           server.New(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Printf("lucidd %s listening on %s", version.Version, addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
