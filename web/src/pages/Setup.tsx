import { useEffect, useMemo, useState, type ReactNode } from "react"
import {
  ArrowLeft,
  ArrowRight,
  ArrowUp,
  Check,
  CheckCircle2,
  CircleDashed,
  Folder,
  FolderGit2,
  FolderOpen,
  FolderPlus,
  Gauge,
  Library,
  Palette,
  PenLine,
  Search,
  Sparkles,
  Users,
  X,
  type LucideIcon,
} from "lucide-react"
import { toast } from "sonner"

import { CopyCommand, copyText } from "@/components/CopyCommand"
import { LogoMark } from "@/components/Logo"
import { ProviderTile, PROVIDERS } from "@/components/ProviderMark"
import { StateSprite } from "@/components/StateSprite"
import { Button } from "@/components/ui/button"
import { ConfirmDialog, type ConfirmRequest } from "@/components/ui/confirm"
import { ErrorState, Skeleton } from "@/components/ui/states"
import { ApiError, errorMessage, getJSON, request, usePoll } from "@/lib/api"
import { useApp } from "@/lib/app"
import { memoryApi, type VaultInfo } from "@/lib/memory"
import { usePrefs, type Prefs } from "@/lib/prefs"
import { putHandoff } from "@/lib/prompts"
import {
  SAMPLE_BRAINDUMP,
  SETUP_DONE_KEY,
  SETUP_ROUTE,
  setupApi,
  type AddResult,
  type Candidate,
  type DirListing,
  type NotAppendable,
} from "@/lib/setup"
import { resolveThemeId, SYSTEM_THEME } from "@/lib/theme"
import { cn, plural } from "@/lib/utils"
import type { Account } from "@/pages/Accounts"
import { ThemeCard } from "@/pages/settings/Appearance"
import { MODES, snippet, type PowerConfig } from "@/pages/settings/Infrastructure"
import { Segmented } from "@/pages/settings/controls"

interface Step {
  id: string
  title: string
  icon: LucideIcon
  blurb: string
}

const STEPS: Step[] = [
  { id: "accounts", title: "Accounts", icon: Users, blurb: "Which AI accounts this machine can use." },
  { id: "memory", title: "Memory vault", icon: Library, blurb: "Where your briefs, notes and boards live." },
  { id: "projects", title: "Projects", icon: FolderGit2, blurb: "The repositories Lucidbench should know about." },
  { id: "theme", title: "Theme", icon: Palette, blurb: "How the whole app looks." },
  { id: "power", title: "Power modes", icon: Gauge, blurb: "When the cluster and runners run." },
  { id: "try", title: "Try it", icon: Sparkles, blurb: "A first braindump for the council." },
]

const PROJECT_TYPES = ["game", "web-app", "desktop-app", "cli", "library", "service", "ml-research", "site", "video-system"]

interface ConfigView {
  vault: { path: string }
  power: PowerConfig
  sources: Record<string, string>
}

interface About {
  data_dir: string
  config_file: string
}

/** Makes sure ui.json exists, so setup does not open by itself again. */
async function markDone(prefs: Prefs, loaded: boolean) {
  // Before the saved prefs have loaded, save what the daemon has, not the defaults.
  if (!loaded) prefs = await getJSON<Prefs>("/api/prefs").catch(() => prefs)
  try {
    localStorage.setItem(SETUP_DONE_KEY, "1")
  } catch {
    /* the daemon's ui.json is the real record */
  }
  await request("/api/prefs", {
    method: "PUT",
    headers: { "Content-Type": "application/json", "X-Lucid-Confirm": "yes" },
    body: JSON.stringify(prefs),
  }).catch((e) => toast.error("Could not save your settings", { description: errorMessage(e) }))
}

/**
 * First-run setup: six calm steps, each skippable, that leave Lucidbench
 * ready for the loop. Nothing is written without a click, projects.yaml is
 * only appended to, and config.yaml is never written: the vault and power
 * steps show the lines to paste.
 */
