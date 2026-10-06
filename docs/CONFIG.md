# Configuration

Lucidbench is configured by you, on your machine. Every user-specific setting
lives in your own config directory or in environment variables. Nothing
personal ever goes in the repository: no paths, accounts, keys or vault
locations.

## Where things live

| What | Default location |
|---|---|
| Config file | Windows: `%APPDATA%\lucidbench\config.yaml` |
| | macOS: `~/Library/Application Support/lucidbench/config.yaml` |
| | Linux: `$XDG_CONFIG_HOME/lucidbench/config.yaml` (usually `~/.config/lucidbench/config.yaml`) |
| Data dir (`locks/`, `kubeconfig`, `projects.yaml`, `ui.json`, `themes/`) | the same `lucidbench` directory as the config file |

These come from Go's `os.UserConfigDir()`. Two variables move them:

- `LUCID_CONFIG`: full path of the config file.
- `LUCID_DATA_DIR`: directory for locks and the kubeconfig.

`lucid config path` prints both locations. A missing config file is fine:
built-in defaults apply.

## Commands

```sh
lucid config path          # print the config file and data dir
lucid config init          # write a fully commented default config.yaml
lucid config init --force  # overwrite an existing one
lucid config show          # effective config, with where each value came from
```

`lucid config init` refuses to overwrite an existing file without `--force`.
The running daemon also serves the effective non-secret config at
`GET /api/config` for the UI.

## Load order

1. Built-in defaults
2. The config file
3. Environment variables (`LUCID_*`)

`lucid config show` prints the source of every value: `default`, `file` or
`env:NAME`.

Unknown keys in the file produce a warning and are ignored. An invalid value
is an error that names the file and the key, for example
`config <path>: ui.theme: must be one of dark, light, system`.

## Keys

