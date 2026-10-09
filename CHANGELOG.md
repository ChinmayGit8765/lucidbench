# Changelog

All notable changes to Lucidbench. Versions follow [semver](https://semver.org); pre-releases are
marked `-beta.N`.

## Unreleased

- **Assistant** (AI › Assistant, **Ask Lucid…** in the mascot launcher and the palette): a chat
  with claude, codex, grok or one of your bots, every turn run with no tools. Answers may propose
  actions from a fixed catalog (card, project, idea, page, council, Work session, needs, builds
  into); each is checked against your projects, the work board and Memory, shown as a card with
  Apply and Skip, checked again before Apply, and applied through the app's own routes. Councils
  and Work sessions go through the confirm dialog; changes to `projects.yaml` other than appending
  a project with a checkout are a snippet to paste. Confidential projects reach the model as an id
  only, and a conversation about one is refused before any CLI runs. Conversations are kept in
  `<data dir>/assistant/`, and their cost counts on the Usage page as `assistant`
  (see [Assistant](docs/CONFIG.md#assistant)).
- **Bots:** saved agents with a provider, model, persona, allowed actions and an emoji or sprite
  avatar, in `<data dir>/bots/`. Import lists, read-only, the agents you already made in Claude
  Code (`.claude/agents`), Codex (`.codex/agents`) and the Grok CLI (`.grok/agents`,
  `.grok/personas`). **Task this bot** starts a Work session with its persona first or puts it in a
  council's proposer seat.
- **Parse a braindump** (in the Assistant and on the Ideas page): a dump split into items with
  your own words, a restatement, a type, a project and a next step, each kept as a card, an idea
  or a council braindump, one at a time or as a selection; an item close to an existing card or
  idea says so.

## 0.3.0-beta.4 (2026-10-07)

- **Follow-ups in Work sessions:** a session no longer ends when the agent's turn does. It goes to
  **waiting** and keeps its worktree, branch, settings and attached browser; you reply from a
  composer at the bottom of the session (Enter sends, Shift+Enter is a new line, with the
  provider and model and "turn N" beside it) and the next turn runs in the same worktree. Each
  provider resumes its own session where its CLI can: `claude --resume` with the session id from
  its stream, `codex exec resume` with the thread id, `grok --resume` with an id Lucidbench gives it
  on the first run. Otherwise, or after a resume fails, the turn is a fresh run whose prompt
  carries a short summary of the earlier turns (your follow-ups, the agent's final messages and the
  changed files). The timeline marks each turn with a separator that says which way it went, and
  shows time and cost per turn and in total. **End session** finishes it; **Open PR** works while it
  waits. Stop now ends the running turn rather than the session. Overview shows "N sessions waiting
  for you". New routes: `POST /api/work/sessions/{id}/followup` and `/end`; the event stream stays
  open while a session waits (see [Work](docs/BETA-CONTRACTS.md#internalwork)).
- **Phone remote:** the follow-up route now works. A session that waits shows "waiting for you"
  and a **Send follow-up** sheet, the overview lists it as needing you, and the live tail shows the
  next turn; the token, confirm header, audit entry and confidential 403 all apply.
- **Restarts:** a session whose turn was running when Lucidbench stopped now waits for you, its turn
  marked interrupted, instead of failing. A waiting session lets go of the agent browser after its
  idle time and attaches it again on the next follow-up.
- Editing runs no longer pass `--no-session-persistence` (claude) or `--ephemeral` (codex), so the
  CLIs keep these sessions on disk like any other; their usage logs may count Work turns that
  Lucidbench also records.

## 0.3.0-beta.3 (2026-10-07)

- **Phone remote extension** (off by default): Settings › Phone remote (and **Pair phone** in the
  mascot launcher) turns on a second listener bound to one chosen LAN interface address or, in
  Tailnet only mode, this machine's Tailscale address; `0.0.0.0` and `::` are always refused, and
  `lucidd` itself stays on 127.0.0.1. A one-time QR code (5 minutes, single use) pairs a phone,
  which gets its own 32-byte token; only its SHA-256 is kept in `remote/devices.json`, and devices
  can be revoked. The phone page at `/r` (plain TypeScript, about 5 KB gzipped, dark, installable
  manifest) shows what needs you, running and recent Work sessions with a live tail and Stop, and
  council briefs to approve or send back, each behind a confirm sheet. The remote is an allowlist of
  nine routes; everything else is 404. Writes need the token and `X-Lucid-Confirm`, pair and token
  failures are rate limited, confidential projects are redacted, and every pairing, revocation and
  write is in `remote/audit.jsonl` and in Settings. Follow-up prompts answer 409 for now: Work
  sessions run once and never wait for input. No self-signed TLS; the docs explain using Tailscale
  instead, with a threat model (see [Phone remote](docs/CONFIG.md#phone-remote)). New Go module:
  `rsc.io/qr` (BSD-3-Clause) for the QR code.
- **Live browser extension:** a headless Chromium (`chromedp/headless-shell`, pulled at run time)
  in a container bound to 127.0.0.1, with its own throw-away profile and no host mounts, started
  on demand and stopped when idle through the power supervisor (every start and stop is in the
  activity log). The page shows it live as a JPEG screencast with an address bar, back, forward,
  reload, tabs, a PNG screenshot and fullscreen; **Take over** sends your mouse, wheel, keyboard and
  paste to it under a banner that says you are controlling it; "Preview a dev server" opens
  `localhost:<port>` in it through `host.docker.internal`. Only http and https URLs open. New
  Session gets **Attach a browser**: the agent gets `LUCID_BROWSER_CDP` and a short note, the
  browser stays up while the session runs, and screenshots taken for the session show in its
  timeline. The CDP client is a small hand-rolled WebSocket with no new module; the daemon grows by
  about 0.2 MB. API: `/api/browser` (see [Live browser](docs/CONFIG.md#live-browser)).
- **Customise, three tiers:** Settings › Customise explains reskin (themes), sections and
  features. **Sections** are widgets on the Overview and on each project's page: JSON in
  `<data dir>/sections/` that reads one route from an allowlist of read-only Lucidbench routes and
  shows a number, a list, a table, bars or text, with no URLs, scripts or code. Six templates ship
  (PRs waiting for me, This week's spend, Cards due soon, Failing deploys, Agents working, Sessions
  on this project), and **Describe a section** has your own CLI (no tools, claude on haiku) write
  one, previewed with live data before you save it. **Describe a feature** (also in the palette)
  never touches the running app: it opens a Council braindump marked as a feature request for
  Lucidbench, and the normal loop takes over.
- **AI team (`lucid-team.yaml` v1):** a project's team, in its checkout at `.lucid/team.yaml`, in
  the data folder, or as the `team:` default in `config.yaml`, names the provider, model, profile,
  MCP servers, allowed commands and budget of each role (proposer, critics, builder, reviewer,
  scout). Approving a brief, opening a PR and merging stay human. The Council seats the team's
  proposer and critics with their models, and Work starts with the builder's. The Team tab on each
  project checks it against this machine (sign-ins, MCP servers, model names, recent cost against
  budget), saves it to the repository or the data folder, and imports YAML. Schema in
  `docs/schemas/lucid-team.schema.json`, spec in `docs/TEAM-SPEC.md`. Confidential projects are
  still refused, whatever the team says.
- **First-run setup:** a fresh install opens a calm six-step setup (also in Settings › General ›
  Run setup again): which accounts are signed in and how to sign in to the rest; a new Memory
  vault or the snippet for a folder you already have; your git repositories, found in a folder you
  pick and appended to `projects.yaml` with a backup and a preview; a theme; power modes; and a
  sample braindump for the council. Every step can be skipped, and `config.yaml` is never written.
- **Quick-access launcher:** the sidebar mascot (or the logo) opens New braindump, Start work,
  Add card, New prompt, Sleep everything idle and Switch theme. The mascot shows what the app is
  doing: working, thinking or asleep.
- **State sprites:** themes can set `art.sprites` for loading, working, thinking, success,
  failure, sleeping, empty and celebrate. Every slot a theme leaves empty shows Lumi, a small
  original placeholder bot drawn in the theme's colours. Settings › Appearance › Sprites shows the
  slots and takes an upload per slot into the theme's folder (built-in themes are copied first).
  A theme may now carry 32 art files.
- **Feel:** cards and lists rise in, loading areas show the loading sprite, and a card reaching
  Done or a merged PR gets a short celebration, once. Moving a card, trashing a Memory page and
  removing an extension each offer Undo (`POST /api/memory/restore` brings a trashed page back).
- **Keyboard:** `?` lists the shortcuts; `g o`, `g w`, `g b`, `g m`, `g c`, `g i` and `g s` go to
  a page, `n` starts a braindump and `/` searches.
- **Work:** Open PR says why it is off (no commits yet, uncommitted changes, still running), and
  Check PR (`POST /api/work/sessions/{id}/pr/refresh`) asks GitHub now instead of within the minute.
- **Overview** tiles and Needs attention fill in as each answers, and one broken tile no longer
  blanks the page. The Projects empty state imports repositories from a folder.
- Fixed: the Ideas entries in the command palette re-read every council session each time the
  palette opened; they are now cached for a minute. Remaining "1 minutes"-style counts use the
  plural helper.
- **End-to-end tests:** a Playwright suite in `e2e/` drives the UI in headless Chromium against a
  lucidd with a temporary data dir and fake claude, codex, grok, gh and docker, from braindump to
  merged PR, plus setup, Memory, boards, themes, extensions, assessments and Studio. CI runs it on
  every push and pull request.
- **Payments extension (Stripe):** read your account in test or live mode (mode badge, balance,
  recent payments, products and prices, payment links, webhook endpoints, seven-day volume on
  Overview), and "Set up payments for a project": a short form becomes a reviewable plan, and
  confirming it creates the product, prices, a payment link and an optional webhook endpoint in
  **test mode only**. A live key is read-only: every write answers 403 before anything is sent to
  Stripe. Lucidbench never refunds, pays out or transfers. Each step carries an idempotency key, so
  running a plan again after a failure continues without duplicates; the webhook signing secret is
  shown once and never stored; created ids are linked in `<data dir>/payments/<project>.yaml` and
  every write is appended to `payments/audit.jsonl` (no secrets). Needs `STRIPE_API_KEY`
  (`integrations.stripe.key`); see [Payments](docs/CONFIG.md#payments-stripe).

## 0.3.0-beta.2 (2026-10-07)

- **Picture extension:** back-end schematics and front-end design canvases next to your code.
  Diagrams are Mermaid (`.mmd`) with a side-by-side editor, a live preview and the parser's error
  when the syntax is wrong; canvases are Excalidraw (`.excalidraw`, MIT) with PNG export. Both are
  lazy chunks that load on first use, and Excalidraw's drawing fonts ship with the app so the canvas
  makes no CDN request. They are saved in the project's `local_path` under `docs/picture`, or as
  Memory pages under `Picture/<project>` when it has none; saving shows the exact path first.
  "New from live state" builds a flowchart from the project's deploy entries, its compose
  containers, the databases linked by compose project or port, its CI runner containers and its
  `builds_into` links, with no AI and no network. "Draft from code" runs your own Claude, Codex or
  Grok CLI with no tools on a three-level file listing and the first 200 lines of the README, shows
  the result as a preview and never saves it; confidential projects are refused. Each project
  card gets a Picture row. The web build grows by about 1.1 MB for Excalidraw and 0.65 MB for Mermaid
  (plus 1.8 MB of Excalidraw's font-subsetting code, fetched only when a PNG is exported, and 0.5 MB
  of fonts), none of it on the first page load.
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
- **Prompt Studio:** compose big prompts in sections (role, context, contract, task, constraints,
  verify, report) from built-in templates: builder, critic, scout, researcher, reviewer and
  braindump → brief. Add context with token sizes: a project with its assessment, a Memory page,
  the project's rules files, a repo map, a card, or recent council decisions. A live preview
  estimates tokens and lints: missing verify or report, no done criteria, secret-looking strings,
  and a confidential page or project, which blocks sending to Work or the Council. Send to Work or
  the Council, copy, or save your own templates and snippets. "Improve this prompt" asks the
  cheapest model to restructure a rough prompt and shows a preview first. The council's prompts
  are listed read-only.
- **Work** builds its prompt from the builder template: the same safety rules, now with the
  session's allowed commands, then the task, and verify and report sections.
- **Ideas:** follow each idea from braindump to merged PR: its stage, the council session, the
  brief, the card and how it moved, the agent sessions, the PRs and their checks, a timeline and
  the cost by provider. Council sessions, cards, Work sessions and briefs link to it.

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