export default function Setup({ subpath }: { subpath: string[] }) {
  const { navigate, open } = useApp()
  const { prefs, loaded } = usePrefs()
  const index = Math.max(0, STEPS.findIndex((s) => s.id === subpath[0]))
  const step = STEPS[index]
  const go = (i: number) => navigate(`${SETUP_ROUTE}/${STEPS[Math.min(STEPS.length - 1, Math.max(0, i))].id}`)

  useEffect(() => {
    document.title = `Setup · ${step.title} · Lucidbench`
  }, [step])

  const finish = async (then?: () => void) => {
    await markDone(prefs, loaded)
    if (then) then()
    else {
      navigate("/")
      toast.success("You're all set", { description: "Press ? any time for keyboard shortcuts." })
    }
  }

  const tryCouncil = () =>
    void finish(() => {
      putHandoff({ to: "council", text: SAMPLE_BRAINDUMP, from: "palette" })
      open("council")
    })

  let body: ReactNode
  switch (step.id) {
    case "accounts":
      body = <AccountsStep />
      break
    case "memory":
      body = <MemoryStep />
      break
    case "projects":
      body = <ProjectsStep />
      break
    case "theme":
      body = <ThemeStep />
      break
    case "power":
      body = <PowerStep />
      break
    default:
      body = <TryStep onTry={tryCouncil} />
  }
  const last = index === STEPS.length - 1

  return (
    <div className="app-backdrop min-h-screen overflow-y-auto" data-testid="setup">
      <div className="mx-auto flex max-w-5xl flex-col gap-6 px-5 py-8 md:px-8">
        <header className="flex items-center justify-between gap-4">
          <span className="flex items-center gap-2.5">
            <LogoMark />
            <span className="text-[15px] font-semibold tracking-[-0.02em]">
              Lucid<span className="text-muted-foreground">bench</span>
            </span>
            <span className="ml-1 rounded-full border px-2 text-2xs text-subtle-foreground">setup</span>
          </span>
          <Button variant="ghost" size="sm" onClick={() => void finish()} data-testid="setup-skip-all">
            Skip setup <X />
          </Button>
        </header>

        <div className="grid gap-6 md:grid-cols-[15rem_minmax(0,1fr)]">
          <aside className="space-y-5">
            <div className="flex items-center gap-3 rounded-2xl border bg-card/60 p-3 shadow-card">
              <StateSprite state={last ? "celebrate" : index === 0 ? "idle" : "working"} className="size-14" />
              <p className="text-xs text-muted-foreground">
                {index === 0 ? "Hi! A few quick questions and you are in. Skip anything you like." : last ? "One last, optional thing." : "Nothing is written until you say so."}
              </p>
            </div>
            <ol aria-label="Setup steps" className="space-y-0.5">
              {STEPS.map((s, i) => {
                const Icon = s.icon
                const done = i < index
                const current = i === index
                return (
                  <li key={s.id}>
                    <button
                      type="button"
                      onClick={() => go(i)}
                      aria-current={current ? "step" : undefined}
                      className={cn(
                        "flex w-full items-center gap-2.5 rounded-lg px-2.5 py-2 text-left text-sm outline-none transition-colors focus-visible:ring-2 focus-visible:ring-ring",
                        current ? "bg-accent font-medium text-foreground" : "text-muted-foreground hover:bg-accent/50 hover:text-foreground",
                      )}
                    >
                      <span
                        className={cn(
                          "flex size-6 shrink-0 items-center justify-center rounded-full border text-2xs",
                          current && "border-brand bg-brand-soft text-brand-fg",
                          done && "border-success/50 bg-success-soft text-success-fg",
                        )}
                      >
                        {done ? <Check className="size-3.5" /> : <Icon className="size-3.5" />}
                      </span>
                      {s.title}
                    </button>
                  </li>
                )
              })}
            </ol>
          </aside>

          <main key={step.id} className="min-w-0 animate-in fade-in-0 slide-in-from-bottom-1 duration-300">
            <div className="rounded-2xl border bg-card shadow-card">
              <div className="border-b px-6 pb-4 pt-5">
                <p className="text-xs font-medium text-subtle-foreground">
                  Step {index + 1} of {STEPS.length}
                </p>
                <h1 className="mt-0.5 text-2xl font-semibold tracking-[-0.02em]">{step.title}</h1>
                <p className="mt-0.5 text-sm text-muted-foreground">{step.blurb}</p>
              </div>
              <div className="px-6 py-5">{body}</div>
              <footer className="flex items-center justify-between gap-3 border-t px-6 py-4">
                <Button variant="ghost" size="sm" onClick={() => go(index - 1)} disabled={index === 0}>
                  <ArrowLeft /> Back
                </Button>
                <div className="flex items-center gap-2">
                  {!last && (
                    <Button variant="ghost" size="sm" onClick={() => go(index + 1)} data-testid="setup-skip">
                      Skip this step
                    </Button>
                  )}
                  {last ? (
                    <Button size="sm" onClick={() => void finish()} data-testid="setup-finish">
                      Finish <Check />
                    </Button>
                  ) : (
                    <Button size="sm" onClick={() => go(index + 1)} data-testid="setup-next">
                      Next <ArrowRight />
                    </Button>
                  )}
                </div>
              </footer>
            </div>
          </main>
        </div>
      </div>
    </div>
  )
}

