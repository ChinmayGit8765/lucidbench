import { Suspense, useCallback, useEffect, useMemo, useState } from "react"
import { Blocks, Compass, Plus } from "lucide-react"
import { Toaster as SonnerToaster, toast } from "sonner"

import { CommandPalette, useCommandPaletteHotkey, type Command } from "@/components/CommandPalette"
import { Header, Sidebar } from "@/components/Shell"
import { Button } from "@/components/ui/button"
import { Card } from "@/components/ui/card"
import { EmptyState, Skeleton } from "@/components/ui/states"
import { AppContext, useApp, type AppValue } from "@/lib/app"
import { useHealth } from "@/lib/health"
import { PrefsProvider, usePrefs } from "@/lib/prefs"
import { MODULES } from "@/modules"
import { isAdded, isOpenable, matchPath, moduleById, navOrder, pathFor } from "@/modules/registry"
import type { ModuleDef } from "@/modules/types"
import { AddAccountDialog } from "@/pages/Accounts"

/** Only ?theme= survives navigation; page-specific parameters do not. */
function keptSearch(): string {
  const theme = new URLSearchParams(location.search).get("theme")
  return theme ? `?theme=${encodeURIComponent(theme)}` : ""
}

export default function App() {
  return (
    <PrefsProvider>
      <Workbench />
    </PrefsProvider>
  )
}

function Workbench() {
  const [path, setPath] = useState(() => location.pathname)
  const navigate = useCallback((p: string) => {
    if (p !== location.pathname) history.pushState(null, "", `${p}${keptSearch()}`)
    setPath(p)
  }, [])
  useEffect(() => {
    const onPop = () => setPath(location.pathname)
    window.addEventListener("popstate", onPop)
    return () => window.removeEventListener("popstate", onPop)
  }, [])

  const { prefs, base } = usePrefs()
  const { module, subpath } = matchPath(path)
  const health = useHealth()
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
        navigate(`/settings/extensions/${m.id}`)
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
      navigate,
      open,
      openProject,
      focus,
      addAccount: () => setAdding(true),
      openPalette: () => setPalette(true),
    }),
    [health, navigate, open, openProject, focus],
  )

  useEffect(() => {
    document.title = `${module?.title ?? "Not found"} · Lucidbench`
  }, [module])

  return (
    <AppContext.Provider value={app}>
      <div className="app-backdrop flex h-screen overflow-hidden">
        <Sidebar current={module?.id} version={health?.version} />
        <div className="flex min-w-0 flex-1 flex-col">
          <Header module={module} health={health} />
          <main className="@container flex-1 overflow-auto">
            <div
              key={module?.id ?? "none"}
              className="mx-auto max-w-6xl px-5 py-7 animate-in fade-in-0 slide-in-from-bottom-1 duration-300 md:px-8"
            >
              <ModuleView module={module} subpath={subpath} />
            </div>
          </main>
        </div>
        <AddAccountDialog key={String(adding)} open={adding} onClose={() => setAdding(false)} />
        <Palette open={palette} onClose={() => setPalette(false)} current={module?.id} />
        <Toaster theme={base} />
      </div>
    </AppContext.Provider>
  )
}

function PageSkeleton() {
  return (
    <div className="space-y-6" aria-busy="true">
      <div className="flex items-start gap-3.5">
        <Skeleton className="size-10 rounded-xl" />
        <div className="space-y-2">
          <Skeleton className="h-7 w-48" />
          <Skeleton className="h-4 w-80" />
        </div>
      </div>
      <div className="grid grid-cols-3 gap-3">
        <Skeleton className="h-28 rounded-xl" />
        <Skeleton className="h-28 rounded-xl" />
        <Skeleton className="h-28 rounded-xl" />
      </div>
      <Skeleton className="h-64 rounded-xl" />
    </div>
  )
}

/** Renders a module, or says plainly why it cannot be shown. */
function ModuleView({ module, subpath }: { module?: ModuleDef; subpath: string[] }) {
  const { prefs, update } = usePrefs()
  const { open } = useApp()
  if (!module) {
    return (
      <Card className="border-dashed">
        <EmptyState icon={<Compass />} title="Nothing lives here" description="This address does not match any module. It may have been renamed.">
          <Button onClick={() => open("overview")}>Go to Overview</Button>
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
            Back to Overview
          </Button>
        </EmptyState>
      </Card>
    )
  }
  if (!isAdded(module, prefs)) {
    return (
      <Card className="border-dashed">
        <EmptyState icon={<Icon />} title={`${module.title} is not in your sidebar`} description={module.description}>
          <Button
            onClick={() =>
              update((p) => ({ ...p, extensions: { ...p.extensions, [module.id]: { added: true, order: p.extensions[module.id]?.order ?? module.order } } }))
            }
          >
            <Plus /> Add to sidebar
          </Button>
          <Button variant="secondary" onClick={() => open("settings", ["extensions", module.id])}>
            <Blocks /> Browse extensions
          </Button>
        </EmptyState>
      </Card>
    )
  }
  const Page = module.component
  if (!Page) return null
  return (
    <Suspense fallback={<PageSkeleton />}>
      <Page subpath={subpath} />
    </Suspense>
  )
}

/** The command palette: "Go to" for every openable module, then each module's own commands. */
function Palette({ open, onClose, current }: { open: boolean; onClose: () => void; current?: string }) {
  const { prefs } = usePrefs()
  const app = useApp()
  // Every module's hook runs on every render, in a fixed order.
  const perModule = MODULES.map((m) => ({ m, commands: m.useCommands ? m.useCommands(open) : [] }))
  const goTo: Command[] = navOrder(prefs)
    .filter((m) => isOpenable(m, prefs))
    .map((m) => ({
      id: `go-${m.id}`,
      label: m.title,
      group: "Go to",
      icon: m.icon,
      hint: current === m.id ? "current page" : undefined,
      keywords: m.keywords ?? "",
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
