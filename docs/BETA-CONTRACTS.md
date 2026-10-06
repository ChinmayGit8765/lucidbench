# Beta contracts

The interfaces that the beta modules (Council, Memory, Boards, Work, Usage) build against. One
agent or contributor can build one module while another builds the next, without guessing.
Change a contract here first, then in code.

## The core loop

```
braindump ─► Council (clarify) ─► Memory page (the brief) ─► Board card (on a project)
          ─► Work session (agent in a worktree, your own account) ─► branch + PR ─► CI ─► Overview
```

Beta is done when this loop runs end to end inside the desktop app on one real idea.

## Data directory layout

Everything lives under `config.DataDir()`. Nothing user-specific is ever in the repository.

```
<DataDir>/
  config.yaml, projects.yaml, ui.json, themes/      existing
  memory/                       default Memory vault (when config `vault.path` is empty)
    Inbox/                      braindumps and council briefs land here
    Boards/<board-id>.md        native boards (see Boards)
    ...                         any user pages and folders
  council/<session-id>.json     council session record (rounds, critiques, usage)
  work/sessions/<id>/           session.json, events.jsonl (normalised events), raw.log
  runs/<id>/                    existing per-run auth staging (internal/runner)
  power/activity.jsonl          every start and stop of the cluster, runners, stacks and database managers (internal/power)
  databases.yaml                saved database connections, passwords as env:NAME (internal/databases)
  payments/<project>.yaml       Stripe object ids created for a project, ids only (internal/payments)
  payments/audit.jsonl          every Stripe write: time, mode, action, object ids; no secrets (internal/payments)
  sections/<id>.json            Overview and project-page sections (internal/sections)
  sections/runs/*.json          what each "Describe a section" call cost (internal/sections)
  teams/<project>.yaml          a project's AI team when it is not kept in its checkout (internal/team)
```

A project's checkout may hold `.lucid/team.yaml`, its AI team (docs/TEAM-SPEC.md); Lucidbench
writes it only when the user saves the team there and confirms.

- `vault.path` in config overrides the Memory location. Lucidbench never picks an existing vault
  by itself; the user chooses one in Settings → General.
- **Confidential folders:** any folder whose `_folder.md` front matter says `confidential: true`,
  plus every page with `confidential: true`. Neither is ever sent to a provider: Council, Work and
  "ask memory" refuse them with a clear error.

## Go packages

### `internal/agentexec`: run a provider CLI once, on the host
Extracted from `internal/themes/generate.go` (the clean flags, the auth-error detection and the
usage parsing). Themes, Council and Work all use it.

```go
type Request struct {
    Provider, Profile string          // claude | codex | grok; profile "" = default
    SystemPrompt      string
    Prompt            string
    Dir               string          // working directory; "" = new empty temp dir
    Tools             ToolMode        // ToolsNone (council, themes) | ToolsEdit (work)
    Model             string          // optional provider model alias
    Harness           string          // "mine" | "clean" (default); ToolsNone is always clean
    Timeout           time.Duration
    Env               []string        // extra KEY=value pairs for the CLI (Work: TMP, TEMP, TMPDIR)
    OnEvent           func(Event)     // optional streaming callback (Work)
}
type Result struct { Text string; Usage Usage; Events []Event }
type Usage  struct { Provider, Model string; InputTokens, OutputTokens, CacheRead, CacheWrite int64; CostUSD float64; DurationMS int64 }
type Event  struct { Time time.Time; Kind string /* text|tool|tool_result|diff|approval|error|done */; Title, Body string; Raw json.RawMessage }
func Run(ctx context.Context, r Request) (*Result, error)
var ErrCLIMissing, ErrNotSignedIn, ErrTimeout, ErrInContainer error
```

- **Flags** must be read from each CLI's `--help`, never assumed.
  - ToolsNone uses the theme flags (no tools, no MCP, no hooks, no session).
  - ToolsEdit streams JSON (`claude -p --output-format stream-json --verbose`, `codex exec --json`,
    grok headless JSON) and allows file edits inside `Dir` only.
- **Harness:** Work sessions choose `harness: "mine" | "clean"`.
  - "mine" lets the CLI load the user's normal settings and hooks.
  - "clean" (the default) adds the no-settings, no-hooks flags: `claude --safe-mode
    --strict-mcp-config`, `codex --ignore-user-config --ignore-rules`. Grok has no such flag, so
    for grok the two behave the same.