/* ---------- accounts ---------- */

function AccountsStep() {
  const { data, error, refresh } = usePoll<Account[]>("/api/accounts", 5000)
  if (error && !data) return <ErrorState title="Could not detect accounts" message={error.message} onRetry={refresh} />
  if (!data) return <Skeleton className="h-48 rounded-xl" />
  return (
    <div className="space-y-4">
      <p className="text-sm text-muted-foreground">
        Lucidbench runs your own signed-in CLIs. It never stores a password or token, and only checks whether each one is signed in. One account is
        enough to start; the council is better with two or three.
      </p>
      <ul className="grid gap-2 sm:grid-cols-2">
        {PROVIDERS.map((p, i) => {
          const mine = data.filter((a) => a.provider === p.id)
          const ok = mine.some((a) => a.status === "logged_in")
          return (
            <li key={p.id} style={{ "--i": i } as React.CSSProperties} className="lb-rise rounded-xl border bg-background/40 p-3" data-testid={`setup-account-${p.id}`}>
              <div className="flex items-center gap-2.5">
                <ProviderTile provider={p.id} size="sm" />
                <div className="min-w-0 flex-1">
                  <div className="text-sm font-medium">{p.label}</div>
                  <div className="text-xs text-muted-foreground">{p.vendor}</div>
                </div>
                {ok ? (
                  <span className="flex items-center gap-1 text-xs font-medium text-success-fg">
                    <CheckCircle2 className="size-3.5" /> {mine.length > 1 ? plural(mine.length, "profile") : "Signed in"}
                  </span>
                ) : (
                  <span className="flex items-center gap-1 text-xs text-subtle-foreground">
                    <CircleDashed className="size-3.5" /> Not found
                  </span>
                )}
              </div>
              {!ok && (
                <div className="mt-2.5 space-y-1.5 text-xs text-muted-foreground">
                  {p.hostLogin ? (
                    <>
                      <div>Install the {p.label} CLI, then sign in from a terminal:</div>
                      <CopyCommand command={p.hostLogin} />
                    </>
                  ) : (
                    <div>Sign in from the {p.label} app; Lucidbench picks it up by itself.</div>
                  )}
                </div>
              )}
            </li>
          )
        })}
      </ul>
      <p className="text-xs text-subtle-foreground">Signed in somewhere new? It shows up here within a few seconds.</p>
    </div>
  )
}

/* ---------- folder picker ---------- */

