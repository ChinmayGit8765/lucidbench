// Package server wires the lucidd HTTP routes.
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/ChinmayGit8765/lucidbench/internal/accounts"
	"github.com/ChinmayGit8765/lucidbench/internal/agentexec"
	"github.com/ChinmayGit8765/lucidbench/internal/boards"
	"github.com/ChinmayGit8765/lucidbench/internal/ci"
	"github.com/ChinmayGit8765/lucidbench/internal/cloud"
	"github.com/ChinmayGit8765/lucidbench/internal/cluster"
	"github.com/ChinmayGit8765/lucidbench/internal/config"
	"github.com/ChinmayGit8765/lucidbench/internal/council"
	"github.com/ChinmayGit8765/lucidbench/internal/databases"
	"github.com/ChinmayGit8765/lucidbench/internal/docker"
	"github.com/ChinmayGit8765/lucidbench/internal/hostinfo"
	"github.com/ChinmayGit8765/lucidbench/internal/ideas"
	"github.com/ChinmayGit8765/lucidbench/internal/jobs"
	"github.com/ChinmayGit8765/lucidbench/internal/k8s"
	"github.com/ChinmayGit8765/lucidbench/internal/linear"
	"github.com/ChinmayGit8765/lucidbench/internal/mcp"
	"github.com/ChinmayGit8765/lucidbench/internal/memory"
	"github.com/ChinmayGit8765/lucidbench/internal/payments"
	"github.com/ChinmayGit8765/lucidbench/internal/picture"
	"github.com/ChinmayGit8765/lucidbench/internal/power"
	"github.com/ChinmayGit8765/lucidbench/internal/prefs"
	"github.com/ChinmayGit8765/lucidbench/internal/projects"
	"github.com/ChinmayGit8765/lucidbench/internal/prompts"
	"github.com/ChinmayGit8765/lucidbench/internal/setup"
	"github.com/ChinmayGit8765/lucidbench/internal/stripe"
	"github.com/ChinmayGit8765/lucidbench/internal/themes"
	"github.com/ChinmayGit8765/lucidbench/internal/trello"
	"github.com/ChinmayGit8765/lucidbench/internal/usage"
	"github.com/ChinmayGit8765/lucidbench/internal/version"
	"github.com/ChinmayGit8765/lucidbench/internal/webui"
	"github.com/ChinmayGit8765/lucidbench/internal/work"
)

// HealthResponse is the body of GET /api/health.
type HealthResponse struct {
	Status  string `json:"status"`
	Version string `json:"version"`
}

// HealthHandler reports daemon liveness and version.
func HealthHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(HealthResponse{Status: "ok", Version: version.Version})
}

// New returns the root handler: /api/* routes plus the embedded UI. An
// optional config (from config.Load) is applied; without one, defaults are used.
func New(cfgs ...*config.Config) http.Handler {
	cfg := config.Default()
	if len(cfgs) > 0 {
		cfg = cfgs[0]
	}
	return NewWith(cfg, Deps{})
}

// Deps are services the caller may build itself, for example to warm one up
// at startup. A nil field is built by NewWith.
type Deps struct {
	Usage *usage.Service
	// Power serves /api/power when set. NewWith never builds one: the daemon
	// does and runs it, so a handler built in a test starts nothing.
	Power *power.Supervisor
}

// mcpHas reports whether any AI client has an MCP server whose name contains
// name (the same test the extensions gallery uses).
func mcpHas(cfg *config.Config, name string) bool {
	for _, s := range mcp.Read(mcp.FromConfig(cfg)).Servers {
		if strings.Contains(strings.ToLower(s.Name), name) {
			return true
		}
	}
	return false
}

// DataDir is where prefs, themes, usage, council and work records live.
// Without a resolvable data dir they are kept in a "lucidbench" folder under
// the working directory.
func DataDir() string {
	data, err := config.DataDir()
	if err != nil {
		return "lucidbench"
	}
	return data
}