- **Event normalisation:** `Result.Events` and `OnEvent` carry the same events. Claude and Codex
  are read from their JSON lines, Grok from `--output-format streaming-json` (its text arrives in
  deltas, which are joined into one `text` event). `Run` returns the partial `Result` along with
  an error. `Body` is cut at 8 KB; `Raw` keeps the CLI's line when it is under 64 KB.

### `internal/memory`: the vault as files
```go
type Page struct { Path string; Title string; Front map[string]any; Body string; Modified time.Time; Confidential bool }
func Open(root string) (*Vault, error)
func (v *Vault) List(dir string) ([]Entry, error)         // tree, folders first
func (v *Vault) Read(path string) (*Page, error)
func (v *Vault) Write(p *Page) error                      // atomic; creates folders
func (v *Vault) Move(from, to string) error
func (v *Vault) Delete(path string) error                 // to <vault>/.trash/
func (v *Vault) Search(q string, limit int) ([]Hit, error)
func (v *Vault) Backlinks(path string) ([]string, error)  // [[wikilinks]]
```

- Pages are Markdown with YAML front matter, and `[[wikilinks]]` stay Obsidian-compatible.
- Paths are vault-relative with `/` separators. `..`, absolute paths and anything outside the vault
  are rejected.

### `internal/boards`: native boards stored in Memory
- **Storage:** a board is `memory/Boards/<id>.md`:
  - front matter `{kanban-plugin: basic, board: <id>, title}`;
  - each `## Column` heading is a column;
  - each `- [ ] card` line is a card. This is the Obsidian Kanban plugin format.
- **Card metadata** sits on the indented lines under each card as `key:: value` pairs (Dataview
  style): `id`, `project`, `memory` (page path), `council`, `work`, `due`, `labels`, and the remote
  links `linear` (issue identifier), `linear_url`, `trello` (card id) and `trello_url`. The remote owns the
  item; the extensions (Linear, Trello) only write these four keys and never sync back.

```go
type Card  struct { ID, Title, Column, Project, Memory, Council, Work, Due string; Labels []string; Done bool; Linear, LinearURL, Trello, TrelloURL string }
type Board struct { ID, Title string; Columns []string; Cards []Card }
func List(v *memory.Vault) ([]BoardSummary, error)
func Get(v *memory.Vault, id string) (*Board, error)
func AddCard(v *memory.Vault, board string, c Card) (Card, error)   // assigns ID
func UpdateCard(v *memory.Vault, board string, c Card) error
func MoveCard(v *memory.Vault, board, cardID, column string, index int) error
```

- The default board `work` has the columns Inbox, Ready, In progress, Review and Done. It is
  created on first use.

### `internal/council`
- **Default flow** (configurable per run):
  1. **Propose:** one proposer, default `claude`, turns the braindump into a draft brief.
  2. **Critique:** two critics, default `codex` and `grok`, run in parallel.
  3. **Synthesise:** the proposer revises.
  4. Repeat steps 2 and 3, **at most 2 rounds**. Stop early when no critic reports a `blocker`.
- **Fallback:** any provider that is not signed in is skipped with a note. With one provider, the
  council runs as propose + self-critique and says so.
- **Brief** front matter: `type: brief, status: draft|approved, project, council: <id>`. Its
  sections are Problem, Outcome, Done criteria (each with a proof), Risks, First steps, and
  Open questions.
- **On approve:** the page status flips to `approved`, and a card is added to board `work`,
  column Ready, linked to the page and the project.

```go
type Session struct { ID, Input string; Rounds []Round; BriefPath string; Status string; Usage []agentexec.Usage }
func Start(ctx, in StartRequest, onUpdate func(Session)) (*Session, error)
func Approve(id string, project string) (*boards.Card, error)
```

### `internal/work`
- **Sessions:** `{id, provider, profile, harness, project, repo_path, branch, worktree, prompt,
  card, status: running|done|failed|stopped, started, ended, usage}`.
- **Environment (beta):** a git worktree at `<repo_path>/../<repo-name>-lucid-<short-id>` on a
  new branch `lucid/<short-id>-<slug>` from the repo's current default branch. The container
  environment is optional and comes after beta.
- **No sandbox yet:** the agent runs as the user. It is asked to stay in its worktree, which is not
  enforced, and the Work UI says so. To keep the files a CLI writes to "the temp folder" inside the
  worktree, `TMP`, `TEMP` and `TMPDIR` for the agent process are set to `<worktree>/.lucid-tmp`.
  - It is created at start and listed in the repository's `info/exclude`, so git never shows it
    (linked worktrees share that file).
  - It is removed when the run ends.
