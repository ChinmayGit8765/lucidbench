# Extending Lucidbench

Every page in the UI is a **module**: one file in `web/src/modules/` that
describes it. The sidebar, the router, the command palette (`Ctrl K` / `⌘K`),
Home's tiles and attention list and the Extensions gallery are all generated
from the list in `web/src/modules/index.ts` plus the user's prefs. Adding a
module is one file and one line; nothing about the sidebar is hard-coded.

## Core modules and extensions

| | Core module | Extension |
|---|---|---|
| `kind` | `"core"` | `"extension"` |
| In the sidebar | its own entry when it has a `group`, else under its `parent` | only once added from the Extensions gallery, under the Extensions entry |
| `group` | `workspace` (the top group) or `tools` (below the divider); leave it out for a module that lives under its parent | ignored |
| `parent`, `tab` | the module it belongs to, and its label in that family's tab strip | not used |
| `order` | position within its group, or among its parent's tabs | position under Extensions |
| `defaultEnabled` | `true` | whether a new user has it added |
| Extra fields | `aliases` (older top-level paths that still open it) | `category`, `requires` |

Make something core when every user needs it and it works with nothing but
the engine. Make it an extension when it depends on a tool, account or
service the user may not have (Docker, a cloud CLI, an MCP server).

The sidebar is nine entries, in two groups:

- **Workspace:** Home, Ideas, Work, Projects, Memory
- **Tools:** Assistant, Accounts, Extensions, Settings

and the modules without an entry of their own live under one, as tabs at the
top of the page (each tab is still its own page with its own address, and the
breadcrumb reads "Parent › Page"):

- **Home** took over Next up (`aliases: ["/nextup"]`).
- **Ideas** holds the braindump composer and the council's briefs; Council
  (`parent: "ideas"`, no tab) keeps `/council/<id>` for one session.
- **Work** has the tabs Sessions and Board (`boards`).
- **Accounts** has the tabs Accounts, Usage and MCP servers.
- **Settings** has the tabs Settings, System and Prompt Studio.
- **Extensions:** Runners & CI, Containers, Kubernetes, Cloud, Databases,
  Picture, Live browser, Linear, Trello and Payments, all added from the
  gallery. (Runners & CI, Containers and Kubernetes were on for everyone
  before; a `ui.json` from then keeps them, a new install starts without.)

Addresses that moved redirect in `redirectPath` (`web/src/modules/registry.ts`):
`/nextup` → `/`, `/council` → `/ideas`, `/assistant/braindump` →
`/ideas/new/split`, `/settings/extensions/*` → `/extensions/*`, and `/boards`
goes straight to the board when there is only one. Add a line there when you
move a page, so bookmarks keep working.

A module with `status: "soon"` shows in the sidebar or gallery but cannot be
opened or added. Opening an extension the user has not added leads to its
card in the Extensions gallery, so links between modules never dead-end.

## A module in one file

```tsx
// web/src/modules/notes.tsx
import { lazy } from "react"
import { NotebookPen, Plus } from "lucide-react"
import type { ModuleDef } from "@/modules/types"

export const notes: ModuleDef = {
  id: "notes",                    // also the prefs key
  title: "Notes",
  icon: NotebookPen,
  route: "/notes",                // /notes/anything arrives as props.subpath
  kind: "extension",
  category: "productivity",
  order: 20,
  defaultEnabled: false,
  description: "Quick notes beside your projects.",
  requires: { clis: ["git"] },    // checked live on the gallery card
  component: lazy(() => import("@/pages/Notes")),
  useCommands: () => [{ id: "new-note", label: "New note", group: "Actions", icon: Plus, run: () => {} }],
}
```

Then add `notes` to the `MODULES` array in `web/src/modules/index.ts`.

The page component receives `{ subpath: string[] }` and reads everything else
from two hooks:

- `useApp()` (`@/lib/app`): `open(id, subpath?)`, `navigate(path, replace?)`,
  `health`, `dockerUp`, `addAccount()`, `openPalette()`.
- `usePrefs()` (`@/lib/prefs`): the active theme, the user's prefs and
  `label(key, fallback)` for theme-renamed strings.

Use `PageHeader` from `@/components/Shell` for the title block,
`EmptyState`, `ErrorState` and `Skeleton` from `@/components/ui/states` for
the three non-happy states, and `usePoll(path, ms)` from `@/lib/api` for data.
Concurrent polls of the same path share one request.

### Optional parts

- `overviewTile` and `tileRank`: a component, usually a `StatTile` from
  `@/components/StatTile`, whose `onOpen` calls `open("<id>")`, and its
  place on Home (lower first). Home shows at most four tiles, only of
  modules that are **live**: added, and for an extension that `requires`
  docker, with the engine up. Prefer a tile that reads what Home already
  fetches; every tile is another request on the first page people see.
- `useCommands(paletteOpen)`: a hook returning palette commands. It runs on
  every render for every module, in a fixed order, so fetch only while
  `paletteOpen` is true. A command with `children` opens a nested list.
