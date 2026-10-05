import { useEffect, useState, type ReactNode } from "react"
import { ChevronRight, Monitor, Moon, PanelLeftClose, PanelLeftOpen, RefreshCw, Search, Sun, type LucideIcon } from "lucide-react"

import { Wordmark } from "@/components/Logo"
import { StatusPill } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { refreshAll } from "@/lib/api"
import { useApp } from "@/lib/app"
import type { Health } from "@/lib/health"
import { usePrefs } from "@/lib/prefs"
import { assetURL, DARK_DEFAULT, LIGHT_DEFAULT, SYSTEM_THEME } from "@/lib/theme"
import { relativeTime, useNow } from "@/lib/time"
import { cn, isMac } from "@/lib/utils"
import { SECTION_LABEL, sidebarGroups } from "@/modules/registry"
import type { ModuleDef } from "@/modules/types"

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

/** Rail or expanded: the user's choice (saved in prefs) on wide screens, always a rail below 1024px. */
export function useSidebar() {
  const wide = useMedia("(min-width: 1024px)")
  const { prefs, update } = usePrefs()
  const collapsed = prefs.overrides.sidebar === "rail"
  const toggle = () =>
    update((p) => ({ ...p, overrides: { ...p.overrides, sidebar: p.overrides.sidebar === "rail" ? "expanded" : "rail" } }))
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

const ITEM = "group relative flex h-8 items-center gap-2.5 rounded-md text-sm transition-colors"

function NavItem({ m, active, rail }: { m: ModuleDef; active: boolean; rail: boolean }) {
  const { open } = useApp()
  const Icon = m.icon
  if (m.status === "soon") {
    const when = m.milestone ? `coming in ${m.milestone}` : "coming soon"
    return (
      <div
        role="link"
        aria-disabled="true"
        aria-label={`${m.title}, ${when}`}
        title={`${m.title}: ${when}${m.description ? `. ${m.description}` : ""}`}
        className={cn(ITEM, "cursor-not-allowed text-subtle-foreground", rail ? "justify-center" : "px-2.5")}
      >
        <Icon className="size-4 shrink-0 opacity-70" />
        {!rail && (
          <>
            <span className="flex-1 truncate">{m.title}</span>
            <span className="rounded border border-dashed border-border-strong px-1.5 text-2xs leading-4 text-subtle-foreground">
              {m.milestone ? `${m.milestone} · soon` : "soon"}
            </span>
          </>
        )}
      </div>
    )
  }
  return (
    <button
      onClick={() => open(m.id)}
      aria-current={active ? "page" : undefined}
      title={rail ? m.title : undefined}
      aria-label={rail ? m.title : undefined}
      className={cn(
        ITEM,
        rail ? "justify-center" : "px-2.5",
        active
          ? "bg-accent font-medium text-foreground shadow-[inset_0_0_0_1px_var(--border)]"
          : "text-muted-foreground hover:bg-accent/60 hover:text-foreground",
      )}
    >
      <Icon className={cn("size-4 shrink-0", active ? "text-brand" : "text-subtle-foreground group-hover:text-foreground")} />
      {!rail && <span className="truncate">{m.title}</span>}
    </button>
  )
}

/** The sidebar, generated from the module registry and the user's prefs. */
export function Sidebar({ current, version }: { current?: string; version?: string }) {
  const { prefs, active, inlineAssets } = usePrefs()
  const { rail, wide, toggle } = useSidebar()
  const { main, bottom } = sidebarGroups(prefs)
  const mascot = active?.art?.sidebarMascot
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

      <nav aria-label="Main" className="flex min-h-0 flex-1 flex-col overflow-y-auto">
        {main.map((g) => (
          <div key={g.id}>
            <SectionLabel rail={rail}>{g.label}</SectionLabel>
            <div className="flex flex-col gap-0.5">
              {g.items.map((m) => (
                <NavItem key={m.id} m={m} active={m.id === current} rail={rail} />
              ))}
            </div>
          </div>
        ))}
        <div className="mt-auto flex flex-col gap-0.5 pt-4">
          {bottom.map((m) => (
            <NavItem key={m.id} m={m} active={m.id === current} rail={rail} />
          ))}
        </div>
      </nav>

      {mascot && active && !rail && (
        <div className="pointer-events-none flex justify-center pb-1 pt-3">
          <img
            src={assetURL(active, mascot, inlineAssets(active))}
            alt=""
            className="theme-art size-14 object-contain opacity-90 drop-shadow-[0_4px_16px_var(--brand-soft)]"
          />
        </div>
      )}

      <div className={cn("flex items-center border-t py-3", rail ? "justify-center" : "justify-between px-1")}>
        {!rail && version && <span className="font-mono text-2xs text-subtle-foreground">lucidd {version}</span>}
        {wide && (
          <button
            onClick={toggle}
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

const MODES: { id: string; label: string; icon: LucideIcon }[] = [
  { id: LIGHT_DEFAULT, label: "Light (Daylight)", icon: Sun },
  { id: DARK_DEFAULT, label: "Dark (Midnight)", icon: Moon },
  { id: SYSTEM_THEME, label: "Match the system", icon: Monitor },
]

/** Quick light / dark / system switch; any other theme is picked in Settings. */
function ThemeToggle() {
  const { prefs, update } = usePrefs()
  return (
    <div role="radiogroup" aria-label="Theme" className="flex items-center rounded-lg border bg-muted/50 p-0.5">
      {MODES.map(({ id, label, icon: Icon }) => {
        const on = prefs.theme === id
        return (
          <button
            key={id}
            role="radio"
            aria-checked={on}
            aria-label={label}
            title={label}
            onClick={() => update((p) => ({ ...p, theme: id }))}
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

export function Header({ module, health }: { module?: ModuleDef; health: Health | null | undefined }) {
  const { openPalette } = useApp()
  const { active, inlineAssets } = usePrefs()
  const ok = health?.status === "ok"
  const section = !module
    ? "Lucidbench"
    : module.kind === "extension"
      ? SECTION_LABEL.extensions
      : (SECTION_LABEL[module.section] ?? "Workspace")
  const banner = active?.art?.headerImage
  return (
    <header className="relative flex h-14 shrink-0 items-center justify-between gap-4 overflow-hidden border-b bg-background/70 px-5 backdrop-blur-md md:px-6">
      {banner && active && (
        <img
          src={assetURL(active, banner, inlineAssets(active))}
          alt=""
          aria-hidden
          className="theme-art pointer-events-none absolute inset-0 size-full object-cover opacity-30 [mask-image:linear-gradient(to_right,black,transparent_75%)]"
        />
      )}
      <nav aria-label="Breadcrumb" className="relative flex min-w-0 items-center gap-1.5 text-sm">
        <span className="text-subtle-foreground">{section}</span>
        <ChevronRight className="size-3.5 shrink-0 text-subtle-foreground" />
        <span aria-current="page" className="truncate font-medium">
          {module?.title ?? "Not found"}
        </span>
      </nav>
      <div className="relative flex items-center gap-3">
        <button
          onClick={openPalette}
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
        <ThemeToggle />
      </div>
    </header>
  )
}

/**
 * The page title block every page uses: an optional eyebrow line, an icon
 * tile, the title, a description and actions on the right.
 */
export function PageHeader({
  icon,
  eyebrow,
  title,
  description,
  actions,
}: {
  icon?: ReactNode
  eyebrow?: ReactNode
  title: ReactNode
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
          {eyebrow && <p className="mb-1 text-xs font-medium text-subtle-foreground">{eyebrow}</p>}
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
