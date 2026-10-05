import { useEffect, useState, type ReactNode } from "react"
import {
  Activity,
  ChevronRight,
  Gauge,
  KanbanSquare,
  LayoutDashboard,
  Monitor,
  Moon,
  PanelLeftClose,
  PanelLeftOpen,
  RefreshCw,
  Search,
  ServerCog,
  Sun,
  Users,
  Vote,
  type LucideIcon,
} from "lucide-react"

import { Wordmark } from "@/components/Logo"
import { StatusPill } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { refreshAll } from "@/lib/api"
import type { Health } from "@/lib/health"
import type { ThemePref } from "@/lib/theme"
import { relativeTime, useNow } from "@/lib/time"
import { cn, isMac } from "@/lib/utils"

export type Page = "overview" | "accounts" | "runners" | "system"

const WORKSPACE: { id: Page; label: string; icon: LucideIcon }[] = [
  { id: "overview", label: "Overview", icon: LayoutDashboard },
  { id: "accounts", label: "Accounts", icon: Users },
  { id: "runners", label: "Runners & CI", icon: ServerCog },
  { id: "system", label: "System", icon: Activity },
]

const ROADMAP: { label: string; milestone: string; icon: LucideIcon }[] = [
  { label: "Council", milestone: "M1", icon: Vote },
  { label: "Usage", milestone: "M2", icon: Gauge },
  { label: "Boards", milestone: "M3", icon: KanbanSquare },
]

const SIDEBAR_KEY = "lucidbench.sidebar"

function useMedia(query: string): boolean {
  const [match, setMatch] = useState(() => matchMedia(query).matches)
  useEffect(() => {
    const mq = matchMedia(query)
    const on = () => setMatch(mq.matches)
    mq.addEventListener("change", on)
    return () => mq.removeEventListener("change", on)
  }, [query])
  return match
}

/** Collapsed state: the user's toggle on wide screens, always a rail below 1024px. */
export function useSidebar() {
  const wide = useMedia("(min-width: 1024px)")
  const [collapsed, setCollapsed] = useState(() => {
    try {
      return localStorage.getItem(SIDEBAR_KEY) === "collapsed"
    } catch {
      return false
    }
  })
  const toggle = () => {
    const next = !collapsed
    setCollapsed(next)
    try {
      localStorage.setItem(SIDEBAR_KEY, next ? "collapsed" : "expanded")
    } catch {
      // not persisted
    }
  }
  return { rail: collapsed || !wide, wide, toggle }
}

function SectionLabel({ rail, children }: { rail: boolean; children: ReactNode }) {
  if (rail) return <div className="mx-auto my-2 h-px w-6 bg-border" />
  return (
    <div className="px-2.5 pb-1 pt-4 text-2xs font-medium uppercase tracking-[0.08em] text-subtle-foreground">
      {children}
    </div>
  )
}

export function Sidebar({
  page,
  onNavigate,
  rail,
  wide,
  onToggle,
  version,
}: {
  page: Page
  onNavigate: (p: Page) => void
  rail: boolean
  wide: boolean
  onToggle: () => void
  version?: string
}) {
  const item = "group relative flex h-8 items-center gap-2.5 rounded-md text-sm transition-colors"
  return (
    <aside
      className={cn(
        "flex shrink-0 flex-col border-r bg-sidebar transition-[width] duration-200 ease-[var(--ease-out-soft)]",
        rail ? "w-14 px-2" : "w-60 px-3",
      )}
    >
      <div className={cn("flex h-14 items-center", rail ? "justify-center" : "px-1.5")}>
        <Wordmark collapsed={rail} />
      </div>

      <nav aria-label="Main" className="flex flex-1 flex-col">
        <SectionLabel rail={rail}>Workspace</SectionLabel>
        <div className="flex flex-col gap-0.5">
          {WORKSPACE.map(({ id, label, icon: Icon }) => {
            const active = page === id
            return (
              <button
                key={id}
                onClick={() => onNavigate(id)}
                aria-current={active ? "page" : undefined}
                title={rail ? label : undefined}
                aria-label={rail ? label : undefined}
                className={cn(
                  item,
                  rail ? "justify-center" : "px-2.5",
                  active
                    ? "bg-accent font-medium text-foreground shadow-[inset_0_0_0_1px_var(--border)]"
                    : "text-muted-foreground hover:bg-accent/60 hover:text-foreground",
                )}
              >
                <Icon className={cn("size-4 shrink-0", active ? "text-brand" : "text-subtle-foreground group-hover:text-foreground")} />
                {!rail && label}
              </button>
            )
          })}
        </div>

        <SectionLabel rail={rail}>Roadmap</SectionLabel>
        <div className="flex flex-col gap-0.5">
          {ROADMAP.map(({ label, milestone, icon: Icon }) => (
            <div
              key={label}
              role="link"
              aria-disabled="true"
              aria-label={`${label}, coming in ${milestone}`}
              title={`${label}: coming in ${milestone}`}
              className={cn(item, "cursor-not-allowed text-subtle-foreground", rail ? "justify-center" : "px-2.5")}
            >
              <Icon className="size-4 shrink-0 opacity-70" />
              {!rail && (
                <>
                  <span className="flex-1">{label}</span>
                  <span className="rounded border border-dashed border-border-strong px-1.5 text-2xs leading-4 text-subtle-foreground">
                    {milestone} · soon
                  </span>
                </>
              )}
            </div>
          ))}
        </div>
      </nav>

      <div className={cn("flex items-center border-t py-3", rail ? "justify-center" : "justify-between px-1")}>
        {!rail && version && <span className="font-mono text-2xs text-subtle-foreground">lucidd {version}</span>}
        {wide && (
          <button
            onClick={onToggle}
            aria-label={rail ? "Expand sidebar" : "Collapse sidebar"}
            title={rail ? "Expand sidebar" : "Collapse sidebar"}
            className="rounded-md p-1.5 text-subtle-foreground transition-colors hover:bg-accent hover:text-foreground"
          >
            {rail ? <PanelLeftOpen className="size-4" /> : <PanelLeftClose className="size-4" />}
          </button>
        )}
      </div>
    </aside>
  )
}

