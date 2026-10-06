# Changelog

All notable changes to Lucidbench. Versions follow [semver](https://semver.org); pre-releases are
marked `-beta.N`.

## Unreleased

- **Databases extension:** finds Postgres, MySQL, MariaDB, Redis and Mongo containers, running or
  stopped, with their published ports and default database and user names (passwords are never
  read). Save one as a connection (`databases.yaml`, password as an `env:NAME` reference), then see
  its health and latency, version, size, tables with row estimates and columns, and run read-only
  queries: a read-only transaction with a 10 s timeout and a 500 row cap for SQL, an allowlist for
  Redis, a limited `find` for Mongo. Writes are not supported yet. "Open in pgweb" (MIT) or, for
  MySQL, Adminer runs as an on-demand container on 127.0.0.1 in a frame and stops when idle; the
  images are pulled at run time. Overview gets a healthy-versus-down tile and an unreachable
  connection shows under Needs attention. The daemon grows by about 9 MB for the four drivers.
- **Linear extension:** your issues as a board grouped by state, with team, project and "assigned
  to me" filters, the active cycle, and a link on every card. Promote a native card to a new Linear
  issue (you pick team and project and confirm) or link an existing issue. Needs a personal API key
  in `LINEAR_API_KEY`.
- **Trello extension:** boards as lists of cards, add a card, move a card between lists (each asks
  first), and link a native card to a Trello card. Needs `TRELLO_API_KEY` and `TRELLO_TOKEN`.
- Cards store `linear::` and `trello::` links. The remote owns the item; nothing is synced back.
  Credentials are `env:NAME` references under the new `integrations` config section.
- **Cloud:** a read-only inventory of what you have deployed, read through the CLIs you are
  already signed in to: Cloud Run services on Google Cloud, Pages projects and named Workers on
  Cloudflare, projects and recent deployments on Vercel, and who is signed in to Azure and AWS.
  Each provider fails on its own with a clear message, results are cached for five minutes, and
  Lucidbench stores no cloud credentials. Add `deploy:` to a project in `projects.yaml` to see its
  live deploy status on its card; a failed deploy shows under Needs attention.

## 0.3.0-beta.1 (2026-10-06)

The first beta: the core loop works end to end.

- **Council:** turn a braindump into a clear brief. One model proposes, two others critique in
  parallel, and the proposer revises for at most two rounds. Approving the brief puts a card on
  your board.
- **Memory:** a Notion-style editor on a plain Markdown vault that stays compatible with Obsidian.
  It has a page tree, wikilinks, backlinks and search. Pages marked confidential never reach a
  provider.
- **Boards:** native kanban boards stored as Markdown in Memory.
- **Work:** run Claude Code, Codex or Grok on a card in its own git worktree, using your own
  logins. Its steps read like a conversation and the raw log is one click away. It can open a
  draft PR on request.
- **Usage:** local token use and Codex rate-limit windows, plus what Lucidbench's own runs cost.
- **Project assessments:** a few questions judge what kind of project something is (game, web app,
  service, coursework and more). The answers suggest done criteria for Council briefs, check
  commands for Work, and a risk level. Choose Assess on a project's card; your projects file is
  never rewritten.
- **On-demand infrastructure:** the local cluster starts when a job needs it and sleeps after it has been idle. Self-hosted runners can wake for queued CI runs and sleep when idle. A Power tile shows what is awake and offers "Sleep everything idle".
- **Agent runs:** each run gets a clean, minimal config; refreshed logins are copied back safely.
  Runner containers can do the same on queued runs (opt in with `power.runners: on-demand`), and
  compose stacks start and stop as a group. A Power tile shows what runs and the memory it uses.

## 0.2.0

- **Desktop app:** upgrades replace a running engine. The engine is tied to the app process, and
  the app checks the engine version at startup.
- **Settings:** a module registry with core modules and extensions, appearance presets, themes
  you can describe in words (generated through your own CLI), and an Extensions gallery.
- **Containers and Kubernetes** extensions. **Projects** map and the **MCP** access matrix.
  **Runners & CI** for self-hosted GitHub Actions runners.

## 0.1.0

- **Foundation:** the Go engine and CLI, detection of the accounts you are signed in to, an agent
  container image, a local kind cluster with jobs, the user config layer, the web UI and the
  Windows desktop app.
