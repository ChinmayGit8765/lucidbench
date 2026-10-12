import { lazy, Suspense, useCallback, useEffect, useMemo, useState } from "react"
import { Blocks, Compass, Plus } from "lucide-react"
import { Toaster as SonnerToaster, toast } from "sonner"

import { CelebrationWatcher, Celebrations } from "@/components/Celebrate"
import { CommandPalette, useCommandPaletteHotkey, type Command } from "@/components/CommandPalette"
import { DockerDown } from "@/components/DockerDown"
import { ShortcutSheet, useShortcuts } from "@/components/Shortcuts"
import { FamilyTabs, Header, PageHeader, Sidebar } from "@/components/Shell"
import { UpdateBanner } from "@/components/UpdateBanner"
import { Button } from "@/components/ui/button"
import { Card } from "@/components/ui/card"
import { EmptyState, LoadingArt, Skeleton } from "@/components/ui/states"
import { getJSON } from "@/lib/api"
import { AppContext, useApp, type AppValue } from "@/lib/app"
import { DEFAULT_BOARD, type BoardSummary } from "@/lib/boards"
import { newBraindump } from "@/lib/council"
import { useHealth } from "@/lib/health"
import { PrefsProvider, usePrefs } from "@/lib/prefs"
import { SETUP_PATH, SETUP_ROUTE, type SetupStatus } from "@/lib/setup"
import { MODULES } from "@/modules"
import { isAdded, isOpenable, matchPath, moduleById, navOrder, navOwner, needsDocker, pathFor, redirectPath } from "@/modules/registry"
import type { ModuleDef } from "@/modules/types"

const Setup = lazy(() => import("@/pages/Setup"))
// Loaded on first use, so the Accounts page stays in its own chunk.
const AddAccountDialog = lazy(() => import("@/components/AddAccountDialog"))

/** Only ?theme= survives navigation; page-specific parameters do not. */
function keptSearch(): string {
  const theme = new URLSearchParams(location.search).get("theme")
  return theme ? `?theme=${encodeURIComponent(theme)}` : ""
}

/** Set once the daemon says first-run setup is done, so Home stops asking. */
const SETUP_DONE_KEY = "lucidbench.setup.done"

/** The address an old link should land on now: itself, or where it moved. */
function settle(p: string): string {
  return redirectPath(p) ?? p
}

export default function App() {
  return (
    <PrefsProvider>
      <Workbench />
    </PrefsProvider>
  )
}

