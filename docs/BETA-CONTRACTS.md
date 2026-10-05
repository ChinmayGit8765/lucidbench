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
```

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
  style): `id`, `project`, `memory` (page path), `council`, `work`, `due`, `labels`.

```go
type Card  struct { ID, Title, Column, Project, Memory, Council, Work, Due string; Labels []string; Done bool }
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
- **The prompt** for a card is the brief (page body), plus "work only in this worktree; commit
  with clear messages; do not push".
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
| `/api/memory/search?q=` | GET | full-text hits |
| `/api/boards` | GET | board summaries |
| `/api/boards/{id}` | GET | board with cards |
| `/api/boards/{id}/cards` | POST | add card |
| `/api/boards/{id}/cards/{card}` | PUT | update / move (`{column,index}`) |
| `/api/council/sessions` | GET / POST | list / start `{input, project?, proposer?, critics?, rounds?}` |
| `/api/council/sessions/{id}` | GET | session (poll) |
| `/api/council/sessions/{id}/events` | GET (SSE) | live updates |
| `/api/council/sessions/{id}/approve` | POST | `{project?}` returns the new card |
| `/api/work/sessions` | GET / POST | list / start `{card? , project, prompt?, provider, profile?, harness}` |
| `/api/work/sessions/{id}` | GET | session + diff summary |
| `/api/work/sessions/{id}/events` | GET (SSE) | live normalised events |
| `/api/work/sessions/{id}/stop` | POST | stop |
| `/api/work/sessions/{id}/pr` | POST | push branch + draft PR |
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
