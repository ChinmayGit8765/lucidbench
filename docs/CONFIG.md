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
| Data dir (`locks/`, `kubeconfig`) | the same `lucidbench` directory as the config file |

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
| `vault.path` | empty (not configured) | `LUCID_VAULT_PATH` | Your notes vault. A leading `~` is expanded. |
| `ui.theme` | `dark` | `LUCID_UI_THEME` | `dark`, `light` or `system`. |
| `ci.github.repos` | `[]` | `LUCID_CI_GITHUB_REPOS` | GitHub repositories (`owner/name`) whose self-hosted runners and recent workflow runs appear under Runners & CI. The variable is a comma-separated list. |
| `ci.github.token` | `env:GITHUB_TOKEN` | `LUCID_CI_GITHUB_TOKEN` | Secret reference for the GitHub API token. If the variable it names is empty, Lucidbench runs `gh auth token` when the GitHub CLI is installed. See [Runners & CI](#runners--ci). |
| `ci.runners.compose_project` | `runforge` | `LUCID_CI_RUNNERS_COMPOSE_PROJECT` | Docker compose project whose containers are runners. Empty disables this match. |
| `ci.runners.image_match` | `github-runner` | `LUCID_CI_RUNNERS_IMAGE_MATCH` | Containers whose image name contains this text are runners too. Empty disables this match. |

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