function Workbench() {
  const [path, setPath] = useState(() => {
    // A bookmark of an address that moved is rewritten in place.
    const p = settle(location.pathname)
    if (p !== location.pathname) history.replaceState(null, "", `${p}${keptSearch()}`)
    return p
  })
  const navigate = useCallback((to: string, replace = false) => {
    const p = settle(to)
    if (p !== location.pathname) history[replace ? "replaceState" : "pushState"](null, "", `${p}${keptSearch()}`)
    setPath(p)
  }, [])
  useEffect(() => {
    const onPop = () => {
      const p = settle(location.pathname)
      if (p !== location.pathname) history.replaceState(null, "", `${p}${keptSearch()}`)
      setPath(p)
    }
    window.addEventListener("popstate", onPop)
    return () => window.removeEventListener("popstate", onPop)
  }, [])

  const { prefs, base, loaded } = usePrefs()
  const { module, subpath } = matchPath(path)
  // Whether docker is up rides on the health poll, and only while an added
  // extension needs it or the page open shows docker state.
  const wantDocker = needsDocker(prefs) || !!module?.requires?.docker || !!module?.checksDocker
  // The first check waits for ui.json, so it asks about docker only if it needs to.
  const health = useHealth(10000, wantDocker, loaded)
  const dockerUp = wantDocker ? health?.docker : undefined
  const [adding, setAdding] = useState(false)
  const [palette, setPalette] = useState(false)
  const togglePalette = useCallback(() => setPalette((o) => !o), [])
  useCommandPaletteHotkey(togglePalette)
  const [focus, setFocus] = useState<{ id: string; n: number } | null>(null)

  const open = useCallback(
    (id: string, sub: string[] = []) => {
      const m = moduleById(id)
      if (!m) return
      if (m.status === "soon") {
        toast(`${m.title} is coming${m.milestone ? ` in ${m.milestone}` : " soon"}`, { description: m.description })
        return
      }
      if (!isAdded(m, prefs)) {
        navigate(`/extensions/${m.id}`)
        return
      }
      navigate(pathFor(m, sub))
    },
    [prefs, navigate],
  )
  const openProject = useCallback(
    (id: string) => {
      navigate("/projects")
      setFocus((f) => ({ id, n: (f?.n ?? 0) + 1 }))
    },
    [navigate],
  )
  const app = useMemo<AppValue>(
    () => ({
      health,
      dockerUp,
      navigate,
      open,
      openProject,
      focus,
      addAccount: () => setAdding(true),
      openPalette: () => setPalette(true),
    }),
    [health, dockerUp, navigate, open, openProject, focus],
  )

  useEffect(() => {
    if (!path.startsWith(SETUP_ROUTE)) document.title = `${module?.title ?? "Not found"} · Lucidbench`
  }, [module, path])

  // First run: no ui.json and no projects.yaml yet opens setup, once, from
  // Home. Once the daemon has said setup is done, this browser stops asking.
  useEffect(() => {
    if (location.pathname !== "/" || localStorage.getItem(SETUP_DONE_KEY) === "1") return
    getJSON<SetupStatus>(SETUP_PATH)
      .then((s) => {
        if (s.needed) {
          if (location.pathname === "/") navigate(SETUP_ROUTE)
        } else {
          try {
            localStorage.setItem(SETUP_DONE_KEY, "1")
          } catch {
            // asked again next time
          }
        }
      })
      .catch(() => undefined)
  }, [navigate])

  const inSetup = path === SETUP_ROUTE || path.startsWith(`${SETUP_ROUTE}/`)
  const [help, setHelp] = useState(false)
  useShortcuts({
    enabled: !inSetup,
    onHelp: () => setHelp((h) => !h),
    onSearch: () => setPalette(true),
    onNew: () => newBraindump(open),
    onGo: (id) => open(id),
  })

  if (inSetup) {
    return (
      <AppContext.Provider value={app}>
        <Suspense fallback={<div className="app-backdrop min-h-screen" />}>
          <Setup subpath={path.split("/").filter(Boolean).slice(1)} />
        </Suspense>
        <Toaster theme={base} />
      </AppContext.Provider>
    )
  }

  return (
    <AppContext.Provider value={app}>
      <div className="app-backdrop flex h-screen overflow-hidden">
        <Sidebar current={navOwner(module)} version={health?.version} />
        <div className="flex min-w-0 flex-1 flex-col">
          <Header module={module} health={health} />
          <UpdateBanner />
          <main className="@container flex-1 overflow-auto">
            <div
              key={module?.id ?? "none"}
              className="mx-auto max-w-6xl px-5 py-7 animate-in fade-in-0 slide-in-from-bottom-1 duration-300 md:px-8"
            >
              <FamilyTabs module={module} />
              <ModuleView module={module} subpath={subpath} />
            </div>
          </main>
        </div>
        {adding && (
          <Suspense fallback={null}>
            <AddAccountDialog open onClose={() => setAdding(false)} />
          </Suspense>
        )}
        <Palette open={palette} onClose={() => setPalette(false)} current={module?.id} />
        <ShortcutSheet open={help} onClose={() => setHelp(false)} />
        <CelebrationWatcher />
        <Celebrations />
        <Toaster theme={base} />
      </div>
    </AppContext.Provider>
  )
}

/** What shows while a page's code loads: its real title (so every page has its h1 at once) over placeholders. */
function PageSkeleton({ title }: { title?: string }) {
  return (
    <div className="space-y-6" aria-busy="true">
      <div className="flex items-start gap-3.5">
        <Skeleton className="size-10 rounded-xl" />
        <div className="space-y-2">
          {title ? <h1 className="text-2xl font-semibold tracking-[-0.02em]">{title}</h1> : <Skeleton className="h-7 w-48" />}
          <Skeleton className="h-4 w-80" />
        </div>
      </div>
      <div className="grid grid-cols-3 gap-3">
        <Skeleton className="h-28 rounded-xl" />
        <Skeleton className="h-28 rounded-xl" />
        <Skeleton className="h-28 rounded-xl" />
      </div>
      <div className="relative h-64 overflow-hidden rounded-xl">
        <Skeleton className="absolute inset-0 rounded-xl" />
        <LoadingArt className="relative h-full" />
      </div>
    </div>
  )
}

/**
 * /boards with a single board has nothing to choose from: it goes straight
 * to that board (the Work tabs' Board). With more than one, the index shows.
 */
