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
| Data dir (`locks/`, `kubeconfig`, `projects.yaml`, `databases.yaml`, `ui.json`, `themes/`, `sections/`, `teams/`) | the same `lucidbench` directory as the config file |

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
| `integrations.linear.token` | `env:LINEAR_API_KEY` | `LUCID_INTEGRATIONS_LINEAR_TOKEN` | Secret reference for a Linear personal API key. See [Board connectors](#board-connectors-linear-and-trello). |
| `integrations.linear.api_url` | `https://api.linear.app/graphql` | `LUCID_INTEGRATIONS_LINEAR_API_URL` | Linear GraphQL endpoint. Change it only to go through a proxy. |
| `integrations.trello.key` | `env:TRELLO_API_KEY` | `LUCID_INTEGRATIONS_TRELLO_KEY` | Secret reference for the Trello API key. |
| `integrations.trello.token` | `env:TRELLO_TOKEN` | `LUCID_INTEGRATIONS_TRELLO_TOKEN` | Secret reference for the Trello token. |
| `integrations.trello.api_url` | `https://api.trello.com/1` | `LUCID_INTEGRATIONS_TRELLO_API_URL` | Trello REST base URL. Change it only to go through a proxy. |
| `integrations.stripe.key` | `env:STRIPE_API_KEY` | `LUCID_INTEGRATIONS_STRIPE_KEY` | Secret reference for a Stripe restricted or secret key. See [Payments (Stripe)](#payments-stripe). |
| `integrations.stripe.api_url` | `https://api.stripe.com` | `LUCID_INTEGRATIONS_STRIPE_API_URL` | Stripe API base URL. Change it only to go through a proxy or a local fake. |
| `team` | empty (Lucidbench's defaults) | | Your default AI team: a whole `lucid-team.yaml` version 1 document under this key, used by a project with no team of its own. It is checked when a project reads it, not at startup; a team that does not validate is skipped and the Team tab says why. See [AI team](#ai-team) and [TEAM-SPEC.md](TEAM-SPEC.md). |

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

The secret settings are `ci.github.token`, `integrations.linear.token`,
`integrations.trello.key`, `integrations.trello.token` and `integrations.stripe.key`. The `password` of a connection in
`databases.yaml` follows the same rule (see [Databases](#databases)).

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

## Board connectors (Linear and Trello)

The Linear and Trello extensions (Settings › Extensions) read and write remote
boards beside your native Boards. They are off until you add them, and each
needs credentials.

```yaml
integrations:
  linear:
    token: "env:LINEAR_API_KEY"
  trello:
    key: "env:TRELLO_API_KEY"
    token: "env:TRELLO_TOKEN"
```

Each value names an environment variable; the daemon reads it when a request
needs it. Secrets are never logged, never written anywhere and never returned
by any API: the status routes say only whether a variable is set. The error
messages Lucidbench shows never quote what the remote service answered.

- **Linear:** create a personal API key in Linear under Settings › Account ›
  Security & access (the "API" section), export it as `LINEAR_API_KEY` and
  restart the daemon. A Linear MCP server in your AI client does not give
  Lucidbench access: the connector talks to the GraphQL API itself and needs
  the key. The page shows your teams, projects, active cycle and issues grouped
  by state. From a native card you can **promote** it to a new Linear issue
  (you pick team and project and confirm; nothing is created before that) or
  **link** an existing issue. The card keeps `linear:: ENG-12` and the issue
  URL. There is no two-way sync: Linear owns the issue, and the card links to
  it.
- **Trello:** create an API key and token at `trello.com/power-ups/admin`, and
  export them as `TRELLO_API_KEY` and `TRELLO_TOKEN`. The page shows a board
  as lists of cards. You can add a card to a list and move a card between
  lists (each asks first), and link a native card to a Trello card.
- Remote reads are cached for 60 seconds, a rate-limited call (HTTP 429) is
  retried up to three times with a back-off (honouring `Retry-After`), and a
  rejected credential (HTTP 401) is reported as "token invalid".
- Every route that changes something (`POST`, `PUT`) requires the header
  `X-Lucid-Confirm: yes`, which the web UI sends. The `api_url` keys exist for
  proxies and for tests; leave them alone otherwise.

## Payments (Stripe)

The Payments extension (Settings › Extensions, category Business) reads a Stripe
account and, in test mode only, sets up a project's payments. It needs one key:

```yaml
integrations:
  stripe:
    key: "env:STRIPE_API_KEY"
```

Export the key as `STRIPE_API_KEY` where the daemon starts and restart it. A Stripe MCP
server in your AI client is not enough: the page talks to the Stripe API itself. The key
is sent in an `Authorization` header only; it is never logged, stored, returned or put in
a URL, and error messages never quote what Stripe answered (only Stripe's short error
code, such as `permission_error`).

- **Use a restricted key.** In the Stripe Dashboard open Developers › API keys › Create
  restricted key. For the read side give it **Read** on Account, Balance, PaymentIntents,
  Products, Prices, Payment Links and Webhook Endpoints. A key missing a permission is
  answered `403 Stripe refused the request (permission_error)` for that tab only.
- **The mode comes from the key.** A secret or restricted key says its mode in its second
  segment (`test` or `live`); Lucidbench reads that on every call and never shows the
  prefix. A test key is test mode, a live key is live mode, and anything that is not
  recognisably one of them is treated as live. The page shows a large TEST or LIVE badge.
- **Reads work in both modes:** account (name, country), balance, recent payments
  (payment intents: amount, currency, status, date, description), products with their
  prices, payment links and webhook endpoints. Lists are paged, cached for 60 seconds, and
  a rate-limited call (HTTP 429) is retried up to three times with a back-off.
- **Writes are test mode only in this version.** With a live key every write route
  answers `403 live mode writes are not supported in this version` before any request is
  sent to Stripe. Lucidbench never issues refunds, payouts or transfers, in any mode: the
  only calls it can make are creating products, prices, payment links and webhook
  endpoints.
- **Set up payments for a project** (Payments › Set up payments). You give a product name,
  its prices (one-off or recurring: amount, currency, interval), a success URL and,
  optionally, a webhook URL with events. Lucidbench builds a **plan**, the list of objects
  to create, and shows it. Nothing is created until you confirm. Each step is sent with an
  `Idempotency-Key` derived from the plan, so running the same plan again after a failure
  continues where it stopped without duplicating anything, and a partial failure shows
  which steps were created, which failed and which were skipped.
- **The webhook signing secret is shown once**, in the result of the step that created the
  endpoint. Lucidbench does not store, log or audit it; copy it into your server's
  environment then, or roll it in the Stripe Dashboard later.
- **What is written on this machine**, under `<data dir>/payments/`:
  - `<project>.yaml` links the created object ids (product, prices, payment link and its
    URL, webhook endpoint) to the project. It holds ids only.
  - `audit.jsonl` has one line per write: time, mode, action, object ids, project, plan id
    and result. It holds no keys and no secrets.
- Routes: `GET /api/payments/status|account|balance|charges|products|links|webhooks|audit`,
  `GET /api/payments/project/{id}`, `POST /api/payments/plan` (builds a plan, writes
  nothing) and `POST /api/payments/plan/execute` (needs `X-Lucid-Confirm: yes`). `charges`
  takes `limit` and `days` (`days` also totals the succeeded payments of that window per
  currency, which the Overview tile shows as seven-day volume).

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
    work:                      # optional: tune Work sessions on this project
      allowed_commands: [make, go, gofmt]   # replaces the stack defaults below
    builds_into: [other-id]    # optional
    needs:                     # optional; status todo | doing | done | blocked
      - { what: "Release pipeline", from: build-tools, status: doing }
    deploy:                    # optional: where it runs (see Cloud)
      - { provider: vercel, service: my-app }
```

`local_path` is where the project's checkout lives on this machine. It must be
an absolute path when set; whether the folder exists is checked when something
uses it (Work refuses a project without one). `lucid projects show` prints it
and `GET /api/projects` returns it as `local_path`.

A Work session cannot answer a permission prompt, so the commands it may run
without asking are fixed when it starts. By default that is read-only git
(`git status`, `diff`, `log`, `show`, `ls-files`, `branch`), `git add`,
`commit` and `restore`, plus `ls`, `cat`, `grep` and `find`, and the build tools
of the stack found at the checkout's root: `go` and `gofmt` for `go.mod`,
`npm`, `node` and `npx` for `package.json`, `cargo` for `Cargo.toml`. A project's
`work.allowed_commands` replaces the stack part (the check commands from its
[assessment](#project-assessments) are added either way), and the New Session form lets
you edit the whole list for one session. An entry is a command prefix (`go`
allows every `go ...`; `git status` only that). `git push`, `git remote`,
`rm`, `sudo`, `curl`, `wget`, `gh` and shells are never allowed, and the CLI is
also told to refuse `git push` and `rm -rf`. Claude and Grok get the list as
permission rules; Codex has no per-command list and runs in its workspace
sandbox with the network off.

A `tool` is a private project that builds other projects; `builds_into` says
which. A need's `from` names the project that supplies it. Lucidbench derives
the reverse links (`built_by`, `needed_by`) and each project's need progress.
Confidential projects are marked as never sent to AI providers.

### Importing projects (first-run setup)

The first-run setup's Projects step (also reachable from the Projects page when
the file does not exist yet) lists the git repositories in a folder you pick
and up to two levels below it, with a type guessed from their files
(`project.godot` is a game, `src-tauri` a desktop app, a `package.json` with
React or Vite a web app, `go.mod` plus `main.go` a CLI, and so on). Only the
folder names and those few top-level files are read. The ones you tick are
added as `category: experiment`, `status: active`, `visibility: private`
entries with their `local_path`; edit the file to change any of it.

Setup only ever appends: an existing `projects.yaml` is copied to
`projects.yaml.bak-<date>-<time>` first, the new entries are added after its
last byte, and the result is parsed to prove every existing entry is unchanged
before it is written. If the `projects:` list is not the last thing in the file
(so appending would change its meaning), nothing is written and setup shows the
lines to paste instead.

| Route | What |
|---|---|
| `GET /api/setup` | `{needed, ui, projects, projects_hint}`: setup opens by itself when neither `ui.json` nor `projects.yaml` exists |
| `GET /api/setup/dirs?path=` | the folders in `path` (your home when empty), names only, with `git: true` on repositories |
| `POST /api/setup/scan` | `{root}` → `{root, repos: [{id, name, local_path, type, why, existing?}]}` |
| `POST /api/setup/projects/preview` | `{entries: [{id, name, local_path, type?}]}` → the YAML that would be appended |
| `POST /api/setup/projects` | the same body; appends (needs `X-Lucid-Confirm`), or `409` with `{error, snippet}` |

The vault and power steps never write `config.yaml`: they show the `vault:` or
`power:` block to paste, as Settings does.

A missing file is not an error: the page shows how to create one. Duplicate
ids, unknown values and references to unknown ids are reported with the
project id and the field, and the rest of the file still loads. The file is
re-read on every request, so edits show up on the next refresh.

### Project assessments

A few preliminary questions judge what kind of project something is (game, web
app, desktop app, CLI or library, service or API, ML or research, site, video or
media, coursework) and help manage how it is built. Choose **Assess** on a
project's card. Each kind has five to seven questions; the answers give:

- **done criteria**, each with a proof hint, that Council offers the proposer when
  you brief that project;
- **check commands** (such as `go test` or `npm test`) that Work adds to the
  project's allowed commands, whether or not `work.allowed_commands` is set;
- a **risk level** (low, medium or high) with the reasons.

The cards show the kind and the risk. "Suggest answers from the repo" asks your
own CLI to guess from the project's file names (not their contents), with no
tools; it needs `local_path`, is never offered for a confidential project, and
every answer is still yours to confirm.

An assessment you confirm in the app is saved in
`<data dir>/assessments/<id>.yaml`, so your `projects.yaml`, with its comments
and layout, is never rewritten. You can also write one by hand under a project:

```yaml
    assessment:
      kind: game          # see GET /api/assess/kinds for the kinds and their questions
      answers: { engine: godot, stage: prototype, audience: just-me, multiplayer: "false", saves: "false" }
```

When a project has both, the one confirmed in the app wins. The kinds and
their rules live in `internal/assess/kinds/`.

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
  (`emptyState`), the Overview sprite board (`spriteBoard`: up to 12
  `{file, caption}`) and the state sprites (`sprites`, below). Files are
  lowercase `.svg`, `.png`, `.webp` or `.gif` names, at most 32 per theme. SVGs are sanitised on save: scripts, event handlers, external
  references, `<image>`, `<foreignObject>`, animations and DOCTYPEs are
  removed. PNG, WebP and GIF files are checked by content. Art is served
  only from inside the theme's folder, with `nosniff` and a locked-down CSP.
- `labels` may rename `overview_title`, `attention`, `all_clear`,
  `sprite_board` and `usage_meter` (up to 40 characters each).

Every `POST`, `PUT` and `DELETE` here needs the `X-Lucid-Confirm: yes`
header, which the UI sends.

### State sprites

A theme can give the app a picture for what it is doing. `art.sprites` maps a
slot to a file in the theme's folder:

| Slot | Shown |
|---|---|
| `loading` | in loading areas |
| `working` | while a Work session runs, on its page and as the sidebar mascot |
| `thinking` | while the council deliberates |
| `success`, `failure` | on Runners & CI when the latest runs pass or fail |
| `sleeping` | on the Power tile when everything is asleep, and as the mascot |
| `empty` | on empty boards and lists (when the theme has no `emptyState`) |
| `celebrate` | when a card reaches Done or a PR is merged |

```json
"art": {
  "sidebarMascot": "fox.png",
  "sprites": { "working": "fox-typing.gif", "celebrate": "fox-party.webp", "sleeping": "fox-nap.png" }
}
```

A slot left out shows Lumi, Lucidbench's own placeholder (a small lens-bot
drawn in the theme's colours), so the built-in themes always have every state.
Unknown slots are refused when a theme is saved and dropped from a generated
one.

To use your own pictures:

- **From the app:** Settings › Appearance › Sprites lists the eight slots with
  what each one shows now. On a theme of your own, Upload takes a PNG, GIF,
  WebP (up to 1 MB) or SVG (up to 256 KB) and saves it as
  `sprite-<slot>.<ext>` in the theme's folder; the trash button clears the
  slot. A built-in theme cannot change, so "Make an editable copy" saves a copy
  you can change and switches to it.
- **By hand:** put the files in `<data dir>/themes/<id>/`, add `art.sprites` to
  that folder's `theme.json`, and reload the page. Files go through the same
  checks as any theme art: SVGs are sanitised (scripts, event handlers and
  external references removed), and PNG, WebP and GIF files must really be
  those formats. Animated GIF and WebP files animate; under the OS's
  reduced-motion setting Lumi stands still.

Use art you made or have the rights to; Lucidbench ships none from other
games, films or brands.

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

## Sections

Settings › Sections and the **Add section** button on the Overview (and on a
project's Sections tab) manage widgets that show part of what Lucidbench
already knows. Each section is one JSON file in `<data dir>/sections/<id>.json`:

```json
{
  "id": "prs-waiting",
  "title": "PRs waiting for me",
  "placement": "overview",
  "source": {"api": "/api/work/sessions"},
  "view": "list",
  "filter": [{"field": "pr_state", "op": "in", "value": ["draft", "open"]}],
  "sort": {"field": "started", "desc": true},
  "limit": 10,
  "fields": [{"path": "title"}, {"path": "pr_state", "format": "badge"}, {"path": "pr_url", "label": "PR", "format": "link"}],
  "refresh_s": 60
}
```

- `source.api` must be one of the allowlisted read-only routes:
  `/api/work/sessions`, `/api/council/sessions`, `/api/ideas`, `/api/boards`,
  `/api/boards/{board}` (`board`), `/api/projects`, `/api/usage/summary`
  (`days`, 1-90), `/api/ci/runs`, `/api/ci/runners`, `/api/cloud/deploys`,
  `/api/accounts`, `/api/mcp` and `/api/memory/search` (`q`, `limit`). Each
  takes only its listed params. The browser reads the route itself, so a
  section reaches nothing a page of Lucidbench could not.
- `view` is `stat`, `list`, `table`, `bars` (two fields: label, number) or
  `markdown` (a text value, shown as plain text). `placement` is `overview`
  (the default) or `project`, which shows it on every project's page; there
  the filter value `{project}` stands for that project's id.
- `rows` is the path to the list in the answer (empty when the answer is a
  list); a `*` segment takes every value of a mapping, so `projects.*`
  flattens `GET /api/cloud/deploys`, and each row gets `_key`. Fields,
  filters and sorts use plain dotted paths only.
- Filter ops: `eq`, `ne`, `contains`, `in`, `nin`, `exists`, `missing`, `gt`,
  `lt`, `within_days` (a date up to N days ahead, overdue included) and
  `since_days`. Formats: `text`, `number`, `usd`, `percent`, `date`,
  `relative`, `link` (only an `http(s)` value becomes a link) and `badge`.
- Refused: any other route, unknown keys, any string with `://`, `<`, `>`,
  a backslash, a backtick or a `javascript:`/`data:` scheme, more than 8
  fields, 6 filters or 4 params, a title over 80 characters, `refresh_s`
  outside 15-3600, `limit` over 50, a file over 16 KB, more than 50 sections.
  A hand-edited file that breaks a rule is listed as broken and never shown.

Six templates ship built in: PRs waiting for me, This week's spend, Cards due
soon, Failing deploys, Agents working, and Sessions on this project.

**Describe a section** (`POST /api/sections/generate`, `{description,
provider?, profile?, model?, placement?}`, needs `X-Lucid-Confirm`) runs your
own CLI with no tools, the same way as Describe a theme, with the versioned
prompt in `internal/sections/prompts/section.md`, which lists the allowed
routes and their answers. Claude runs on `haiku` unless you name a model. The
answer is validated like a saved section and returned as a preview; the
Settings page fetches its route once to show it with live data, and nothing
is saved until you choose Save. Each call's cost is recorded in
`<data dir>/sections/runs/` and counts on the Usage page as source
`sections`.

| Route | Method | Purpose |
|---|---|---|
| `/api/sections` | GET | `{sections, broken}` |
| `/api/sections/catalog` | GET | the allowed routes with their params, the templates, views, formats and ops |
| `/api/sections/check` | POST | validate a section without saving it |
| `/api/sections/{id}` | PUT / DELETE | save (the body's id must match) / remove |
| `/api/sections/templates/{id}` | POST | add a copy of a template |
| `/api/sections/generate` | POST | a validated preview, not saved |

## AI team

A project's team (`lucid-team.yaml` version 1) says which provider and model
fill each role: proposer, critics, builder, reviewer and scout. It is read
from `<local_path>/.lucid/team.yaml`, else `<data dir>/teams/<project>.yaml`,
else the `team:` key of `config.yaml`:

```yaml
team:
  version: 1
  roles:
    proposer: {provider: claude, model: sonnet}
    critic: [{provider: codex}, {provider: grok}]
    builder: {provider: claude, model: sonnet, budget_usd: 2}
```

The Council takes its proposer, critics and their models from the team of
the project a braindump names, and Work takes the builder's provider, model
and allowed commands as a new session's defaults. Approving a brief, opening
a PR and merging always stay with you, and a confidential project is refused
whatever its team says. The format, the checks and the routes are in
[TEAM-SPEC.md](TEAM-SPEC.md).

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
  queued workflow run, its stopped runner is started; any queued run counts,
  including one waiting for a GitHub-hosted runner, so such a run can wake
  the runner for one idle period. A runner is idle when
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

## Cloud

The Cloud extension is a read-only inventory of what you have deployed. It runs the CLIs you are
already signed in to, asks them for JSON, and keeps nothing: Lucidbench has no cloud settings and
stores no cloud credential. Add it from Settings › Extensions.

| Provider | CLI | What is read |
|---|---|---|
| Google Cloud | `gcloud` | the active account and project, your projects, and Cloud Run services across regions (URL, last revision, ready or failed) |
| Cloudflare | `wrangler` | the signed-in account and its Pages projects; a Pages project's latest deployment and the deployments of a Worker only when a `deploy` entry names them |
| Vercel | `vercel` | the signed-in user and team, projects, and recent deployments (a project's status is its newest production deployment) |
| Azure | `az` | who is signed in (`az account show`) |
| AWS | `aws` | the account id (`aws sts get-caller-identity`) |

Wrangler has no command that lists Workers, so they appear only when a project's `deploy` list
names them. Each provider fails on its own with a clear state: not installed, not signed in
(shown with the command to sign in), or the CLI's own error, trimmed to one line. Emails, tokens
and environment values are never returned; the one account id shown is what the CLI prints.
Results are cached for five minutes and **Refresh** runs the CLIs again. Each CLI call has a
30-second limit.

`deploy` on a project in `projects.yaml` links it to this inventory:

```yaml
    deploy:
      - { provider: gcloud, service: my-api, region: us-central1, project: my-gcp-project }
      - { provider: vercel, service: my-app }          # the Vercel project name
      - { provider: wrangler, service: my-site }       # a Pages project or a Worker name
```

`provider` is `gcloud`, `wrangler` or `vercel`, and `service` is required. `region` and `project`
(the Google Cloud project id; the active project by default) are for `gcloud` only. Invalid
entries are reported and ignored. With the Cloud extension added, the project's card then shows
each entry's status, last deploy time and links. A failed deploy also appears under Needs
attention on Overview.

API: `GET /api/cloud` (every provider), `GET /api/cloud/{provider}` and `GET /api/cloud/deploys`
(project entries matched to the inventory). Add `?refresh=1` to skip the cache.

## Databases

The Databases extension finds database containers on your Docker engine, keeps connection profiles
for the ones you choose, and reads them through their own Go drivers: PostgreSQL (pgx), MySQL and
MariaDB (go-sql-driver), Redis (go-redis) and MongoDB (the official driver). It only reads. Add it
from Settings › Extensions; Docker is needed for discovery and for the embedded managers, and the
native view works against any reachable host once a connection is saved.

**Discovery** lists containers whose image is `postgres` (also `postgresql`, `postgis`,
`timescaledb`, `pgvector`), `mysql`, `mariadb`, `redis` (also `valkey`), or `mongo`, running or
stopped, with the port each publishes on this machine. It reads only the names of the default
database and user (`POSTGRES_DB`, `POSTGRES_USER`, `MYSQL_DATABASE`, `MYSQL_USER` and the
`MARIADB_` pair): the filter runs inside `docker inspect`, so no other environment value, a
password above all, reaches Lucidbench. A stopped container that lets Docker pick its port shows
no port until it runs.

**Connections** are saved in `<data dir>/databases.yaml`. The file is written by the app (comments
are not kept), but you can also edit it:

```yaml
connections:
  - id: shop                       # lowercase letters, digits, - and _
    label: Shop (local)
    engine: postgres               # postgres | mysql | redis | mongo
    host: 127.0.0.1
    port: 5432                     # the engine's default when omitted
    database: shopdb               # for Redis, the database number
    user: shop
    password: env:SHOP_DB_PASSWORD # a reference, never the value
    readonly: true
    prod: false                    # a prod connection is always read-only
```

`password` follows the [secrets policy](#secrets-policy): only `env:NAME` is accepted, a literal
value is rejected (and never echoed), and the variable must be set in the environment Lucidbench
starts from. A saved connection whose variable is unset shows the variable's name, not a value. API
responses show the `env:NAME` reference and nothing else. **Save as connection** on a discovered
container prefills host, port, database and user; you add the variable's name.

**Reading.** Per connection: health (connect and ping, with latency), version, size, the tables
(or collections, or Redis keys) with row estimates, and each table's columns. The query box is
read-only in every engine:

| Engine | What runs |
|---|---|
| PostgreSQL, MySQL, MariaDB | one `SELECT`, `WITH`, `SHOW`, `EXPLAIN`, `VALUES`, `TABLE` or `DESCRIBE` statement, inside a `READ ONLY` transaction that is always rolled back, with a 10 second statement timeout and at most 500 rows. Anything else is refused before it reaches the database; a write hidden in a statement (for example a data-modifying `WITH`) is refused by the transaction. |
| Redis | an allowlist of read commands: `GET`, `MGET`, `SCAN`, `HSCAN`, `SSCAN`, `ZSCAN`, `TYPE`, `TTL`, `PTTL`, `EXISTS`, `STRLEN`, `HGET`, `HGETALL`, `HKEYS`, `HVALS`, `HLEN`, `HMGET`, `LRANGE`, `LLEN`, `LINDEX`, `SMEMBERS`, `SCARD`, `SISMEMBER`, `SRANDMEMBER`, `ZRANGE`, `ZCARD`, `ZSCORE`, `ZRANK`, `ZCOUNT`, `DBSIZE`, `INFO`, `PING`, `EXPIRETIME`. `KEYS` is left out because it blocks the server. |
| MongoDB | a `find` with a limit of at most 500 documents: `db.users.find({"a": 1})` or `{"find": "users", "filter": {}, "limit": 20}`. Filters that run JavaScript (`$where`, `$function`, `$accumulator`) are refused. |

Writes are not supported in this version, and the page says so. TLS: PostgreSQL asks for TLS and
falls back only if the server has none, and MySQL prefers it, so point a non-local connection at a
server you trust.

**Embedded managers (optional).** "Open in pgweb" runs [pgweb](https://github.com/sosedoff/pgweb)
(MIT, image `sosedoff/pgweb`) as a container bound to `127.0.0.1` on a free port, connected to that
database in its own read-only mode, and shows it in a frame. MySQL and MariaDB can open
[Adminer](https://www.adminer.org) (Apache-2.0 or GPL-2.0, used under Apache-2.0; official image
`adminer`) the same way: Adminer asks for the password itself, has no read-only mode, and is
therefore not offered for a read-only or prod connection. Redis and Mongo have the native view only
(RedisInsight is not under a permissive licence, so it is not offered). The images are pulled the
first time you open a manager and are not redistributed with Lucidbench. A manager is an on-demand
thing like those in [On-demand infrastructure](#on-demand-infrastructure): it stops by itself 10
minutes after you last had its page open, "Sleep everything idle" stops it, and every start and
stop shows in the power activity log. The connection string reaches pgweb through a short-lived env
file, not the command line; `docker inspect` on a running pgweb container shows it, as it would for
any container started with a database URL.

API (every request that changes something or reaches a database needs `X-Lucid-Confirm: yes`):
`GET /api/databases[?health=1]`, `POST /api/databases` (save), `DELETE /api/databases/{id}`,
`GET /api/databases/{id}/health`, `GET /api/databases/{id}/schema`,
`POST /api/databases/{id}/query` with `{"query": "..."}`, `GET /api/databases/{id}/manager` and
`POST /api/databases/{id}/manager/start` or `stop`. Discovered containers (`docker:<name>`) have no
password and cannot be read until they are saved.

## Picture

The Picture extension keeps a project's diagrams and design canvases next to its code. Add it from
Settings › Extensions; it needs nothing else. A **diagram** is Mermaid text (`.mmd`) with a live
preview, for back-end schematics. A **canvas** is an Excalidraw drawing (`.excalidraw`, JSON) for
front-end layouts, with PNG export. The Mermaid and Excalidraw libraries load only when you first
preview a diagram or open a canvas. Excalidraw's drawing fonts are served from the app itself
(`/excalidraw/fonts`), except the large CJK set, which Excalidraw fetches from its CDN the first
time CJK text is drawn. `npm run dev` does not serve the fonts, so a dev canvas uses that CDN.

**Where pictures live.** If the project has a `local_path` that is a folder on this machine, in
`<local_path>/docs/picture/<name>.mmd` or `.excalidraw`, where git will see them. Otherwise in
Memory, as the page `Picture/<project>/<name>.mmd.md` (or `.excalidraw.md`) with the source in a
fenced block, so the vault stays Markdown. A name is one lower-case segment of `a-z`, `0-9`, `-` and
`_` (63 characters at most), so a name can never be a path, and a `docs` or `docs/picture` that is
a link or junction is refused. Saving asks first and shows the exact path.

**New from live state** builds a flowchart without AI or network from what Lucidbench knows: the
project's `deploy:` entries, the containers of its compose project, the databases linked to it, the
local CI runner containers of its `repo`, and its `builds_into` links. A compose project belongs to
the project when its name is the project id or the name of the `local_path` folder; a database is
linked when it is in such a compose project, or is published on a port one of those containers
publishes. Docker being unreachable leaves those parts out and says so. The same inputs always
give the same text, so you can commit it and read the diff.

**Draft from code** runs your own Claude, Codex or Grok CLI once, with no tools, and shows the
Mermaid it returns as a preview that is never saved on its own. The CLI sees exactly one prompt: the
project's file and folder names three levels deep (without `.git`, `node_modules`, `dist` and similar
folders and without credential-looking names such as `.env` or `*.pem`, at most 300 entries) and the
first 200 lines of its README. Left on its default it tries Claude Haiku first, the cheapest, then
Codex and Grok; you can pick a provider and model instead. Confidential projects
(`visibility: confidential`) are refused. The cost is shown on the preview.

API (a request that writes or calls a provider needs `X-Lucid-Confirm: yes`):
`GET /api/picture` (every project with its store and count), `GET /api/picture/{project}`,
`GET /api/picture/{project}/item?name=&kind=` (`kind` is `mermaid` or `excalidraw`),
`GET /api/picture/{project}/target?name=&kind=` (the exact path a save would write),
`PUT /api/picture/{project}/item?name=&kind=` with `{"content": "..."}`,
`GET /api/picture/{project}/live` and `POST /api/picture/{project}/draft` with
`{"provider": "", "model": ""}`.

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