const THEMES: { id: ThemePref; label: string; icon: LucideIcon }[] = [
  { id: "light", label: "Light", icon: Sun },
  { id: "dark", label: "Dark", icon: Moon },
  { id: "system", label: "System", icon: Monitor },
]

function ThemeToggle({ pref, onChange }: { pref: ThemePref; onChange: (p: ThemePref) => void }) {
  return (
    <div role="radiogroup" aria-label="Theme" className="flex items-center rounded-lg border bg-muted/50 p-0.5">
      {THEMES.map(({ id, label, icon: Icon }) => {
        const on = pref === id
        return (
          <button
            key={id}
            role="radio"
            aria-checked={on}
            aria-label={`${label} theme`}
            title={`${label} theme`}
            onClick={() => onChange(id)}
            className={cn(
              "flex size-6 items-center justify-center rounded-md transition-colors",
              on ? "bg-elevated text-foreground shadow-card" : "text-subtle-foreground hover:text-foreground",
            )}
          >
            <Icon className="size-3.5" />
          </button>
        )
      })}
    </div>
  )
}

export const pageLabel = (p: Page) => WORKSPACE.find((w) => w.id === p)?.label ?? p
export const PAGES = WORKSPACE

export function Header({
  page,
  health,
  theme,
  onTheme,
  onSearch,
}: {
  page: Page
  health: Health | null | undefined
  theme: ThemePref
  onTheme: (p: ThemePref) => void
  onSearch: () => void
}) {
  const ok = health?.status === "ok"
  const title = pageLabel(page)
  return (
    <header className="flex h-14 shrink-0 items-center justify-between gap-4 border-b bg-background/70 px-5 backdrop-blur-md md:px-6">
      <nav aria-label="Breadcrumb" className="flex min-w-0 items-center gap-1.5 text-sm">
        <span className="text-subtle-foreground">Workspace</span>
        <ChevronRight className="size-3.5 shrink-0 text-subtle-foreground" />
        <span aria-current="page" className="truncate font-medium">
          {title}
        </span>
      </nav>
      <div className="flex items-center gap-3">
        <button
          onClick={onSearch}
          aria-label="Open command palette"
          aria-keyshortcuts={isMac() ? "Meta+K" : "Control+K"}
          className="flex h-7 w-60 items-center gap-2 whitespace-nowrap rounded-lg border bg-muted/40 pl-2.5 pr-1.5 text-xs text-subtle-foreground transition-colors hover:border-border-strong hover:text-muted-foreground max-[1100px]:w-auto"
        >
          <Search className="size-3.5" />
          <span className="flex-1 text-left max-[1100px]:sr-only">Search or run a command…</span>
          <kbd className="rounded border bg-background px-1 font-sans text-2xs leading-4">{isMac() ? "⌘K" : "Ctrl K"}</kbd>
        </button>
        {health === undefined ? (
          <StatusPill tone="neutral">Connecting</StatusPill>
        ) : (
          <StatusPill tone={ok ? "success" : "danger"} pulse={ok} className="max-sm:px-1.5">
            <span className="max-sm:sr-only">{ok ? "Daemon online" : "Daemon unreachable"}</span>
          </StatusPill>
        )}
        <ThemeToggle pref={theme} onChange={onTheme} />
      </div>
    </header>
  )
}

/** Page title block: an icon tile, title, description and actions on the right. */
export function PageHeader({
  icon,
  title,
  description,
  actions,
}: {
  icon?: ReactNode
  title: string
  description?: ReactNode
  actions?: ReactNode
}) {
  return (
    <div className="flex items-start justify-between gap-6">
      <div className="flex min-w-0 items-start gap-3.5">
        {icon && (
          <span className="relative mt-0.5 flex size-10 shrink-0 items-center justify-center overflow-hidden rounded-xl border border-border-strong bg-elevated text-brand shadow-card [&_svg]:size-[18px]">
            <span aria-hidden className="absolute inset-0 bg-gradient-to-b from-brand-soft to-transparent" />
            <span className="relative">{icon}</span>
          </span>
        )}
        <div className="min-w-0">
          <h1 className="text-2xl font-semibold tracking-[-0.02em]">{title}</h1>
          {description && <p className="mt-0.5 max-w-2xl text-sm text-muted-foreground">{description}</p>}
        </div>
      </div>
      {actions && <div className="flex shrink-0 items-center gap-2 pt-1">{actions}</div>}
    </div>
  )
}

/** Manual refresh for every poll on the page, with when it last succeeded. */
export function RefreshButton({ refreshing, updatedAt }: { refreshing: boolean; updatedAt: number | null }) {
  const now = useNow(5000)
  return (
    <div className="flex items-center gap-2">
      {updatedAt !== null && (
        <span className="text-xs tabular-nums text-subtle-foreground max-[1100px]:hidden">
          Updated {relativeTime(new Date(updatedAt).toISOString(), now)}
        </span>
      )}
      <Button variant="secondary" size="sm" onClick={refreshAll} aria-label="Refresh now" title="Refresh now">
        <RefreshCw className={cn(refreshing && "animate-spin")} />
        Refresh
      </Button>
    </div>
  )
}
