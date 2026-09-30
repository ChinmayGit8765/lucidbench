# Lucidbench

Lucidbench is an open-source, all-in-one AI workspace: your Claude, ChatGPT/Codex, Grok and Cursor accounts in one clear workspace, a cross-model council that lets models check each other, usage planning across all of them, Linear, GitHub and Obsidian integrations, and a Kubernetes job runner for agent work.

**Status: pre-alpha, M0.** Only the skeleton exists: a Go daemon, a CLI and an empty web UI.

## Quick start

```sh
docker compose up
```

Then open <http://localhost:7420>.

## Development

Requirements: Go 1.25, Node 22.

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

`LUCID_ADDR` overrides the listen address (default `:7420`). To embed the UI in the daemon, run `npm run build` in `web/` before `go build ./cmd/lucidd`.

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

- joins the external docker network `kind`,
- mounts `/var/run/docker.sock` (the image ships the docker CLI, which kind's docker provider shells out to) and runs as root to use it.

Because `kind` is declared `external`, create the cluster first, otherwise `docker compose up` fails with "network kind declared as external, but could not be found":

```sh
go run ./cmd/lucid cluster up
docker compose up -d --build
```

Mounting the docker socket gives the daemon control of your Docker engine. Only run it on a machine you trust.

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
