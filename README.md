# Lucidbench

Lucidbench is an open-source, all-in-one AI workspace: your Claude, ChatGPT/Codex, Grok and Cursor accounts in one clear workspace, a cross-model council that lets models check each other, usage planning across all of them, Linear, GitHub and Obsidian integrations, and a Kubernetes job runner for agent work.

**Status: pre-alpha, M0 (containerised foundation).** Works today: account detection across Claude, Codex, Grok and Cursor (several accounts per provider), running a prompt through your own logged-in CLI inside a container, a local Kubernetes cluster with jobs, and a web UI for all of it. The council arrives in M1.

## Quick start

```sh
docker compose up
```

Then open <http://localhost:7420>.

## Development

Requirements: Go 1.26, Node 22, Docker.

```sh
# Web UI (dev server on :5173, proxies /api to :7420)
cd web
npm install
npm run dev

# Daemon (serves the API and the embedded UI from web/dist)
go run ./cmd/lucidd

# CLI
go run ./cmd/lucid version
go run ./cmd/lucid health

# Checks
go vet ./... && go test ./...
cd web && npm run build
```

`LUCID_ADDR` overrides the listen address (default `127.0.0.1:7420`, loopback only). To embed the UI in the daemon, run `npm run build` in `web/` before `go build ./cmd/lucidd`.

## Configuration

Everything user-specific lives in your own config directory or in `LUCID_*` environment variables, never in this repository. Run `go run ./cmd/lucid config init` to write a commented `config.yaml` (see `config.example.yaml`), and `lucid config show` to see the effective values. Secrets are never stored in the file; only `env:NAME` references are accepted. Details: [docs/CONFIG.md](docs/CONFIG.md).

## Accounts

Lucidbench detects the provider accounts you are already signed in to, by presence only. It never reads, prints, logs or copies token values.

```sh
go run ./cmd/lucid accounts          # table: provider, name, location, status
go run ./cmd/lucid accounts --json   # same, as JSON (also served at GET /api/accounts)
go run ./cmd/lucid login claude --profile work   # also: codex, grok
```

Evidence checked: Claude `~/.claude/.credentials.json` (plus extra dirs in `LUCID_CLAUDE_DIRS` and `CLAUDE_CONFIG_DIR`), Codex `$CODEX_HOME` or `~/.codex` `auth.json`, Grok `~/.grok/auth.json`, Cursor `~/.cursor/cli-config.json` (detection only, status `unknown` unless an auth marker is present), and the API key variables `ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, `XAI_API_KEY`, `CURSOR_API_KEY` (location `env`).

Extra accounts live in Docker named volumes labelled `lucidbench.profile=<provider>/<name>`. `lucid login` creates the volume and prints the interactive `docker run` command to sign in inside the agent image; it does not log in for you. Grok has no known home override, so additional Grok accounts are volume profiles only.

Inside Docker, `lucidd` reads the host home mounted read-only at `/host-home` (`LUCID_HOST_HOME`); `docker-compose.yml` sets this up.

**Fair use:** profiles are separate accounts you own. Lucidbench never auto-rotates between profiles to extend or evade provider usage limits.

## Local cluster

Lucidbench runs agent jobs on a local Kubernetes cluster. It embeds [kind](https://kind.sigs.k8s.io) as a library, so you only need Docker (no kind binary).

```sh
go run ./cmd/lucid cluster up      # create the "lucidbench" cluster (first run pulls the node image)
go run ./cmd/lucid job hello       # submit a busybox hello Job, prints its name
go run ./cmd/lucid job list
go run ./cmd/lucid job logs <name>
go run ./cmd/lucid cluster status
go run ./cmd/lucid cluster down
```

The kubeconfig is written to `<user config dir>/lucidbench/kubeconfig`, never to your default kubeconfig. Use it with `kubectl --kubeconfig <path> get jobs -n lucidbench`. The daemon exposes `GET /api/jobs` and `GET /api/jobs/{name}/logs`, and answers 503 while no cluster exists. It also exposes `GET /api/cluster` (`{name, running, kubeconfig_present}`) and `POST /api/jobs/hello`, which the System page uses.

### Running the daemon in docker compose

The host kubeconfig points at `127.0.0.1`, which is unreachable from inside a container. With `LUCID_IN_CONTAINER=1` (set by `docker-compose.yml`) `lucidd` instead asks kind for an internal kubeconfig that addresses the control-plane container by name on the `kind` docker network. For that, the compose service:

- joins the `kind` docker network by itself (`docker network connect`) the first time it needs the cluster,
- mounts `/var/run/docker.sock` (the image ships the docker CLI, which kind's docker provider shells out to) and runs as root to use it.

So `docker compose up` works with or without a cluster. Create one whenever you want jobs:

```sh
docker compose up -d --build
go run ./cmd/lucid cluster up
```

Mounting the docker socket gives the daemon control of your Docker engine. Only run it on a machine you trust.

## Runners & CI

Lucidbench watches your self-hosted GitHub Actions runners: the runner containers on this machine (for example ephemeral [`myoung34/github-runner`](https://github.com/myoung34/docker-github-actions-runner) containers started by docker compose) and, for each repository you list, its registered runners and last 10 workflow runs.

```yaml
# config.yaml (lucid config path shows where it lives)
ci:
  github:
    repos: ["you/your-repo"]
    token: "env:GITHUB_TOKEN"     # falls back to `gh auth token` when empty
  runners:
    compose_project: "runforge"   # containers of this compose project ...
    image_match: "github-runner"  # ... or whose image contains this text
```

```sh
go run ./cmd/lucid ci                          # summary: runners, containers, last 24h of runs
go run ./cmd/lucid ci runners                  # registered runners and local containers
go run ./cmd/lucid ci runs --repo you/your-repo
```

The daemon serves the same data at `GET /api/ci/summary`, `GET /api/ci/runners` and `GET /api/ci/runs?repo=`, and can act: `POST /api/ci/containers/{name}/{start|stop|restart}` (runner containers only; any other container is refused) and `POST /api/ci/runs/{owner%2Fname}/{id}/rerun` (re-runs failed jobs). Every POST needs the header `X-Lucid-Confirm: yes`, which the web UI sends. The token is never logged or returned; GitHub responses are cached for 30 seconds. In docker compose the daemon uses the mounted docker socket for containers, and needs `GITHUB_TOKEN` exported (or in `.env`) for GitHub, because the image has no `gh`. Details: [docs/CONFIG.md](docs/CONFIG.md#runners--ci).

## Web UI

The UI at <http://localhost:7420> has an Accounts page (every detected account, grouped by provider) and a System page (daemon, cluster and jobs, with a log viewer). It follows `ui.theme` from your config (`dark`, `light` or `system`); the toggle in the header overrides it and is remembered in the browser. Add `?theme=light` or `?theme=dark` to a URL to force a theme for one page view.

Provider names and marks are trademarks of their owners and are used only to identify each service. See [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).

## Roadmap

- **M0**: containerised foundation (daemon, CLI, web shell, Docker, CI)
- **M1**: accounts, council and clarity
- **M2**: usage planner
- **M3**: boards and integrations
- **M4**: Kubernetes runner and acting agents
- **M5**: conversational terminal
- **M6**: public launch

See [docs/STACK.md](docs/STACK.md) for the stack decision.

## License

Apache-2.0. See [LICENSE](LICENSE).
