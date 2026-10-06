# Lucidbench

Lucidbench is an open-source, all-in-one AI workspace for building things with the AI accounts you already pay for (Claude, ChatGPT/Codex, Grok and Cursor). It turns a messy idea into clear, reviewed work and runs agents on it, from one desktop app.

**Status: beta (0.3.0-beta.1).** Expect rough edges. Report them in [issues](https://github.com/ChinmayGit8765/lucidbench/issues).

### The core loop

```
braindump ─► Council ─► brief in Memory ─► card on a Board ─► Work session ─► draft PR ─► CI ─► Overview
```

- **Council:** one model drafts a brief from your braindump, two others critique it in parallel, and the draft is revised for up to two rounds. You approve the result, and approving puts a card on your board.
- **Memory:** a Notion-style editor over a plain Markdown vault that Obsidian can open (page tree, `[[wikilinks]]`, backlinks, search). Pages marked `confidential: true` are never sent to a provider.
- **Boards:** native kanban boards stored as Markdown in Memory (Obsidian Kanban format).
- **Work:** runs Claude Code, Codex or Grok on a card in its own git worktree, using your own logins. The steps read like a conversation, with the diff inline, the raw log one click away, and Stop that really stops. When you confirm, it opens a **draft** PR. Merging is always your call.
- **Usage:** token use from your local Claude Code and Codex logs, Codex rate-limit windows, and what Lucidbench's own runs cost.
- **Also here:** account detection (several accounts per provider), the MCP access matrix, a projects map, Runners & CI for self-hosted GitHub Actions runners, Containers, Kubernetes (local kind), themes you can describe in words, and a Windows desktop app.
- **Extensions you add:** **Linear** (your issues as a board by state, promote a card to a new issue or link an existing one) and **Trello** (boards as lists of cards, add and move cards, link a card). Each needs credentials from your environment; see [Board connectors](docs/CONFIG.md#board-connectors-linear-and-trello). The remote owns the item: there is no two-way sync.
- **Cloud:** a read-only inventory of your Google Cloud Run services, Cloudflare Pages and Workers and Vercel projects (plus Azure and AWS sign-ins) through the CLIs you are already signed in to. Lucidbench stores no cloud credentials, and a project's card shows its live deploy status.
- **Databases:** finds Postgres, MySQL, MariaDB, Redis and Mongo containers (running or stopped), keeps connection profiles with `env:` passwords, and reads them read-only: health, tables and columns, and a query box. An optional pgweb (or Adminer for MySQL) opens in a frame and stops when idle. See [Databases](docs/CONFIG.md#databases).
- **Picture:** back-end schematics as Mermaid diagrams (with a live preview) and front-end design canvases in Excalidraw, saved in the project's own repo under `docs/picture` or in Memory. "From live state" draws a project's deploys, containers, databases and runners with no AI; "Draft from code" asks your own CLI for a diagram from the file tree and README. See [Picture](docs/CONFIG.md#picture).
- **Payments:** a Stripe extension. It reads your account (mode, balance, recent payments, products and prices, payment links, webhooks) in test or live mode, and "Set up payments for a project" turns a short form into a reviewable plan and creates products, prices, a payment link and an optional webhook endpoint in **test mode only**. See [Payments](docs/CONFIG.md#payments-stripe).
- **Live browser:** a headless Chromium in a container with its own empty profile (nothing from your own browser), started on demand and stopped when idle. Watch it live, take over with your mouse and keyboard under a clear banner, preview your own local dev server in it, take screenshots, and attach it to a Work session so the agent can drive it over CDP. See [Live browser](docs/CONFIG.md#live-browser).

**Safety in this beta:**
- Work agents run as you on your machine. They are told to stay in their worktree, but this is not sandboxed yet.
- Agents can spend real money on your subscriptions or API keys, and every run shows its cost.
- Payments never moves money. It cannot refund, pay out or transfer in any mode, and it creates things only with a test-mode key: with a live key every write is refused. A webhook signing secret is shown once and never stored. Use a restricted key.
- The Windows installer is not code-signed yet, so SmartScreen will warn on first run: choose *More info → Run anyway*.

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
    compose_project: ""   # containers of this compose project ...
    image_match: "github-runner"  # ... or whose image contains this text
```

```sh
go run ./cmd/lucid ci                          # summary: runners, containers, last 24h of runs
go run ./cmd/lucid ci runners                  # registered runners and local containers
go run ./cmd/lucid ci runs --repo you/your-repo
```

The daemon serves the same data at `GET /api/ci/summary`, `GET /api/ci/runners` and `GET /api/ci/runs?repo=`, and can act: `POST /api/ci/containers/{name}/{start|stop|restart}` (runner containers only; any other container is refused) and `POST /api/ci/runs/{owner%2Fname}/{id}/rerun` (re-runs failed jobs). Every POST needs the header `X-Lucid-Confirm: yes`, which the web UI sends. The token is never logged or returned; GitHub responses are cached for 30 seconds. In docker compose the daemon uses the mounted docker socket for containers, and needs `GITHUB_TOKEN` exported (or in `.env`) for GitHub, because the image has no `gh`. Details: [docs/CONFIG.md](docs/CONFIG.md#runners--ci).

## Projects and MCP

Describe what you build in your own `projects.yaml` (in the Lucidbench data directory, or `LUCID_PROJECTS`; never in this repository): each project's category (product, portfolio piece, private tool that builds other projects, experiment or coursework), type, status, visibility, what it builds into and what it still needs. Start from [`projects.example.yaml`](projects.example.yaml):

```sh
go run ./cmd/lucid projects init       # write the example, then edit it
go run ./cmd/lucid projects            # table grouped by category
go run ./cmd/lucid projects show my-app
go run ./cmd/lucid mcp                 # which MCP servers Claude Code, Codex, Grok and Cursor can reach
```

`lucid mcp` (and the MCP page, `GET /api/mcp`) reads only server names, transports and hosts from each client's own config; env, headers, arguments and tokens are never read out. Details: [docs/CONFIG.md](docs/CONFIG.md#projects).

## Web UI

The UI at <http://localhost:7420> opens on an Overview: AI accounts, runners, CI health with a pass rate and recent-run bars, the cluster, a "needs attention" list (failed runs with a re-run button, offline runners, stopped runner containers, expired sign-ins), recent activity and quick actions. Projects shows your projects by category as cards (status, visibility, repo, Linear id, builds-into links and a needs checklist) or as a "what needs what" dependency graph. MCP servers shows a matrix of servers against clients. Runners & CI shows the runner fleet with container controls (each asks before it acts) and every recent workflow run; Accounts groups every detected account by provider; System covers the daemon, cluster and jobs, with a log viewer. Press Ctrl+K (⌘K on macOS) for the command palette. Data refreshes every 10 to 15 seconds, or on demand with Refresh. It follows `ui.theme` from your config (`dark`, `light` or `system`); the toggle in the header overrides it and is remembered in the browser. Add `?theme=light` or `?theme=dark` to a URL to force a theme for one page view.

Provider names and marks are trademarks of their owners and are used only to identify each service. See [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).

## Desktop app

A Tauri 2 shell (`desktop/`) wraps the same UI in a native Windows window. Requirements: Go 1.26, Node 22, Rust (stable) and WebView2.

```sh
cd desktop
npm ci
npm run build:sidecar   # builds web/ and lucidd into src-tauri/binaries/lucidd-<triple>.exe
npx tauri build         # NSIS installer
```

The installer lands in `desktop/src-tauri/target/release/bundle/nsis/Lucidbench_<version>_x64-setup.exe` and installs per user (no admin prompt). Installing or uninstalling first stops any `lucidbench-desktop.exe` and `lucidd.exe` running from the install directory (and only those), so an update is never blocked by a locked file.

On start the app reads the daemon address from `LUCID_SERVER_ADDR` or `LUCID_ADDR` (default `127.0.0.1:7420`) and checks `/api/health`:

- **Attach**: if a daemon already answers within 1.5 s (for example the docker compose one), the window just opens it. The app never stops a daemon it did not start, with one exception below.
- **Version check**: the app compares its version with the one `/api/health` reports. If they differ and the daemon is the app's own bundled `lucidd`, it is restarted. Any other daemon is still attached, with a dismissible banner "Engine vX differs from app vY".
- **Sidecar**: otherwise it starts the bundled `lucidd`, waits up to 15 s for it to become healthy, and stops it when the app exits. On Windows the sidecar runs in a kill-on-close job object, so it also dies if the app is force-killed.

If neither works, the splash screen shows the error with a Retry button. The window only navigates to the local daemon (`127.0.0.1` / `localhost` on the configured port), and the daemon UI gets no access to the app's native APIs.

## Roadmap

- **M0** (done): containerised foundation (daemon, CLI, web shell, Docker, CI)
- **M1** (beta): accounts, council, memory, boards, work sessions
- **M2**: usage planner (a minimal Usage page ships in the beta)
- **M3**: boards and integrations
- **M4**: Kubernetes runner and acting agents
- **M5**: conversational terminal
- **M6**: public launch

See [docs/STACK.md](docs/STACK.md) for the stack decision.

## License

Apache-2.0. See [LICENSE](LICENSE).