/** Browses folders on this machine (names only) and picks one. */
function FolderPicker({ onPick, pickLabel, testid }: { onPick: (path: string) => void; pickLabel: string; testid: string }) {
  const [path, setPath] = useState("")
  const [typed, setTyped] = useState("")
  const [list, setList] = useState<DirListing | null>(null)
  const [error, setError] = useState<string | null>(null)
  const load = async (p: string) => {
    try {
      const l = await setupApi.dirs(p)
      setList(l)
      setPath(l.path)
      setTyped(l.path)
      setError(null)
    } catch (e) {
      setError(errorMessage(e))
    }
  }
  useEffect(() => void load(""), [])
  return (
    <div className="overflow-hidden rounded-xl border" data-testid={testid}>
      <form
        className="flex items-center gap-2 border-b bg-muted/30 p-2"
        onSubmit={(e) => {
          e.preventDefault()
          void load(typed)
        }}
      >
        <Button type="button" variant="ghost" size="icon-sm" aria-label="Up one folder" disabled={!list?.parent} onClick={() => list?.parent && void load(list.parent)}>
          <ArrowUp />
        </Button>
        <input
          value={typed}
          onChange={(e) => setTyped(e.target.value)}
          aria-label="Folder path"
          spellCheck={false}
          className="h-7 min-w-0 flex-1 rounded-md border bg-background px-2 font-mono text-xs outline-none focus-visible:ring-2 focus-visible:ring-ring"
        />
        <Button type="submit" variant="secondary" size="sm">
          Go
        </Button>
      </form>
      {error && <p className="border-b bg-danger-soft px-3 py-2 text-xs text-danger-fg">{error}</p>}
      <ul className="max-h-56 overflow-y-auto py-1" aria-label="Folders">
        {!list && !error && (
          <li className="space-y-1.5 p-2">
            <Skeleton className="h-6" />
            <Skeleton className="h-6" />
          </li>
        )}
        {list?.dirs.length === 0 && <li className="px-3 py-3 text-xs text-muted-foreground">No folders in here.</li>}
        {list?.dirs.map((d) => (
          <li key={d.path}>
            <button
              type="button"
              onClick={() => void load(d.path)}
              className="flex w-full items-center gap-2 px-3 py-1.5 text-left text-sm hover:bg-accent/50 focus-visible:bg-accent/50 focus-visible:outline-none"
            >
              {d.git ? <FolderGit2 className="size-4 text-brand" /> : <Folder className="size-4 text-subtle-foreground" />}
              <span className="flex-1 truncate">{d.name}</span>
              {d.git && <span className="text-2xs text-subtle-foreground">git</span>}
            </button>
          </li>
        ))}
      </ul>
      <div className="flex items-center justify-between gap-2 border-t bg-muted/30 px-3 py-2">
        <span className="min-w-0 truncate font-mono text-2xs text-muted-foreground" title={path}>
          {path}
        </span>
        <Button size="sm" onClick={() => path && onPick(path)} disabled={!path} data-testid={`${testid}-pick`}>
          <FolderOpen /> {pickLabel}
        </Button>
      </div>
    </div>
  )
}

/* ---------- memory ---------- */

function Choice({ selected, onSelect, icon: Icon, title, hint, testid }: { selected: boolean; onSelect: () => void; icon: LucideIcon; title: string; hint: string; testid: string }) {
  return (
    <button
      type="button"
      role="radio"
      aria-checked={selected}
      onClick={onSelect}
      data-testid={testid}
      className={cn(
        "flex items-start gap-3 rounded-xl border bg-background/40 p-3 text-left outline-none transition-[border-color,box-shadow] hover:border-border-strong focus-visible:ring-2 focus-visible:ring-ring",
        selected && "border-brand/60 shadow-[0_0_0_1px_var(--brand)]",
      )}
    >
      <span className="flex size-8 shrink-0 items-center justify-center rounded-lg bg-brand-soft text-brand-fg">
        <Icon className="size-4" />
      </span>
      <span>
        <span className="block text-sm font-medium">{title}</span>
        <span className="block text-xs text-muted-foreground">{hint}</span>
      </span>
    </button>
  )
}