- **Stop ends the whole tree.** `agentexec` runs each CLI in a Windows Job Object
  (kill-on-close) or a Unix process group and kills all of it on cancel or timeout, so a helper
  the CLI started cannot keep editing. A stopped session is `stopped` and keeps the usage the CLI
  had streamed (Claude reports it per message), with `usage.note` "stopped before the CLI reported
  its final cost".
- **The prompt** for a card is the brief (page body), plus "work only in this worktree; commit
  with clear messages; do not push". It is built from the builder template; see Prompt Studio below.
- **After the run:** a diff summary is shown. "Open PR" pushes the branch and runs
  `gh pr create --draft`, only after the user confirms it in the UI (`X-Lucid-Confirm`). Merging
  is always the user's click on GitHub.
- **Repo paths:** `projects.yaml` gains an optional `local_path`. Work refuses a project without
  one, with a clear message.

### `internal/usage`
- **Claude Code:** read the local JSONL logs (`~/.claude/projects/**/*.jsonl` message usage)
  for tokens per day, model and project.
- **Codex:** read `~/.codex/sessions/**/rollout-*.jsonl`, taking `token_count` events for tokens
  and the `rate_limits.primary/secondary` windows (used %, resets_at).
- **Lucidbench runs:** read `agentexec.Usage` from council and work records.
- **Unknown sources:** Grok and Cursor show "not available yet". Do not invent numbers.

## HTTP routes (all JSON; every write needs `X-Lucid-Confirm: yes`)