| Key | Default | Environment override | Notes |
|---|---|---|---|
| `server.addr` | `127.0.0.1:7420` | `LUCID_SERVER_ADDR`, `LUCID_ADDR` | `host:port` the daemon listens on. The CLI uses it to reach the daemon. Loopback only by default; see [Network exposure](#network-exposure). |
| `providers.<p>.enabled` | `true` | `LUCID_PROVIDERS_<P>_ENABLED` | `p` is `claude`, `codex`, `grok` or `cursor`. `false` skips detection for that provider. |
| `providers.<p>.extra_dirs` | `[]` | `LUCID_PROVIDERS_<P>_EXTRA_DIRS` | Extra config directories to scan, for example a second Claude profile. The variable is a path list (`;` on Windows, `:` elsewhere) and replaces the file value. |
| `cluster.name` | `lucidbench` | `LUCID_CLUSTER_NAME` | Local kind cluster name. Lowercase letters, digits and dashes. |
| `agent.image` | `lucidbench/agent:dev` | `LUCID_AGENT_IMAGE` | Image used to run provider CLIs. |
| `vault.path` | empty (not configured) | `LUCID_VAULT_PATH` | Your notes vault, the folder behind Memory and Boards (any Obsidian vault works). Empty means `<data dir>/memory`. A leading `~` is expanded. Settings › General shows the folder in use; the UI does not write the config file, so change this key and restart. |
| `ui.theme` | `dark` | `LUCID_UI_THEME` | `dark`, `light` or `system`: the starting theme until you pick one in Settings (dark is Midnight, light is Daylight). Afterwards `ui.json` wins. |
| `ci.github.repos` | `[]` | `LUCID_CI_GITHUB_REPOS` | GitHub repositories (`owner/name`) whose self-hosted runners and recent workflow runs appear under Runners & CI. The variable is a comma-separated list. |
| `ci.github.token` | `env:GITHUB_TOKEN` | `LUCID_CI_GITHUB_TOKEN` | Secret reference for the GitHub API token. If the variable it names is empty, Lucidbench runs `gh auth token` when the GitHub CLI is installed. See [Runners & CI](#runners--ci). |
| `ci.runners.compose_project` | (empty) | `LUCID_CI_RUNNERS_COMPOSE_PROJECT` | Docker compose project whose containers are runners. Empty disables this match. |
| `ci.runners.image_match` | `github-runner` | `LUCID_CI_RUNNERS_IMAGE_MATCH` | Containers whose image name contains this text are runners too. Empty disables this match. |
| `docker.allowed_projects` | `[]` | `LUCID_DOCKER_ALLOWED_PROJECTS` | Compose projects whose containers the Containers page may start, stop and restart, besides the `lucidbench` project and the runner containers. Every other container is read-only. The variable is a comma-separated list. See [Docker and Kubernetes](#docker-and-kubernetes). |
| `power.cluster` | `on-demand` | `LUCID_POWER_CLUSTER` | `always`, `on-demand` or `off`. On demand, the kind cluster is started when a job is submitted and stopped when idle. See [On-demand infrastructure](#on-demand-infrastructure). |
| `power.cluster_idle_minutes` | `15` | | Minutes with no running pod in the `lucidbench` namespace, no active job and no job submitted before an on-demand cluster is stopped (1-1440). |
| `power.runners` | `always` | `LUCID_POWER_RUNNERS` | `always`, `on-demand` or `off` for the runner containers (`ci.runners`). On demand, a stopped runner is started when its repository has a queued run and stopped when idle. |
| `power.runner_idle_minutes` | `10` | | Minutes a runner has had no job and its repository no queued or running run before an on-demand runner is stopped (1-1440). |
| `power.stacks` | `[]` | | Compose projects to start and stop as a group: a list of `{project, mode}` (mode defaults to `on-demand`). Listed projects may also be started and stopped on the Containers page. |
| `power.poll_seconds` | `60` | | How often the cluster and runners are checked (10-3600). |

`LUCID_CLAUDE_DIRS` (a path list) still works and is added to
`providers.claude.extra_dirs`. `CLAUDE_CONFIG_DIR`, `CODEX_HOME` and the
provider API key variables keep their existing meaning.

## Secrets policy

References only, never values. Nothing personal ever goes in the repo.

- Config files never hold secret values. A secret setting accepts only a
  reference of the form `env:NAME`, which names an environment variable that
  holds the value. Anything else, such as a pasted key, is rejected, and the
  error does not echo the rejected text.
- `lucid config show` and `GET /api/config` print references and never
  resolve them.
- Your config file, `.env` files, databases and kubeconfig are git-ignored.
  Keep them in your user directories, not in a checkout.

The only secret setting today is `ci.github.token`.

## Runners & CI

The Runners & CI page, `lucid ci` and `/api/ci/*` show your self-hosted GitHub
Actions runners: the local runner containers (any `github-runner` image, or
every container of one compose project) and, for each repository in
`ci.github.repos`, its registered self-hosted runners and last 10 workflow
runs.

```yaml
ci:
  github:
    repos: ["you/your-repo", "you/another-repo"]
    token: "env:GITHUB_TOKEN"
```

The token needs read access to Actions (and Administration: read to list a
repository's runners); re-running failed jobs needs Actions: write. It is
resolved in this order: the variable named by `ci.github.token`, then
`gh auth token` if the GitHub CLI is installed and signed in. The token is
never logged, never written anywhere and never returned by the API; responses
only say where it came from (`env`, `gh` or `none`). GitHub responses are
cached for 30 seconds per repository.

Container actions (start, stop, restart) only ever apply to containers that
match the runner filter; any other name is refused. Every `POST` under
`/api/ci/` requires the header `X-Lucid-Confirm: yes`, which the web UI sends.

In docker compose, the daemon reaches local containers through the mounted
docker socket, but the image has no GitHub CLI. Export `GITHUB_TOKEN` in the
shell that runs `docker compose up` (or put it in a git-ignored `.env`);
compose passes it through to the container.

## Projects

The Projects page, `lucid projects` and `GET /api/projects` read your own
project list: what you build, what kind of thing each project is, what builds
into what, and what each one still needs. The file is yours and never part of
the repository:

- `<data dir>/projects.yaml` (next to `config.yaml`), or
- the path in `LUCID_PROJECTS`.

```sh
lucid projects init        # write the example to the data dir (refuses to overwrite)
lucid projects             # table grouped by category
lucid projects show <id>   # one project with its links and needs
```

The shape is in [`projects.example.yaml`](../projects.example.yaml):

```yaml
version: 1
projects:
  - id: my-app                 # required, unique, lowercase letters, digits, dashes
    name: My App               # required
    category: product          # product | portfolio | tool | experiment | coursework
    type: web-app              # game | web-app | desktop-app | cli | library | service | ml-research | site | video-system
    status: active             # idea | active | paused | frozen | shipped | archived
    visibility: public         # public | private | confidential
    repo: you/my-app           # optional
    local_path: <absolute path to your checkout>   # optional, absolute; Work sessions need it
    linear: APP-1              # optional, free text
    summary: One line.         # optional
    builds_into: [other-id]    # optional
    needs:                     # optional; status todo | doing | done | blocked
      - { what: "Release pipeline", from: build-tools, status: doing }
```

`local_path` is where the project's checkout lives on this machine. It must be
an absolute path when set; whether the folder exists is checked when something
uses it (Work refuses a project without one). `lucid projects show` prints it
and `GET /api/projects` returns it as `local_path`.

A `tool` is a private project that builds other projects; `builds_into` says
which. A need's `from` names the project that supplies it. Lucidbench derives
the reverse links (`built_by`, `needed_by`) and each project's need progress.
Confidential projects are marked as never sent to AI providers.

A missing file is not an error: the page shows how to create one. Duplicate
ids, unknown values and references to unknown ids are reported with the
project id and the field, and the rest of the file still loads. The file is
re-read on every request, so edits show up on the next refresh.

## MCP servers

The MCP page, `lucid mcp` and `GET /api/mcp` show which MCP servers each
client can reach, as a matrix. Lucidbench reads, for each server, only its
name, its transport (`stdio`, `http` or `sse`) and, for remote servers, the
URL host. Env, headers, arguments, commands, tokens and full URLs are never
read out, stored or returned.

| Client | Where servers are read |
|---|---|
| Claude Code | `~/.claude.json` (`mcpServers`, and each project's `mcpServers`, shown by folder name only), plus `~/.claude/settings.json` |
| Codex | `$CODEX_HOME/config.toml` or `~/.codex/config.toml`, `[mcp_servers.<name>]` |
| Grok | `~/.grok/config.toml`, `[mcp_servers.<name>]`; `[compat.claude] mcps = true` (or `cursor`) marks servers it loads from that client |
| Cursor | `~/.cursor/mcp.json`, `mcpServers` |

Disabled providers (`providers.<p>.enabled: false`) are skipped, and inside
Docker the host home comes from `LUCID_HOST_HOME`, as for accounts.

## Themes and UI prefs

Settings › Appearance, Sidebar and Extensions save to `<data dir>/ui.json`
through `GET`/`PUT /api/prefs`:

```json
{
  "theme": "midnight",
  "overrides": { "accent": "#4f8ff7", "density": "compact", "radius": 10, "font": "geist", "sidebar": "expanded" },
  "modules": { "projects": { "order": 1 } },
  "extensions": { "containers": { "added": true, "order": 0 } },
  "sprite_board": false
}
```

Every field is optional and validated; a bad value is refused with `400` and
the file is left as it was. Writes go to a temporary file that is renamed
over `ui.json`, so a crash never leaves half a file. `theme` is a theme id or
`system` (Midnight or Daylight, following the OS). Adding `?theme=<id>` to
any UI address shows that theme for the page view without saving it.

Themes:

| Route | What |
|---|---|
| `GET /api/themes` | the five presets (Midnight, Daylight, Graphite, Aurora, Paper) and your themes |
| `GET /api/themes/{id}/export` | one theme with its art inline (the import format) |
| `GET /api/themes/{id}/assets/{file}` | one art file of a user theme |
| `POST /api/themes` | create or replace a user theme: `{"theme": {...}, "assets": {"file.svg": "<svg…>", "file.png": "<base64>"}}` |
| `DELETE /api/themes/{id}` | delete a user theme (presets cannot be deleted) |
| `POST /api/themes/generate` | draft a theme from a description (below) |

Your themes live in `<data dir>/themes/<id>/theme.json` with their art next
to it. A theme is `{ id, name, version: 1, base: "dark" | "light",
description?, tokens, fonts?, art?, labels? }`:

- `tokens` may set only the UI's design tokens (`--background`, `--brand`,
  `--glow-1`, `--radius`, …; the list is in `internal/themes/theme.go`).
  Each value is checked by kind: colours (`#hex`, `oklch()`, `rgb()`,
  `hsl()`, `color-mix()`), shadows, lengths (`px`, `rem`, `em`). `url()`,
  `expression()`, `@import`, `;` and braces are refused.
- `fonts` names installed fonts only (`sans`, `mono`); themes cannot load
  web fonts.
- `art` names files in the theme's folder for the header banner
  (`headerImage`), the sidebar mascot (`sidebarMascot`), empty states
  (`emptyState`) and the Overview sprite board (`spriteBoard`: up to 12
  `{file, caption}`). Files are lowercase `.svg`, `.png`, `.webp` or `.gif`
  names. SVGs are sanitised on save: scripts, event handlers, external
  references, `<image>`, `<foreignObject>`, animations and DOCTYPEs are
  removed. PNG, WebP and GIF files are checked by content. Art is served
  only from inside the theme's folder, with `nosniff` and a locked-down CSP.
- `labels` may rename `overview_title`, `attention`, `all_clear`,
  `sprite_board` and `usage_meter` (up to 40 characters each).

Every `POST`, `PUT` and `DELETE` here needs the `X-Lucid-Confirm: yes`
header, which the UI sends.

### Describe a theme

`POST /api/themes/generate` with `{"description": "...", "provider":
"claude" | "codex" | "grok", "profile"?: "<account profile>"}` runs that
provider's own CLI on this machine, signed in with your subscription. It
sends a fixed prompt (`internal/themes/prompts/theme.md`) asking for theme
JSON and up to four original SVGs, and runs with no tools, MCP servers or
hooks in an empty temporary folder:

| Provider | Command |
|---|---|
| Claude | `claude -p --output-format json --model sonnet --system-prompt-file <prompt> --safe-mode --strict-mcp-config --tools "" --no-session-persistence` (description on stdin) |
| Codex | `codex exec --skip-git-repo-check --ephemeral --ignore-user-config --ignore-rules --sandbox read-only -o <file> -` |
| Grok | `grok -p <prompt> --output-format json --disable-web-search --no-subagents --max-turns 1` |

The answer is validated and sanitised (unsafe parts are dropped and listed)
and returned as a preview with token counts and cost when the CLI reports
them. Nothing is saved until you choose Save. A run is limited to 120
seconds and one at a time. A missing or signed-out CLI answers `424` with
what to run; a daemon running in Docker answers `501`, because the CLIs and
your sign-ins live on the host: use the desktop app or a lucidd started on
the host.

## Docker and Kubernetes

The Containers and Kubernetes extensions read the local Docker engine and
the Lucidbench kind cluster.

| Route | What |
|---|---|
| `GET /api/docker/containers` | every container, grouped by compose project (kind nodes by cluster), with the actions allowed on each |
| `GET /api/docker/stats` | one `docker stats --no-stream` sample |
| `GET /api/docker/images`, `/api/docker/volumes` | read-only lists |
| `GET /api/docker/containers/{name}/logs?tail=200[&follow=1]` | log tail; `follow=1` streams server-sent events |
| `POST /api/docker/containers/{name}/{start\|stop\|restart}` | only where allowed (below) |
| `POST /api/docker/projects/{project}/{start\|stop}` | every container of a compose project, only when all of them are allowed |
| `GET /api/k8s/namespaces`, `/nodes`, `/pods`, `/jobs`, `/events` | `?namespace=` filters; events are the newest 50 |
| `GET /api/k8s/pods/{ns}/{name}/logs?tail=200[&container=c][&follow=1]` | pod log tail or stream |
| `DELETE /api/k8s/jobs/{ns}/{name}` | deletes a job that has completed or failed (`409` otherwise) |

Container labels, mounts and environment values are never returned; of the
labels only the compose project and service and the kind cluster name are
read. Start, stop and restart are allowed only for containers of the
`lucidbench` compose project, runner containers (the `ci.runners` filter),
compose projects listed in `docker.allowed_projects` or `power.stacks`, and
kind nodes (restart only). Every other container is read-only. Every `POST`
and `DELETE` needs `X-Lucid-Confirm: yes`.

## On-demand infrastructure

The kind cluster, the runner containers and listed compose stacks can run
only when something needs them. The daemon checks them every
`power.poll_seconds` and records every start and stop, automatic or from a
button, in `<data dir>/power/activity.jsonl`.

| Mode | Meaning |
|---|---|
| `always` | Lucidbench never stops it. A stopped cluster is still started when a job is submitted. |
| `on-demand` | Started when something needs it, stopped when it has been idle. |
| `off` | Lucidbench never starts or stops it on its own. A job submitted to a stopped cluster fails with a clear message. |

The Start and Stop buttons (System, Settings › Infrastructure, Kubernetes,
the palette) work in every mode.

- **Cluster.** A job submitted through the API (`POST /api/jobs/hello`) or
  the Start button wakes the kind node: Lucidbench runs `docker start`
  (three tries, for the `Exited (128)` a node can show after Docker
  restarts), writes the kubeconfig again and waits up to 120 seconds for the
  API and every node to be Ready, restarting the node once if its API has
  not come up halfway through. It is idle when the `lucidbench` namespace has
  no running or pending pod, no job is active and no job has been submitted
  for `power.cluster_idle_minutes`. Reading the Kubernetes page never wakes
  the cluster and never counts as activity, so an open page does not keep it
  awake. A cluster whose pods cannot be read is never stopped.
- **Runners.** For each runner container, the repository comes from its
  `REPO_URL` (an organisation URL is left alone). When the repository has a
  queued workflow run, its stopped runner is started. A runner is idle when
  GitHub reports it not busy and its repository has no queued or in-progress
  run; after `power.runner_idle_minutes` of that it is stopped, after asking
  GitHub again, without the cache, that it is not busy. A busy runner is
  never stopped, and nothing is stopped while GitHub cannot be read. Only
  `on-demand` runners cause GitHub requests: two conditional requests
  (`If-None-Match`) per repository per check, which do not count against
  the rate limit when nothing changed, with backoff after errors and
  `Retry-After` honoured.
- **Stacks.** Compose projects in `power.stacks` start and stop as a group
  from the UI. There is no idle signal for a stack, so an `on-demand` stack
  is stopped only by "Sleep everything idle".
- **Sleep everything idle** stops every `on-demand` thing that is idle right
  now, without waiting for its timeout. Busy things stay up.

| Route | What |
|---|---|
| `GET /api/power` | every managed thing: mode, state (`running`, `starting`, `stopping`, `sleeping`, `stopped`, `partial`, `missing`), idle time, time until an on-demand stop, memory in use from `docker stats`, the last error, and the newest activity |
| `POST /api/power/{cluster\|runner\|stack}/{name}/{start\|stop}` | start or stop one thing. Starting the cluster answers `202` at once and shows `starting` until it is ready. Stopping a cluster with running pods or a busy runner answers `409`. |
| `POST /api/power/sleep` | sleep everything idle; the answer says what stopped and why the rest stayed up |

The daemon never writes `config.yaml`. Settings › Infrastructure shows the
`power:` block for the modes you pick, to paste into the file before a
restart.

## Containers

`docker-compose.yml` mounts your Lucidbench config directory read-only at
`/config` and sets `LUCID_CONFIG=/config/config.yaml`, so the containerised
daemon reads the same file as the CLI. The default source directory is
`%APPDATA%\lucidbench` on Windows and `~/.config/lucidbench` elsewhere. Set
`LUCID_CONFIG_DIR` (see `.env.example`, copy it to `.env`) to point somewhere
else, for example on macOS. Run `lucid config init` first so the directory
exists.

Inside the container:

- `LUCID_ADDR` is set by compose to `:7420` to match the published port, so it
  overrides `server.addr` from the file.
- The config directory is read-only. The daemon keeps its own data (locks,
  kubeconfig) in the container's data dir.
- Host paths written in the file (`extra_dirs`, `vault.path`) are resolved
  inside the container. The host home is visible at `/host-home`.

## Network exposure

`lucidd` has no login of its own, so by default only this machine can reach
it:

- `server.addr` defaults to `127.0.0.1:7420` (loopback).
- `docker-compose.yml` publishes the port as `127.0.0.1:7420:7420`. Inside the
  container the daemon listens on `:7420`, and only the loopback-published
  port reaches it.

Set `server.addr` to `:7420`, or publish the compose port on every
interface, only on a network you trust.