function MemoryStep() {
  const cfg = usePoll<ConfigView>("/api/config", 60000)
  const about = usePoll<About>("/api/about", 60000)
  const [mode, setMode] = useState<"new" | "existing">("new")
  const [created, setCreated] = useState<VaultInfo | null>(null)
  const [busy, setBusy] = useState(false)
  const [picked, setPicked] = useState<string | null>(null)
  const configured = cfg.data?.vault.path
  const dataDir = about.data?.data_dir?.replace(/[\\/]+$/, "")
  const sep = dataDir?.includes("\\") ? "\\" : "/"

  const create = async () => {
    setBusy(true)
    try {
      setCreated(await memoryApi.info())
    } catch (e) {
      toast.error("Could not create the vault", { description: errorMessage(e) })
    } finally {
      setBusy(false)
    }
  }

  if (configured) {
    return (
      <div className="flex items-start gap-3 rounded-xl border bg-success-soft/40 p-4 text-sm">
        <CheckCircle2 className="mt-0.5 size-4 text-success" />
        <div>
          <div className="font-medium">Memory already uses a vault you chose</div>
          <div className="mt-0.5 break-all font-mono text-xs text-muted-foreground">{configured}</div>
          <div className="mt-1 text-xs text-muted-foreground">Change it any time in Settings › General.</div>
        </div>
      </div>
    )
  }
  return (
    <div className="space-y-4">
      <div role="radiogroup" aria-label="Memory vault" className="grid gap-2 sm:grid-cols-2">
        <Choice selected={mode === "new"} onSelect={() => setMode("new")} icon={FolderPlus} title="Create a new vault" hint="Recommended. A fresh folder in Lucidbench's data folder." testid="setup-vault-new" />
        <Choice selected={mode === "existing"} onSelect={() => setMode("existing")} icon={FolderOpen} title="Use a folder I already have" hint="Any Obsidian vault or folder of Markdown." testid="setup-vault-existing" />
      </div>
      {mode === "new" ? (
        <div className="space-y-3 rounded-xl border bg-background/40 p-4">
          <div className="text-sm">
            Pages go in <span className="break-all font-mono text-xs">{dataDir ? `${dataDir}${sep}memory` : "the data folder"}</span>. It opens in Obsidian too.
          </div>
          {created ? (
            <div className="flex items-center gap-2 text-sm text-success-fg" data-testid="setup-vault-ready">
              <CheckCircle2 className="size-4" /> Your vault is ready{created.pages ? ` with ${plural(created.pages, "page")}` : ""}.
            </div>
          ) : (
            <Button size="sm" onClick={() => void create()} disabled={busy} data-testid="setup-vault-create">
              <FolderPlus /> {busy ? "Creating" : "Create it now"}
            </Button>
          )}
        </div>
      ) : picked ? (
        <div className="space-y-3 rounded-xl border bg-background/40 p-4 text-sm">
          <p>
            Lucidbench never edits your config file by itself. To use <span className="break-all font-mono text-xs">{picked}</span>, add this to it and restart:
          </p>
          <pre className="overflow-x-auto rounded-lg border bg-background px-3 py-2 font-mono text-xs" data-testid="setup-vault-snippet">{`vault:\n  path: "${picked.replace(/\\/g, "\\\\")}"`}</pre>
          <div className="flex flex-wrap items-center gap-2">
            <Button size="sm" variant="secondary" onClick={() => void copyText(`vault:\n  path: "${picked.replace(/\\/g, "\\\\")}"\n`, "Snippet copied")}>
              Copy snippet
            </Button>
            <Button size="sm" variant="ghost" onClick={() => setPicked(null)}>
              Pick another folder
            </Button>
          </div>
          <div className="text-xs text-muted-foreground">
            Your config file is {about.data?.config_file ? <span className="break-all font-mono">{about.data.config_file}</span> : "shown by"}:
            <CopyCommand command="lucid config path" className="mt-1.5" />
            Or set <code className="font-mono">LUCID_VAULT_PATH</code> instead. Nothing in the folder is copied or moved.
          </div>
        </div>
      ) : (
        <FolderPicker onPick={setPicked} pickLabel="Use this folder" testid="setup-vault-picker" />
      )}
    </div>
  )
}

/* ---------- projects ---------- */

interface Row extends Candidate {
  on: boolean
}