- `useAttention(live)`: a hook returning Home's "Needs your move" entries
  (`AttentionItem[]`, or `null` until its first load answers). It runs on
  every Home render for every module, in a fixed order, and `live` says
  whether the module is live: when it is false the hook must fetch nothing
  (pass a `null` path to `usePoll`) and return `[]`, so a module that is not
  added, or whose docker engine is down, costs no request and no 503. Each
  entry has a `severity` (`danger`, `warning` or `info`) and a `kind`:
  `move` (the default: the user's own next step) or `infra` (what the
  machine needs: docker down, a runner offline). Home lists every move
  before any infra entry, each part most urgent first.
- `requires`: `clis` (an entry `"a|b"` is satisfied by either), `mcp`
  (matched against the MCP servers page), `env` (presence of a variable,
  never its value), `docker`, `anyOf` (any one requirement is enough) and
  `optional.clis`. The gallery shows each as a found or missing pill; unmet
  requirements do not block adding.
- `keywords`: extra words the palette matches.

### Engine side

If the module needs data, add a package under `internal/` with a
`Register(mux, ...)` function and wire it in `internal/server/server.go`.
Every request that changes something must require the `X-Lucid-Confirm: yes`
header (`apiutil.Confirmed`); the UI's `postAction` and `sendJSON` send it.
Never return secret values, environment variables or host paths the user did
not ask for.

## Themes

Themes are data, not code: `<data dir>/themes/<id>/theme.json` plus optional
SVG, PNG, WebP or GIF art in the same folder. A theme may only set the design
tokens in `web/src/index.css` (listed in `internal/themes/theme.go`), and each
value is checked as a colour, shadow, length or font stack. See
[CONFIG.md](CONFIG.md#themes-and-ui-prefs).

A page that loads, works, thinks or celebrates should show
`<StateSprite state="…" />` from `@/components/StateSprite`: the active
theme's picture for that state, or Lumi when it has none
([state sprites](CONFIG.md#state-sprites)). `LoadingArt` from
`@/components/ui/states` is a loading area with the sprite and a line of text,
and `celebrate({key, title, detail})` from `@/components/Celebrate` celebrates
something once.

## Sections

The second way to customise, after themes and before writing a module: a
section is a widget on Home or on every project's page, kept as JSON
in `<data dir>/sections/`. It names one route from an allowlist of read-only
Lucidbench GET routes, the rows and fields to show from its answer, and a view
(`stat`, `list`, `table`, `bars` or `markdown`). It carries no code, no URL and
no markup, so a section can be shared, generated by a model or hand-edited
without being able to do anything a page of Lucidbench could not already do.
The format and the rules are in [CONFIG.md](CONFIG.md#sections).

- **Engine:** `internal/sections`: `APIs` is the allowlist, `Validate` the
  rules, `Templates` the built-ins, and the generator's system prompt is
  `internal/sections/prompts/section.md` (versioned in its first line).
- **Web:** `SectionRenderer` (`@/components/SectionRenderer`) draws any
  section, fetching its route with `usePoll` every `refresh_s`;
  `SectionGrid` lays out the sections for one placement with an **Add
  section** button. `@/lib/sections` evaluates rows, filters, sorts and
  formats; every value is rendered as text.
- **Adding a route to the allowlist:** it must be a GET that changes nothing,
  costs nothing and returns nothing secret. Add it to `APIs` with its
  params, describe its answer in `prompts/section.md` (bump the version), and
  `TestPromptListsTheAllowlist` keeps the two in step.
- **A new built-in template:** append to `Templates`; `TestTemplatesAreValid`
  checks it.

A feature that needs code is the third tier: **Describe a feature** in
Settings › Customise (or the palette) turns the description into a Council
braindump for the Lucidbench repository or your fork, and the normal loop
takes it from there. Nothing is ever injected into the running app.

## Shortcuts and undo

Single-key shortcuts live in `web/src/components/Shortcuts.tsx` (`?` lists
them). They are ignored while the user types in an input, a textarea or the
Memory editor, and while a dialog is open. A reversible action should act at
once and offer Undo in its toast (sonner's `action`) rather than ask first.

## End-to-end tests

```sh
cd e2e
npm ci
npx playwright install chromium   # once
npm run e2e                       # builds lucidd and the fake CLIs, then runs the suite
```

The suite needs Go, Node and git, and port 7466 free. It builds `web/dist` only
when it is missing; rebuild the web UI after changing it. `E2E_SKIP_BUILD=1`
reuses the last lucidd (which embeds the web UI as it was when built), and
`E2E_KEEP=1` keeps each run's temporary folder. Screenshots a test takes go to
`e2e/shots/` (not committed). Add a fake answer to `e2e/fakecli/main.go`
rather than ever letting a real CLI run.

## Not supported yet: third-party extensions

Every extension ships inside Lucidbench and is reviewed with it. Installing
extensions at runtime (from a folder or a registry) is a future design, not a
feature: it needs a sandbox for untrusted UI code (an iframe with a strict
CSP and a message API rather than direct access to the app), a signed
manifest, and a permission model for the engine routes an extension may
call. Until then, add extensions by contributing a module file.
