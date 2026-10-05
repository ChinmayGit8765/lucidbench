import { useCallback, useEffect, useState } from "react"
import { Play, Plus, RefreshCw, RotateCw, SunMoon } from "lucide-react"
import { Toaster as SonnerToaster, toast } from "sonner"

import { CommandPalette, useCommandPaletteHotkey, type Command } from "@/components/CommandPalette"
import { Header, PAGES, pageLabel, Sidebar, useSidebar, type Page } from "@/components/Shell"
import { getJSON, refreshAll } from "@/lib/api"
import { failingRuns, rerunFailed, shortRepo, type CIRun, type CIRuns } from "@/lib/ci"
import { useHealth } from "@/lib/health"
import { runHelloJob } from "@/lib/jobs"
import { useTheme } from "@/lib/theme"
import Accounts, { AddAccountDialog } from "@/pages/Accounts"
import Overview from "@/pages/Overview"
import Runners from "@/pages/Runners"
import System from "@/pages/System"

const ROUTES: Record<string, Page> = { "/": "overview", "/accounts": "accounts", "/runners": "runners", "/system": "system" }
const pathFor = (p: Page) => (p === "overview" ? "/" : `/${p}`)
const pageFromPath = (): Page => ROUTES[location.pathname.replace(/\/+$/, "") || "/"] ?? "overview"

export default function App() {
  const [page, setPageState] = useState<Page>(pageFromPath)
  const setPage = useCallback((p: Page) => {
    // Keep ?theme= so a forced theme survives navigation.
    history.pushState(null, "", `${pathFor(p)}${location.search}`)
    setPageState(p)
  }, [])
  useEffect(() => {
    const onPop = () => setPageState(pageFromPath())
    window.addEventListener("popstate", onPop)
    return () => window.removeEventListener("popstate", onPop)
  }, [])
  useEffect(() => {
    document.title = `${pageLabel(page)} · Lucidbench`
  }, [page])

  const health = useHealth()
  const theme = useTheme()
  const sidebar = useSidebar()
  const [adding, setAdding] = useState(false)
  const [palette, setPalette] = useState(false)
  const togglePalette = useCallback(() => setPalette((o) => !o), [])
  useCommandPaletteHotkey(togglePalette)

  // The palette's "re-run last failed" needs fresh runs; fetch on open.
  const [lastFailed, setLastFailed] = useState<CIRun | null | undefined>(undefined)
  useEffect(() => {
    if (!palette) return
    let cancelled = false
    setLastFailed(undefined)
    getJSON<CIRuns>("/api/ci/runs")
      .then((r) => !cancelled && setLastFailed(failingRuns(r.runs)[0] ?? null))
      .catch(() => !cancelled && setLastFailed(null))
    return () => {
      cancelled = true
    }
  }, [palette])

  const commands: Command[] = [
    ...PAGES.map(
      (p): Command => ({
        id: `go-${p.id}`,
        label: p.label,
        group: "Go to",
        icon: p.icon,
        hint: page === p.id ? "current page" : undefined,
        keywords: p.id === "runners" ? "ci github actions workflow" : p.id === "system" ? "cluster jobs daemon" : "",
        run: () => setPage(p.id),
      }),
    ),
    {
      id: "hello",
      label: "Run hello job",
      group: "Actions",
      icon: Play,
      hint: "local cluster",
      keywords: "job kubernetes test",
      run: () => void runHelloJob(),
    },
    {
      id: "add-account",
      label: "Add account",
      group: "Actions",
      icon: Plus,
      keywords: "login profile claude codex grok",
      run: () => setAdding(true),
    },
    {
      id: "rerun-last-failed",
      label: "Re-run last failed run",
      group: "Actions",
      icon: RotateCw,
      disabled: !lastFailed,
      hint:
        lastFailed === undefined
          ? "checking…"
          : lastFailed
            ? `${lastFailed.name} · ${shortRepo(lastFailed.repo)}`
            : "nothing failing",
      keywords: "ci github retry",
      run: () => {
        if (lastFailed) void rerunFailed(lastFailed).then((ok) => ok && setTimeout(refreshAll, 1500))
      },
    },
    {
      id: "theme",
      label: theme.resolved === "dark" ? "Switch to light theme" : "Switch to dark theme",
      group: "Actions",
      icon: SunMoon,
      keywords: "toggle theme dark light appearance",
      run: () => theme.setPref(theme.resolved === "dark" ? "light" : "dark"),
    },
    {
      id: "refresh",
      label: "Refresh data",
      group: "Actions",
      icon: RefreshCw,
      keywords: "reload update",
      run: () => {
        refreshAll()
        toast.success("Refreshing")
      },
    },
  ]

  return (
    <div className="app-backdrop flex h-screen overflow-hidden">
      <Sidebar
        page={page}
        onNavigate={setPage}
        rail={sidebar.rail}
        wide={sidebar.wide}
        onToggle={sidebar.toggle}
        version={health?.version}
      />
      <div className="flex min-w-0 flex-1 flex-col">
        <Header
          page={page}
          health={health}
          theme={theme.pref}
          onTheme={theme.setPref}
          onSearch={() => setPalette(true)}
        />
        <main className="@container flex-1 overflow-auto">
          <div
            key={page}
            className="mx-auto max-w-6xl px-5 py-7 animate-in fade-in-0 slide-in-from-bottom-1 duration-300 md:px-8"
          >
            {page === "overview" && <Overview health={health} onNavigate={setPage} onAddAccount={() => setAdding(true)} />}
            {page === "accounts" && <Accounts onAdd={() => setAdding(true)} />}
            {page === "runners" && <Runners />}
            {page === "system" && <System health={health} />}
          </div>
        </main>
      </div>
      <AddAccountDialog key={String(adding)} open={adding} onClose={() => setAdding(false)} />
      <CommandPalette open={palette} onClose={() => setPalette(false)} commands={commands} />
      <Toaster theme={theme.resolved} />
    </div>
  )
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