// NewWith is New with a config and caller-built services.
func NewWith(cfg *config.Config, d Deps) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", HealthHandler)
	mux.Handle("/api/accounts", accounts.HandlerFor(func() accounts.Roots { return accounts.FromConfig(cfg) }))
	mux.HandleFunc("/api/cluster", cluster.Handler)
	mux.Handle("/api/config", config.Handler(cfg, nil))
	mux.HandleFunc("/api/jobs/", jobs.Handler)
	mux.HandleFunc("/api/jobs", jobs.Handler)
	ci.Register(mux, ci.New(cfg.CI))
	runners := ci.Filter{ComposeProject: cfg.CI.Runners.ComposeProject, ImageMatch: cfg.CI.Runners.ImageMatch}
	allowed := append([]string{"lucidbench"}, cfg.Docker.AllowedProjects...)
	for _, s := range cfg.Power.Stacks {
		allowed = append(allowed, s.Project)
	}
	docker.Register(mux, &docker.Service{Docker: docker.Exec, Policy: docker.Policy{
		Projects: allowed,
		IsRunner: runners.Matches,
	}})
	if d.Power != nil {
		power.Register(mux, d.Power)
	}
	k8s.Register(mux, &k8s.Service{Connect: k8s.Clientset})
	mux.Handle("/api/projects", projects.Handler())
	projects.RegisterAssessment(mux, &projects.AssessAPI{Runner: &agentexec.Runner{InContainer: cluster.InContainer, LookPath: exec.LookPath}})
	cloud.Register(mux, cloud.New(func() []projects.Project {
		l, err := projects.Load()
		if err != nil {
			return nil
		}
		return l.Projects
	}))
	mux.Handle("/api/mcp", mcp.HandlerFor(func() mcp.Roots { return mcp.FromConfig(cfg) }))
	data := DataDir()
	mux.Handle("/api/prefs", prefs.Handler(&prefs.Store{Path: filepath.Join(data, prefs.FileName), LegacyTheme: cfg.UI.Theme}))
	themes.Register(mux, &themes.Store{Dir: filepath.Join(data, "themes")})
	if d.Usage == nil {
		d.Usage = usage.NewService(cfg, data)
	}
	usage.Register(mux, d.Usage)
	var powerLog *power.ActivityLog
	if d.Power != nil {
		powerLog = d.Power.Log
	}
	dbs := databases.New(data, powerLog)
	if d.Power != nil {
		d.Power.AddExtra(dbs.Managers)
	}
	databases.Register(mux, dbs)
	themes.RegisterGenerate(mux, &themes.Generator{
		InContainer: cluster.InContainer,
		LookPath:    exec.LookPath,
		ProfileDir:  func(provider, profile string) (string, error) { return hostProfileDir(cfg, provider, profile) },
	})
	// The vault is opened on the first Memory or Boards request, so a daemon
	// that never uses them creates no folder.
	vault := memory.LazyOpener(cfg)
	memory.Register(mux, vault)
	picture.Register(mux, &picture.Service{
		Projects: projects.Load,
		Store:    &picture.Store{Vault: vault},
		Runner:   &agentexec.Runner{InContainer: cluster.InContainer, LookPath: exec.LookPath},
		Containers: func(ctx context.Context) ([]docker.Container, error) {
			return docker.List(ctx, docker.Exec, docker.Policy{})
		},
		Databases: func(ctx context.Context) ([]databases.Discovered, error) { return databases.Discover(ctx, docker.Exec) },
		Runners: func(ctx context.Context) ([]ci.Container, error) {
			return ci.ListContainers(ctx, ci.ExecDocker, runners)
		},
	})
	boards.Register(mux, vault)
	linear.Register(mux, linear.New(cfg.Integrations.Linear, vault, func() bool { return mcpHas(cfg, "linear") }))
	trello.Register(mux, trello.New(cfg.Integrations.Trello, vault))
	payments.Register(mux, &payments.Service{Stripe: stripe.New(cfg.Integrations.Stripe), Dir: filepath.Join(data, "payments"), Projects: projects.Load})
	councilSvc := council.New(filepath.Join(data, "council"), vault, &agentexec.Runner{InContainer: cluster.InContainer, LookPath: exec.LookPath})
	council.Register(mux, councilSvc)
	workSvc := work.New(filepath.Join(data, "work", "sessions"), &agentexec.Runner{
		InContainer: cluster.InContainer,
		LookPath:    exec.LookPath,
		ProfileDir:  func(provider, profile string) (string, error) { return hostProfileDir(cfg, provider, profile) },
	}, vault, projects.Load)
	work.Register(mux, workSvc)
	ideas.Register(mux, &ideas.Service{Council: councilSvc, Work: workSvc, Vault: vault})
	home, _ := os.UserHomeDir()
	setup.Register(mux, &setup.Service{DataDir: data, ProjectsPath: projects.Path, Home: home})
	resolver := &prompts.Resolver{Projects: projects.Load, Vault: vault, Council: councilSvc, Home: home}
	prompts.Register(mux, &prompts.API{
		Store:    &prompts.Store{Dir: filepath.Join(data, "prompts")},
		Resolver: resolver,
		Improver: &prompts.Improver{
			Runner:   &agentexec.Runner{InContainer: cluster.InContainer, LookPath: exec.LookPath},
			Resolver: resolver,
			RunsDir:  filepath.Join(data, "prompts", "runs"),
		},
	})
	mux.Handle("GET /api/about", hostinfo.AboutHandler(cfg, runtime.GOOS))
	mux.Handle("GET /api/host/tools", hostinfo.ToolsHandler(hostinfo.NewDetector()))
	mux.Handle("/api/", http.NotFoundHandler())
	mux.Handle("/", webui.Handler())
	return mux
}

// hostProfileDir returns the config directory of a signed-in host account
// profile, so a theme can be generated with that account.
func hostProfileDir(cfg *config.Config, provider, profile string) (string, error) {
	for _, p := range accounts.Detect(accounts.FromConfig(cfg)) {
		if p.Provider == provider && p.Name == profile && p.Location == accounts.LocHost && p.ConfigDir != "" {
			return p.ConfigDir, nil
		}
	}
	return "", fmt.Errorf("no host %s profile named %q", provider, profile)
}
