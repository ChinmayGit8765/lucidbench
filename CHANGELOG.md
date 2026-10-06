# Changelog

All notable changes to Lucidbench. Versions follow [semver](https://semver.org); pre-releases are
marked `-beta.N`.

## 0.3.0-beta.1 (unreleased)

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
- **Agent runs:** each run gets a clean, minimal config; refreshed logins are copied back safely.
- **On-demand infrastructure:** the kind cluster sleeps when idle and wakes when a job needs it.
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