function ProjectsStep() {
  const [root, setRoot] = useState<string | null>(null)
  const [rows, setRows] = useState<Row[] | null>(null)
  const [scanning, setScanning] = useState(false)
  const [preview, setPreview] = useState<string | null>(null)
  const [previewError, setPreviewError] = useState<string | null>(null)
  const [result, setResult] = useState<AddResult | null>(null)
  const [refused, setRefused] = useState<NotAppendable | null>(null)
  const [confirm, setConfirm] = useState<ConfirmRequest | null>(null)
  const status = usePoll<{ projects_hint: string }>("/api/setup", 60000)

  const scan = async (path: string) => {
    setRoot(path)
    setScanning(true)
    setPreview(null)
    setResult(null)
    setRefused(null)
    try {
      const r = await setupApi.scan(path)
      setRows(r.repos.map((c) => ({ ...c, on: !c.existing })))
    } catch (e) {
      toast.error("Could not scan the folder", { description: errorMessage(e) })
    } finally {
      setScanning(false)
    }
  }
  const picked = useMemo(() => (rows ?? []).filter((r) => r.on && !r.existing), [rows])
  const entries = picked.map((r) => ({ id: r.id, name: r.name, local_path: r.local_path, type: r.type || undefined }))
  const key = JSON.stringify(entries)

  // The preview follows the selection.
  useEffect(() => {
    if (entries.length === 0) {
      setPreview(null)
      setPreviewError(null)
      return
    }
    let cancelled = false
    const t = setTimeout(() => {
      setupApi
        .preview(entries)
        .then((r) => {
          if (cancelled) return
          setPreview(r.snippet)
          setPreviewError(null)
        })
        .catch((e) => !cancelled && setPreviewError(errorMessage(e)))
    }, 250)
    return () => {
      cancelled = true
      clearTimeout(t)
    }
  }, [key]) // eslint-disable-line react-hooks/exhaustive-deps

  const set = (id: string, patch: Partial<Row>) => setRows((rs) => rs && rs.map((r) => (r.local_path === id ? { ...r, ...patch } : r)))

  const add = () =>
    setConfirm({
      title: `Add ${plural(entries.length, "project")} to projects.yaml?`,
      description: `They are appended to ${status.data?.projects_hint ?? "projects.yaml"}. The file is backed up first, and nothing already in it is changed.`,
      confirmLabel: "Add projects",
      run: async () => {
        try {
          const r = await setupApi.add(entries)
          setResult(r)
          toast.success(`Added ${plural(r.added.length, "project")}`, { description: r.backup ? `Backup: ${r.backup}` : "projects.yaml created" })
        } catch (e) {
          if (e instanceof ApiError && e.status === 409) {
            try {
              setRefused(JSON.parse(e.message) as NotAppendable)
              return
            } catch {
              /* fall through */
            }
          }
          toast.error("Could not add the projects", { description: errorMessage(e) })
        }
      },
    })

  if (result) {
    return (
      <div className="space-y-3" data-testid="setup-projects-done">
        <div className="flex items-start gap-3 rounded-xl border bg-success-soft/40 p-4 text-sm">
          <CheckCircle2 className="mt-0.5 size-4 text-success" />
          <div>
            <div className="font-medium">
              {plural(result.added.length, "project")} added to {result.path_hint}
            </div>
            <div className="mt-0.5 text-xs text-muted-foreground">
              {result.created ? "The file was created." : `The previous file is kept as ${result.backup}.`} Edit the file to set each project's category, visibility
              and more.
            </div>
          </div>
        </div>
        <Button variant="ghost" size="sm" onClick={() => setResult(null)}>
          Add more
        </Button>
      </div>
    )
  }

  return (
    <div className="space-y-4">
      <p className="text-sm text-muted-foreground">
        Pick the folder that holds your code. Lucidbench lists the git repositories in it and one or two levels down; tick the ones to add. They are added
        as <span className="font-mono text-xs">private</span>, <span className="font-mono text-xs">experiment</span> projects you can change later.
      </p>
      {!root || !rows ? (
        scanning ? (
          <div className="space-y-2">
            <Skeleton className="h-9" />
            <Skeleton className="h-9" />
            <Skeleton className="h-9" />
          </div>
        ) : (
          <FolderPicker onPick={(p) => void scan(p)} pickLabel="Scan this folder" testid="setup-projects-picker" />
        )
      ) : (
        <>
          <div className="flex items-center justify-between gap-2 text-xs text-muted-foreground">
            <span className="min-w-0 truncate">
              {plural(rows.length, "repository", "repositories")} in <span className="font-mono">{root}</span>
            </span>
            <Button variant="ghost" size="sm" onClick={() => setRows(null)}>
              <Search /> Another folder
            </Button>
          </div>
          {rows.length === 0 ? (
            <p className="rounded-xl border border-dashed px-4 py-6 text-center text-sm text-muted-foreground">
              No git repositories in this folder or two levels below it. Try the folder one level up.
            </p>
          ) : (
            <div className="overflow-hidden rounded-xl border">
              <table className="w-full table-fixed text-sm" data-testid="setup-repos">
                <colgroup>
                  <col className="w-10" />
                  <col />
                  <col className="w-36" />
                  <col className="w-36" />
                </colgroup>
                <thead className="bg-muted/40 text-left text-2xs uppercase tracking-[0.06em] text-subtle-foreground">
                  <tr>
                    <th className="w-8 px-3 py-2" />
                    <th className="px-2 py-2 font-medium">Name</th>
                    <th className="px-2 py-2 font-medium">Id</th>
                    <th className="px-2 py-2 font-medium">Type</th>
                  </tr>
                </thead>
                <tbody className="divide-y">
                  {rows.map((r, i) => (
                    <tr key={r.local_path} style={{ "--i": i } as React.CSSProperties} className={cn("lb-rise", r.existing && "opacity-60")}>
                      <td className="px-3 py-1.5">
                        <input
                          type="checkbox"
                          aria-label={`Add ${r.name}`}
                          checked={r.on && !r.existing}
                          disabled={!!r.existing}
                          onChange={(e) => set(r.local_path, { on: e.target.checked })}
                          className="size-4 accent-[var(--brand)]"
                        />
                      </td>
                      <td className="px-2 py-1.5">
                        <input
                          value={r.name}
                          disabled={!!r.existing}
                          aria-label={`Name of ${r.local_path}`}
                          onChange={(e) => set(r.local_path, { name: e.target.value })}
                          className="h-7 w-full rounded-md border border-transparent bg-transparent px-1.5 outline-none hover:border-border focus-visible:border-ring"
                        />
                        <div className="truncate px-1.5 font-mono text-2xs text-subtle-foreground" title={r.local_path}>
                          {r.existing ? `already listed as ${r.existing}` : r.local_path}
                        </div>
                      </td>
                      <td className="px-2 py-1.5">
                        <input
                          value={r.id}
                          disabled={!!r.existing}
                          aria-label={`Id of ${r.name}`}
                          onChange={(e) => set(r.local_path, { id: e.target.value.toLowerCase().replace(/[^a-z0-9-]/g, "-") })}
                          className="h-7 w-full rounded-md border border-transparent bg-transparent px-1.5 font-mono text-xs outline-none hover:border-border focus-visible:border-ring"
                        />
                      </td>
                      <td className="px-2 py-1.5">
                        <select
                          value={r.type}
                          disabled={!!r.existing}
                          aria-label={`Type of ${r.name}`}
                          title={r.why ? `Guessed from ${r.why}` : "No file gave the type away"}
                          onChange={(e) => set(r.local_path, { type: e.target.value })}
                          className="h-7 w-full rounded-md border bg-background px-1.5 text-xs outline-none focus-visible:ring-2 focus-visible:ring-ring"
                        >
                          <option value="">(none)</option>
                          {PROJECT_TYPES.map((t) => (
                            <option key={t} value={t}>
                              {t}
                            </option>
                          ))}
                        </select>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
          {refused ? (
            <div className="space-y-2 rounded-xl border border-warning/40 bg-warning-soft/40 p-4 text-sm" data-testid="setup-projects-refused">
              <div className="font-medium">projects.yaml was left as it is</div>
              <div className="text-xs text-muted-foreground">{refused.error}. Paste these lines at the end of its projects: list yourself:</div>
              <pre className="overflow-x-auto rounded-lg border bg-background px-3 py-2 font-mono text-xs">{refused.snippet}</pre>
              <Button size="sm" variant="secondary" onClick={() => void copyText(refused.snippet, "Snippet copied")}>
                Copy snippet
              </Button>
            </div>
          ) : (
            entries.length > 0 && (
              <div className="space-y-2">
                <div className="text-xs font-medium text-muted-foreground">These lines are appended to {status.data?.projects_hint ?? "projects.yaml"}:</div>
                {previewError ? (
                  <p className="rounded-lg border border-danger/30 bg-danger-soft px-3 py-2 text-xs text-danger-fg" role="alert">
                    {previewError}
                  </p>
                ) : (
                  <pre className="max-h-48 overflow-auto rounded-lg border bg-background px-3 py-2 font-mono text-xs" data-testid="setup-projects-preview">
                    {preview ?? "…"}
                  </pre>
                )}
                <Button size="sm" onClick={add} disabled={!!previewError || preview === null} data-testid="setup-projects-add">
                  <Check /> Add {plural(entries.length, "project")}
                </Button>
              </div>
            )
          )}
        </>
      )}
      <ConfirmDialog request={confirm} onClose={() => setConfirm(null)} />
    </div>
  )
}

/* ---------- theme ---------- */

function ThemeStep() {
  const { themes, prefs, update } = usePrefs()
  const chosen = prefs.theme === SYSTEM_THEME ? SYSTEM_THEME : resolveThemeId(prefs.theme)
  if (themes.length === 0) return <Skeleton className="h-48 rounded-xl" />
  return (
    <div className="space-y-3">
      <p className="text-sm text-muted-foreground">The whole app switches at once. More choices, your own accent and themes you describe live in Settings › Appearance.</p>
      <div className="grid grid-cols-2 gap-3 sm:grid-cols-3" data-testid="setup-themes">
        {themes.map((t, i) => (
          <div key={t.id} style={{ "--i": i } as React.CSSProperties} className="lb-rise">
            <ThemeCard theme={t} selected={chosen === t.id} onSelect={() => update((p) => ({ ...p, theme: t.id }))} />
          </div>
        ))}
      </div>
    </div>
  )
}

/* ---------- power ---------- */

function PowerStep() {
  const cfg = usePoll<ConfigView>("/api/config", 60000)
  const current = cfg.data?.power
  const [draft, setDraft] = useState<PowerConfig | null>(null)
  useEffect(() => {
    if (current && !draft) setDraft(current)
  }, [current, draft])
  if (cfg.error && !current) return <ErrorState title="Could not read the configuration" message={cfg.error.message} onRetry={cfg.refresh} />
  if (!draft || !current) return <Skeleton className="h-48 rounded-xl" />
  const changed = snippet(draft) !== snippet(current)
  return (
    <div className="space-y-4">
      <p className="text-sm text-muted-foreground">
        A local Kubernetes cluster and CI runner containers can use a lot of memory. On demand starts them when something needs them and puts them to sleep
        when idle. If you use neither, skip this.
      </p>
      <div className="divide-y rounded-xl border">
        <div className="flex flex-wrap items-center justify-between gap-3 px-4 py-3">
          <div>
            <div className="text-sm font-medium">Kubernetes cluster</div>
            <div className="text-xs text-muted-foreground">Started when you submit a job.</div>
          </div>
          <Segmented label="Cluster mode" value={draft.cluster} options={MODES} onChange={(cluster) => setDraft({ ...draft, cluster })} />
        </div>
        <div className="flex flex-wrap items-center justify-between gap-3 px-4 py-3">
          <div>
            <div className="text-sm font-medium">Runner containers</div>
            <div className="text-xs text-muted-foreground">Started when a run is queued for their repository.</div>
          </div>
          <Segmented label="Runner mode" value={draft.runners} options={MODES} onChange={(runners) => setDraft({ ...draft, runners })} />
        </div>
      </div>
      {changed ? (
        <div className="space-y-2 rounded-xl border bg-background/40 p-4 text-sm" data-testid="setup-power-snippet">
          <p>Lucidbench never edits your config file by itself. Paste this into it and restart:</p>
          <pre className="overflow-x-auto rounded-lg border bg-background px-3 py-2 font-mono text-xs">{snippet(draft)}</pre>
          <div className="flex flex-wrap items-center gap-2">
            <Button size="sm" variant="secondary" onClick={() => void copyText(`${snippet(draft)}\n`, "Snippet copied")}>
              Copy snippet
            </Button>
            <span className="text-xs text-muted-foreground">
              Find the file with <code className="font-mono">lucid config path</code>.
            </span>
          </div>
        </div>
      ) : (
        <p className="flex items-center gap-2 text-xs text-muted-foreground">
          <CheckCircle2 className="size-3.5 text-success" /> These are your current modes; nothing to change.
        </p>
      )}
    </div>
  )
}

/* ---------- try it ---------- */

function TryStep({ onTry }: { onTry: () => void }) {
  return (
    <div className="space-y-4">
      <p className="text-sm text-muted-foreground">
        The loop starts with a braindump: a messy paragraph about something you want. The council turns it into a brief with done criteria, and approving
        the brief puts a card on your board. Here is one to try; it opens in the council so you can read it first, and nothing runs until you press Start.
      </p>
      <blockquote className="whitespace-pre-line rounded-xl border bg-background/40 px-4 py-3 text-sm text-muted-foreground">{SAMPLE_BRAINDUMP}</blockquote>
      <Button onClick={onTry} data-testid="setup-try">
        <PenLine /> Open it in the council
      </Button>
      <p className="text-xs text-subtle-foreground">Optional. The council runs on your own accounts, so a run uses a little of your plan.</p>
    </div>
  )
}