function BoardsIndex({ children }: { children: React.ReactNode }) {
  const { navigate } = useApp()
  const [many, setMany] = useState<boolean | null>(null)
  useEffect(() => {
    let alive = true
    getJSON<BoardSummary[]>("/api/boards").then(
      (l) => {
        if (!alive) return
        if (l.length <= 1) navigate(`/boards/${l[0]?.id ?? DEFAULT_BOARD}`, true)
        else setMany(true)
      },
      () => alive && setMany(true),
    )
    return () => {
      alive = false
    }
  }, [navigate])
  return many ? <>{children}</> : <PageSkeleton title="Boards" />
}

/** Renders a module, or says plainly why it cannot be shown. */
function ModuleView({ module, subpath }: { module?: ModuleDef; subpath: string[] }) {
  const { prefs, update } = usePrefs()
  const { open, dockerUp } = useApp()
  if (!module) {
    return (
      <Card className="border-dashed">
        <EmptyState icon={<Compass />} title="Nothing lives here" description="This address does not match any page. It may have been renamed.">
          <Button onClick={() => open("overview")}>Go to Home</Button>
        </EmptyState>
      </Card>
    )
  }
  const Icon = module.icon
  if (module.status === "soon") {
    return (
      <Card className="border-dashed">
        <EmptyState
          icon={<Icon />}
          title={`${module.title} is coming${module.milestone ? ` in ${module.milestone}` : " soon"}`}
          description={module.description}
        >
          <Button variant="secondary" onClick={() => open("overview")}>
            Back to Home
          </Button>
        </EmptyState>
      </Card>
    )
  }
  if (!isAdded(module, prefs)) {
    return (
      <Card className="border-dashed">
        <h1 className="sr-only">{module.title}</h1>
        <EmptyState icon={<Icon />} title={`${module.title} is not in your sidebar`} description={module.description}>
          <Button
            onClick={() =>
              update((p) => ({ ...p, extensions: { ...p.extensions, [module.id]: { added: true, order: p.extensions[module.id]?.order ?? module.order } } }))
            }
          >
            <Plus /> Add to sidebar
          </Button>
          <Button variant="secondary" onClick={() => open("extensions", [module.id])}>
            <Blocks /> Browse extensions
          </Button>
        </EmptyState>
      </Card>
    )
  }
  // A docker page opens only once the engine answers: until then nothing
  // of it mounts, so it polls nothing and logs no 503.
  if (module.requires?.docker && dockerUp !== true) {
    if (dockerUp === undefined) return <PageSkeleton title={module.title} />
    return (
      <div className="space-y-6">
        <PageHeader icon={<Icon />} title={module.title} description={module.description} />
        <DockerDown what={module.title} />
      </div>
    )
  }
  const Page = module.component
  if (!Page) return null
  const page = (
    <Suspense fallback={<PageSkeleton title={module.title} />}>
      <Page subpath={subpath} />
    </Suspense>
  )
  return module.id === "boards" && subpath.length === 0 ? <BoardsIndex>{page}</BoardsIndex> : page
}

/** The command palette: "Go to" for every openable module, sidebar or not, then each module's own commands. */
function Palette({ open, onClose, current }: { open: boolean; onClose: () => void; current?: string }) {
  const { prefs } = usePrefs()
  const app = useApp()
  // Every module's hook runs on every render, in a fixed order.
  const perModule = MODULES.map((m) => ({ m, commands: m.useCommands ? m.useCommands(open) : [] }))
  const goTo: Command[] = navOrder(prefs)
    // A child without a tab (Council) has no page to go to: its parent is the page.
    .filter((m) => isOpenable(m, prefs) && !(m.parent && !m.tab))
    .map((m) => ({
      id: `go-${m.id}`,
      label: m.parent ? `${moduleById(m.parent)?.title ?? m.parent} › ${m.title}` : m.title,
      group: "Go to",
      icon: m.icon,
      hint: current === m.id ? "current page" : undefined,
      keywords: `${m.title} ${m.keywords ?? ""}`,
      run: () => app.open(m.id),
    }))
  // Core modules always contribute (Settings carries "Add extension…"); extensions only once added.
  const own = perModule.filter(({ m }) => (m.kind === "core" ? m.status !== "soon" : isOpenable(m, prefs))).flatMap((x) => x.commands)
  return <CommandPalette open={open} onClose={onClose} commands={[...goTo, ...own]} />
}

function Toaster({ theme }: { theme: "dark" | "light" }) {
  return (
    <SonnerToaster
      theme={theme}
      position="bottom-right"
      toastOptions={{ className: "!font-sans !text-sm !rounded-lg !border-border-strong !shadow-pop" }}
      style={
        {
          "--normal-bg": "var(--elevated)",
          "--normal-text": "var(--foreground)",
          "--normal-border": "var(--border-strong)",
        } as React.CSSProperties
      }
    />
  )
}
