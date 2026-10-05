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

`LUCID_ADDR` overrides the listen address (default `:7420`). To embed the UI in the daemon, run `npm run build` in `web/` before `go build ./cmd/lucidd`.

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

## Web UI

The UI at <http://localhost:7420> has an Accounts page (every detected account, grouped by provider) and a System page (daemon, cluster and jobs, with a log viewer). It follows `ui.theme` from your config (`dark`, `light` or `system`); the toggle in the header overrides it and is remembered in the browser. Add `?theme=light` or `?theme=dark` to a URL to force a theme for one page view.

Provider names and marks are trademarks of their owners and are used only to identify each service. See [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).

## Desktop app

A Tauri 2 shell (`desktop/`) wraps the same UI in a native Windows window. Requirements: Go 1.26, Node 22, Rust (stable) and WebView2.

```sh
cd desktop
npm ci
npm run build:sidecar   # builds web/ and lucidd into src-tauri/binaries/lucidd-<triple>.exe
npx tauri build         # NSIS installer
```

The installer lands in `desktop/src-tauri/target/release/bundle/nsis/Lucidbench_0.1.0_x64-setup.exe` and installs per user (no admin prompt).

On start the app reads the daemon address from `LUCID_SERVER_ADDR` or `LUCID_ADDR` (default `127.0.0.1:7420`) and checks `/api/health`:

- **Attach**: if a daemon already answers within 1.5 s (for example the docker compose one), the window just opens it. The app never stops a daemon it did not start.
- **Sidecar**: otherwise it starts the bundled `lucidd`, waits up to 15 s for it to become healthy, and stops it when the app exits.

If neither works, the splash screen shows the error with a Retry button. The window only navigates to the local daemon (`127.0.0.1` / `localhost` on the configured port), and the daemon UI gets no access to the app's native APIs.

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
