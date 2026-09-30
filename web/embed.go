// Package web exposes the built UI (web/dist) for embedding into lucidd.
package web

import "embed"

// Dist holds the Vite build output. Only .gitkeep is committed; run
// `npm run build` in this directory to populate it.
//
//go:embed all:dist
var Dist embed.FS
