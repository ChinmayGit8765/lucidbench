# Extending Lucidbench

Every page in the UI is a **module**: one file in `web/src/modules/` that
describes it. The sidebar, the router, the command palette (`Ctrl K` / `⌘K`),
the Overview tiles and the Settings › Extensions gallery are all generated
from the list in `web/src/modules/index.ts` plus the user's prefs. Adding a
module is one file and one line.

## Core modules and extensions

| | Core module | Extension |
|---|---|---|
| `kind` | `"core"` | `"extension"` |
| In the sidebar | always; the user can reorder it | only once added in Settings › Extensions |
| `section` | `workspace`, `ai`, `infrastructure` (pinned to the bottom) or `settings` | ignored: extensions sit under "Extensions" |
| `defaultEnabled` | `true` | whether a new user has it added |
| Extra fields | | `category`, `requires` |

Make something core when every user needs it and it works with nothing but
the engine. Make it an extension when it depends on a tool, account or
service the user may not have (Docker, a cloud CLI, an MCP server).

Today's modules:

- **Workspace (core):** Overview, Work (soon), Projects, Memory (soon),
  Boards (soon)
- **AI (core):** Accounts, MCP servers, Council (soon), Usage (soon)
- **Bottom (core):** System, Settings
- **Extensions:** Runners & CI, Containers and Kubernetes (added by default),
  Cloud and Databases (added from the gallery), Linear and Trello (board connectors that need credentials),
  plus roadmap cards for Payments, Picture and Live browser

A module with `status: "soon"` shows in the sidebar or gallery but cannot be
opened or added. Opening an extension the user has not added leads to its
card in Settings › Extensions, so links between modules never dead-end.

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
  section: "workspace",
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

- `useApp()` (`@/lib/app`): `open(id, subpath?)`, `navigate(path)`,
  `health`, `addAccount()`, `openPalette()`.
- `usePrefs()` (`@/lib/prefs`): the active theme, the user's prefs and
  `label(key, fallback)` for theme-renamed strings.

Use `PageHeader` from `@/components/Shell` for the title block,
`EmptyState`, `ErrorState` and `Skeleton` from `@/components/ui/states` for
the three non-happy states, and `usePoll(path, ms)` from `@/lib/api` for data.
Concurrent polls of the same path share one request.

### Optional parts

- `overviewTile`: a component, usually a `StatTile` from
  `@/components/StatTile`, whose `onOpen` calls `open("<id>")`. It appears
  on the Overview in sidebar order while the module is in the sidebar.
- `useCommands(paletteOpen)`: a hook returning palette commands. It runs on
  every render for every module, in a fixed order, so fetch only while
  `paletteOpen` is true. A command with `children` opens a nested list.
- `useAttention()`: a hook returning Overview's "Needs attention" entries
  (`AttentionItem[]`, or `null` until its first load answers). Each entry has
  a `severity`: `danger` (something broke), `warning` (something will block
  you) or `info` (your move in the loop). Overview sorts all modules' entries
  by severity and shows only those of modules in the sidebar. Like
  `useCommands`, it runs on every Overview render for every module.
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
