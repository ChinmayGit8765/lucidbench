package mcp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// HandlerFor serves GET /api/mcp. Config files are read on every request.
func HandlerFor(roots func() Roots) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(Read(roots()))
	})
}

// labels are the client names used in tables.
var labels = map[string]string{"claude": "Claude Code", "codex": "Codex", "grok": "Grok", "cursor": "Cursor"}

// Table renders the matrix: one row per server, one column per client. "x"
// is configured, "~" is loaded through another client's config.
func Table(m Matrix) string {
	var b strings.Builder
	if len(m.Clients) == 0 {
		return "All providers are disabled in the config.\n"
	}
	nameW, tW := len("SERVER"), len("TRANSPORT")
	for _, r := range m.Servers {
		nameW = max(nameW, len(r.Name))
		tW = max(tW, len(strings.Join(r.Transports, ",")))
	}
	fmt.Fprintf(&b, "%-*s", nameW, "SERVER")
	for _, c := range m.Clients {
		fmt.Fprintf(&b, "  %-*s", len(labels[c.Provider]), labels[c.Provider])
	}
	fmt.Fprintf(&b, "  %-*s  %s\n", tW, "TRANSPORT", "HOST")
	for _, r := range m.Servers {
		fmt.Fprintf(&b, "%-*s", nameW, r.Name)
		for _, c := range m.Clients {
			cell := ""
			switch {
			case contains(r.Providers, c.Provider):
				cell = "x"
			case contains(r.Inherited, c.Provider):
				cell = "~"
			}
			fmt.Fprintf(&b, "  %-*s", len(labels[c.Provider]), cell)
		}
		host := strings.Join(r.Hosts, ",")
		if host == "" {
			host = "-"
		}
		fmt.Fprintf(&b, "  %-*s  %s\n", tW, strings.Join(r.Transports, ","), host)
	}
	if len(m.Servers) == 0 {
		b.WriteString("(no MCP servers configured)\n")
	}
	b.WriteString("\n")
	for _, c := range m.Clients {
		state := fmt.Sprintf("%d servers", len(c.Servers))
		if len(c.Servers) == 1 {
			state = "1 server"
		}
		switch {
		case c.Error != "":
			state = c.Error
		case !c.ConfigFound:
			state = "no config found"
		}
		if len(c.Inherits) > 0 {
			var from []string
			for _, p := range c.Inherits {
				from = append(from, labels[p])
			}
			state += " (+ loads " + strings.Join(from, ", ") + " servers)"
		}
		fmt.Fprintf(&b, "%-12s %-28s %s\n", labels[c.Provider], c.ConfigHint, state)
	}
	b.WriteString("\nx configured   ~ loaded through another client's config\n")
	return b.String()
}