| Route | Method | Purpose |
|---|---|---|
| `/api/memory/tree?dir=` | GET | folder listing |
| `/api/memory/page?path=` | GET / PUT / DELETE | read / write / trash a page |
| `/api/memory/move` | POST | `{from,to}` |
| `/api/memory/restore` | POST | `{path}` moves the newest trashed copy of `path` back (undo); `409` when something is at `path` again |
| `/api/memory/search?q=&limit=` | GET | full-text hits |
| `/api/memory/backlinks?path=` | GET | pages that link to a page |
| `/api/memory/info` | GET | `{root, pages}`: the vault folder and its page count (folder notes and boards excluded) |
| `/api/boards` | GET | board summaries |
| `/api/boards/{id}` | GET | board with cards |
| `/api/boards/{id}/cards` | POST | add card |
| `/api/boards/{id}/cards/{card}` | PUT | update the fields in the body, or move (`{column,index}`) |
| `/api/council/sessions` | GET / POST | list / start `{input, project?, proposer?, critics?, rounds?}` |
| `/api/council/sessions/{id}` | GET | session (poll) |
| `/api/council/sessions/{id}/events` | GET (SSE) | live updates |
| `/api/council/sessions/{id}/again` | POST | `{notes}` re-runs a round with the operator's extra notes |
| `/api/council/sessions/{id}/approve` | POST | `{project?}` returns the new card |
| `/api/work/sessions` | GET / POST | list / start `{card? , project, prompt?, provider, profile?, harness, model?}`; `card` is a card id on the work board or `<board>/<id>` |
| `/api/work/sessions/{id}` | GET | session + diff summary; `?refresh=1` reads the diff again |
| `/api/work/sessions/{id}/events` | GET (SSE) | live normalised events |
| `/api/work/sessions/{id}/raw` | GET | the CLI's own lines, as written |
| `/api/work/sessions/{id}/stop` | POST | stop (kills the CLI's whole process tree) |
| `/api/work/sessions/{id}/remove` | POST | `{discard}` removes the worktree; unpushed or uncommitted work needs `discard: true` |
| `/api/work/sessions/{id}/pr` | POST | push branch + draft PR |
| `/api/work/sessions/{id}/pr/refresh` | POST | read the PR's state from gh now; a merged PR moves its card to Done once |
| `/api/usage/summary?days=` | GET | per provider: tokens, cost, windows |

## Web modules (ids are fixed)

`council`, `memory`, `boards`, `work`, `usage`. Each replaces its "soon" placeholder in
`web/src/modules/` and keeps its section. Each one provides:
- **Overview tiles:** Council: drafts waiting for approval. Work: running sessions. Boards: Ready
  count. Usage: window bars.
- **Needs attention entries:** a brief to approve, a session finished with a diff to review, a
  usage window above 80 %.
- **Palette commands:** "New braindump…", "New page…", "Add card…", "Start work on card…".

## Rules every module follows
- Your own logged-in CLIs. No credentials are stored.
- Confirm before anything that writes outside Lucidbench's data dir, pushes or spends.
- Confidential pages and projects never reach a provider.
- Every agent run records its usage.

## Additions after the beta review

**Work**
- `GET /api/work/defaults?project=` returns the default allowed commands for a project. These are detected from its stack (go.mod, package.json, Cargo.toml), plus read-only git and `git add`/`commit`/`restore`. `git push`, `git remote`, `rm`, `sudo`, `curl`, `wget`, `gh` and shells are never allowed.
- `projects.yaml` `work.allowed_commands` replaces the stack part for one project. `StartRequest.allowed_commands` replaces the whole list for one session. A refused entry is a 400.
- `GET /api/work/sessions` returns summaries without prompt, answer or patches. `GET /api/work/sessions/{id}` returns everything.
- New session fields: `allowed_commands`, `pr_state` (draft|open|merged|closed), `pr_checks` (passing/failing/pending), `pr_checked` and `card_done`.
  - The PR state is read with `gh pr view`, at most once a minute per session.
  - The first time a PR is seen merged, its card moves to Done.

**Council**
- Approving returns 409 while the latest round still has a blocker, unless the body carries `approved_with_blockers: true`. That flag is recorded on the session.
- The automatic loop is capped at 2 rounds. Ask again adds rounds beyond the cap.

**Overview**
- "Review" counts the cards in the work board's Review column.
- "PRs open" counts draft and open PRs only. Merged and closed PRs are not counted; a PR whose state is not yet known still is.

**Project assessments**
- `GET /api/assess/kinds` lists the kinds with their questions (`id`, `text`, `type` choice|text|bool, `options`).
- `GET /api/projects/{id}/assessment` returns `{assessment, source, suggestion, suggested_kind, can_suggest}`; `assessment` is null until the project is assessed.
- `PUT /api/projects/{id}/assessment` with `{kind, answers}` confirms it. Every choice and bool question must be answered; `assessed_at` is set by the daemon. It is saved in `<data dir>/assessments/<id>.yaml`; `projects.yaml` is never written.
- `POST /api/projects/{id}/assessment/preview` computes the suggestion for answers without saving. `POST .../suggest` asks the user's CLI (no tools, file names only) to guess answers; confidential projects get 403 and nothing is saved.
- `suggestion` is `{done_criteria, check_commands, risk{level, reasons}}`. `GET /api/projects` carries `assessment`, `risk` and `assessment_from` per project.
- Council adds the done criteria to the propose prompt. Work adds the check commands to a project's default allowed commands, also when `work.allowed_commands` is set.

**Power**
- `GET /api/power`, `POST /api/power/{kind}/{name}/{start|stop}`, `POST /api/power/sleep` and `POST /api/docker/projects/{project}/{start|stop}`. See docs/CONFIG.md `power`.

**Payments** (`internal/stripe`, `internal/payments`, web module `payments`, Business extension)
- `GET /api/payments/status|account|balance|charges|products|links|webhooks|audit` and `GET /api/payments/project/{id}` read Stripe (cached 60 s) and the local files. `charges` takes `limit` and `days`; with `days` it also totals succeeded payments per currency.
- `POST /api/payments/plan` takes `{project, product_name, description?, prices[{amount (smallest unit), currency, interval?, nickname?}], success_url, webhook_url?, webhook_events?}` and returns a plan `{id, digest, mode, writable, refusal?, request, steps[{id, kind, title, detail, path, idempotency_key, needs}], warnings}`. It calls nobody and writes nothing.
- `POST /api/payments/plan/execute` takes the plan back unchanged, needs `X-Lucid-Confirm: yes`, rebuilds the steps from `request` and refuses a changed digest (409). A key that is not a test key gets 403 `live mode writes are not supported in this version` before any request is made. The answer is `{plan_id, project, mode, status: complete|partial|failed, steps[{id, kind, title, status: created|failed|skipped, object_id?, url?, error?}], webhook_secret?, note?}`; the first failed step stops the run and the rest are `skipped`.
- Writes are the allow-listed creates only: products, prices, payment links, webhook endpoints, each with an `Idempotency-Key` of `lucid-<plan id>-<step id>`. No refund, payout or transfer call exists. `webhook_secret` appears in that one answer and nowhere else.

**Prompt Studio** (`internal/prompts`, web module `studio`, AI section)
- **Templates** have sections, each optional: `role`, `context`, `contract`, `task`, `constraints`, `verify`, `report`. The built-ins are versioned in `internal/prompts/templates/*.yaml`: builder, critic, scout, researcher, reviewer and braindump. The user's templates and snippets live in `<data dir>/prompts/templates/` and `<data dir>/prompts/snippets/`. A user template with a built-in's id overrides it in Studio. The council's three prompts are listed read-only as `council-propose`, `council-critique` and `council-synthesise`.
- **Variables:** `{{project.id|name|repo|local_path|summary|kind|risk|criteria|checks}}` and `{{date}}`, filled from the chosen project (`local_path` with home as `~`). An unknown or empty variable is left as written, with a lint note.
- **Work** builds every prompt from the built-in builder template, never from a user override. The order is its role and constraints (worktree only, commit, never push), then the session's allowed commands as the last constraint, the task, and its verify and report sections. A prompt composed in Studio keeps its own Verify and Report sections, and its Constraints lines join Work's.
- **Context sources** each return `{label, text, format, chars, tokens, confidential, truncated?}`:
  - `project`: the projects.yaml entry and its assessment's criteria and checks;
  - `page`: a Memory page;
  - `rules`: `docs/BETA-CONTRACTS.md`, `CONTRIBUTING.md`, `AGENTS.md` and `CLAUDE.md` from the project's checkout;
  - `repo`: the tree two levels deep with file counts, leaving out `.git`, `node_modules`, `dist`, `target` and `.claude`;
  - `card`: a card and its brief;
  - `decisions`: the project's recent council briefs and their outcomes.
- **Render** re-reads every source on the daemon, so text and confidentiality never come from the caller. `tokens` is an estimate (characters ÷ 4). For target `work`, the role and Work's own safety lines are left out, because Work adds them.
- **Lint** findings are `{severity: error|warning|info, code, message, section?}`:
  - errors: `confidential` (a confidential source or project with target `work` or `council`), `empty-task`, `source` (one cannot be read), `too-long` (a Council braindump over 20,000 characters);
  - warnings: `no-verify`, `no-report`, `no-done-criteria`, `secret`;
  - info: `unfilled`, `work-adds`.
  Any error sets `blocked`. Copy is never blocked.
- **Improve** is one call with no tools, on the chosen provider. The default is the first installed of claude, codex and grok; the claude model defaults to `haiku`. It sends the sections' text only, never the context sources. It refuses a confidential project (403), a `[[link]]` to a confidential page (403) and secret-looking text (400). The answer is a preview and is never applied. Each call's usage is kept in `<data dir>/prompts/runs/*.json`, and the Usage page counts it as source `studio`.
- **Send** is a hand-off in the browser. Work's New Session and the Council composer open with the text; nothing starts until the user starts it there.

| Route | Method | Purpose |
|---|---|---|
| `/api/prompts/templates` | GET | `{templates, sections, variables}` |
| `/api/prompts/templates/{id}` | PUT / DELETE | save a user template or override / delete it (a built-in comes back); council ids are 409 |
| `/api/prompts/snippets` | GET | the user's snippets, newest first |
| `/api/prompts/snippets/{id}` | PUT / DELETE | `{name, section?, body}` |
| `/api/prompts/context?kind=&project=&path=&board=&id=` | GET | one context source |
| `/api/prompts/render` | POST | `{sections, context: [{kind, project?, path?, board?, id?}], project?, target: work\|council\|copy}` returns `{text, chars, tokens, tokens_note, unfilled, lint, blocked, sources}`; changes nothing, so no confirm header |
| `/api/prompts/improve` | POST | `{text\|sections, provider?, model?, project?}` returns `{sections, text, provider, model, usage}` |

**Ideas** (`internal/ideas`, web module `ideas`, Workspace section)
- An idea is derived on every request; nothing is stored. A council session gives the idea `id` = the session id. A card with no council link gives `card:<board>/<card id>`. A `card:` id of a card the council made resolves to the council's idea.
- The joins: a card's `council::` names the session, and a Work session names its card (`card` + `board`), or the card's `work::` names the session.
- **Stage:** `merged` (a session's PR is merged), `pr` (a session has a PR), `work` (a session ran), `card` (a card exists), `brief` (a brief was written), else `braindump`.
- **Card history** is rebuilt from the records: Ready at the council's approval, In progress at a session's start, Review at its end, and Done when its PR was first seen merged. Moves made by hand are not recorded, and `history_note` says so. The time a PR was opened is not stored, so that timeline event is `untimed`.
- Ideas only read. They never ask gh or git: the PR state shown is the one Work last read.

| Route | Method | Purpose |
|---|---|---|
| `/api/ideas` | GET | `[{id, title, stage, status, project, created, updated, cost_usd, council, card, sessions, pr_url, pr_state}]`, newest first; `cost_usd` sums what the council and Work runs reported |
| `/api/ideas/{id...}` | GET | the summary plus `council_session`, `brief {path, title, status, body, exists, confidential}`, `card_view {…card, board, history, history_note}`, `work` (session summaries with diff stat, cost, `pr_url`, `pr_state`, `pr_checks`), `links` (pages that link the brief), `timeline`, `costs` (by provider and part) and `missing` (pieces that could not be read) |

**Sections** (`internal/sections`, Settings › Sections, Overview and the project detail)
- A section is JSON naming one route from `sections.APIs`, an allowlist of read-only GET routes, plus rows, filters, a sort, a limit, fields and a view. `Validate` refuses any other route or param, unknown keys, URLs, markup, scripts and anything but dotted field paths. Routes and the format are in docs/CONFIG.md, Sections.
- `POST /api/sections/generate` runs the user's CLI with `agentexec.ToolsNone` (claude on `haiku` by default) and the versioned prompt `internal/sections/prompts/section.md`; the answer is validated like a saved section and is a preview. Usage is recorded under `sections/runs/` and the Usage page counts it as source `sections`.

**AI team** (`internal/team`, the project detail's Team tab; docs/TEAM-SPEC.md)
- `lucid-team.yaml` v1: `version: 1`, `roles` (proposer, critic as one role or up to two, builder, reviewer, scout: `{provider, profile?, model?, mcp_allow?, allowed_commands?, budget_usd?}`) and `gates` (approve_brief, open_pr, merge: `human` only). Schema: `docs/schemas/lucid-team.schema.json`.
- Resolution: `<local_path>/.lucid/team.yaml`, then `teams/<project>.yaml`, then config `team:`, then the built-in defaults (which change nothing). Schema errors are hard; sign-in, MCP, model and budget findings are warnings.
- Council: `council.Service.Team` gives a project's proposer, critics and their models; a start request that leaves `proposer` or `critics` out takes the team's. Sessions record `models`, `profiles` and `team`; steps record `model`.
- Work: `work.Service.Team` gives the builder. `StartRequest.provider` may be empty when the project has one; the request's own provider, model, profile and `allowed_commands` win. Builder commands replace the stack part of the defaults, like `work.allowed_commands`. `GET /api/work/defaults` adds `team` (the builder, its budget and recent average cost). Sessions record `model` and `team`.
- The confidential rule wins: Council and Work refuse a confidential project before any provider runs, whatever its team.

**First-run setup** (`internal/setup`, the `/setup` view; not a module)
- Opens by itself on `/` when the data dir has neither `ui.json` nor `projects.yaml`, and from Settings › General › Run setup again. Six steps, each skippable: accounts, Memory vault, projects, theme, power modes, a sample braindump. Finishing or skipping writes `ui.json`.
- The vault step creates the default vault (`GET /api/memory/info`) or shows the `vault:` snippet for a folder the user picks; the power step shows the `power:` snippet. `config.yaml` is never written.
- Projects are appended to `projects.yaml` only: backup first, then the old bytes plus the new entries, proven by parsing before writing. Routes are in docs/CONFIG.md, Projects.

**State sprites and the launcher**
- Theme `art.sprites` slots: `loading`, `working`, `thinking`, `success`, `failure`, `sleeping`, `empty`, `celebrate`. An empty slot shows Lumi, the built-in placeholder (`web/src/components/StateSprite.tsx`).
- The sidebar mascot (and the logo) open the quick-actions launcher: New braindump, Start work, Add card, New prompt, Sleep idle (asks first), Switch theme. The mascot shows `working` while a Work session runs, `thinking` while a council runs, and `sleeping` when every managed thing is asleep.
- A card reaching Done or a PR first seen merged is celebrated once per browser (`localStorage` `lucidbench.celebrated`).

**End-to-end tests** (`e2e/`)
- Playwright, headless Chromium, one lucidd on 127.0.0.1:7466 per spec file with a temporary home and data dir. PATH holds only `e2e/fakecli` (as claude, codex, grok, gh, docker) and git; the harness refuses to start when any of those names resolves elsewhere.
