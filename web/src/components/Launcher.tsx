import { useEffect, useRef, useState, type ReactNode } from "react"
import { ArrowLeft, Check, Moon, Palette, PenLine, Plus, ScrollText, SquareTerminal, type LucideIcon } from "lucide-react"

import { spriteLabel, StateSprite, useThemeSprite, type SpriteState } from "@/components/StateSprite"
import { ConfirmDialog, type ConfirmRequest } from "@/components/ui/confirm"
import { usePoll } from "@/lib/api"
import { useApp } from "@/lib/app"
import { DEFAULT_BOARD, requestBoards } from "@/lib/boards"
import { COUNCIL_POLL_MS, newBraindump, SESSIONS_PATH, type CouncilSummary } from "@/lib/council"
import { allItems, isAsleep, sleepIdle, usePower } from "@/lib/power"
import { usePrefs } from "@/lib/prefs"
import { resolveThemeId, SYSTEM_THEME } from "@/lib/theme"
import { cn, plural } from "@/lib/utils"
import { sessionsPath, WORK_POLL_MS, type WorkSession } from "@/lib/work"

/**
 * What the app is doing, for the mascot: working while an agent runs,
 * thinking while the council deliberates, sleeping when every managed thing
 * is asleep, else idle. Shares its polls with Overview and the pages.
 */
export function useActivity(): { state: SpriteState; detail: string } {
  const work = usePoll<WorkSession[]>(sessionsPath, WORK_POLL_MS * 2)
  const council = usePoll<CouncilSummary[]>(SESSIONS_PATH, COUNCIL_POLL_MS)
  const power = usePower(30_000)
  const running = work.data?.filter((s) => s.status === "running").length ?? 0
  const thinking = council.data?.filter((s) => s.status === "running").length ?? 0
  const items = allItems(power.data)
  if (running > 0) return { state: "working", detail: `${plural(running, "agent")} working` }
  if (thinking > 0) return { state: "thinking", detail: `The council is thinking about ${plural(thinking, "braindump")}` }
  if (items.length > 0 && items.every(isAsleep)) return { state: "sleeping", detail: "Everything is asleep" }
  return { state: "idle", detail: "Nothing running" }
}

interface Action {
  id: string
  label: string
  icon: LucideIcon
  run: () => void
}

/**
 * The quick-access launcher: the mascot (or the logo) opens a small popover
 * of the loop's first moves, Sleep everything idle and a theme switcher.
 */
export function Launcher({
  children,
  label,
  placement = "up",
  className,
}: {
  children: ReactNode
  label: string
  /** Up from the sidebar's foot, or down from the logo. */
  placement?: "up" | "down"
  className?: string
}) {
  const { open } = useApp()
  const [shown, setShown] = useState(false)
  const [view, setView] = useState<"actions" | "themes">("actions")
  const [confirm, setConfirm] = useState<ConfirmRequest | null>(null)
  const root = useRef<HTMLDivElement>(null)
  const button = useRef<HTMLButtonElement>(null)
  const panel = useRef<HTMLDivElement>(null)

  const close = (refocus: boolean) => {
    setShown(false)
    setView("actions")
    if (refocus) button.current?.focus()
  }

  useEffect(() => {
    if (!shown) return
    requestAnimationFrame(() => panel.current?.querySelector<HTMLButtonElement>("button:not(:disabled)")?.focus())
    const onDown = (e: MouseEvent) => {
      if (!root.current?.contains(e.target as Node)) close(false)
    }
    document.addEventListener("mousedown", onDown)
    return () => document.removeEventListener("mousedown", onDown)
  }, [shown, view])

  const actions: Action[] = [
    { id: "braindump", label: "New braindump", icon: PenLine, run: () => newBraindump(open) },
    { id: "work", label: "Start work", icon: SquareTerminal, run: () => open("work", ["new"]) },
    {
      id: "card",
      label: "Add card",
      icon: Plus,
      run: () => {
        requestBoards({ kind: "add-card", board: DEFAULT_BOARD })
        open("boards", [DEFAULT_BOARD])
      },
    },
    { id: "prompt", label: "New prompt", icon: ScrollText, run: () => open("studio") },
    {
      id: "sleep",
      label: "Sleep idle",
      icon: Moon,
      run: () =>
        setConfirm({
          title: "Sleep everything idle?",
          description: "Stops the cluster, runners, stacks and database managers that are on demand and have nothing to do. Busy things stay up, and anything stopped starts again when it is needed.",
          confirmLabel: "Sleep idle things",
          run: () => sleepIdle(),
        }),
    },
    { id: "theme", label: "Switch theme", icon: Palette, run: () => setView("themes") },
  ]

  const onKey = (e: React.KeyboardEvent) => {
    if (e.key === "Escape") {
      e.stopPropagation()
      if (view === "themes") setView("actions")
      else close(true)
      return
    }
    const keys = ["ArrowDown", "ArrowUp", "ArrowLeft", "ArrowRight", "Home", "End"]
    if (!keys.includes(e.key)) return
    e.preventDefault()
    const btns = [...(panel.current?.querySelectorAll<HTMLButtonElement>("button:not(:disabled)") ?? [])]
    if (btns.length === 0) return
    const i = btns.indexOf(document.activeElement as HTMLButtonElement)
    const step = view === "actions" && (e.key === "ArrowDown" || e.key === "ArrowUp") ? 3 : 1
    const dir = e.key === "ArrowDown" || e.key === "ArrowRight" ? 1 : -1
    const next = e.key === "Home" ? 0 : e.key === "End" ? btns.length - 1 : Math.min(btns.length - 1, Math.max(0, i + dir * step))
    btns[next].focus()
  }

  return (
    <div ref={root} className={cn("relative", className)} onKeyDown={onKey}>
      <button
        ref={button}
        type="button"
        aria-label={label}
        title={label}
        aria-haspopup="menu"
        aria-expanded={shown}
        data-testid="launcher"
        onClick={() => (shown ? close(false) : setShown(true))}
        className="rounded-xl outline-none transition-transform duration-200 hover:scale-[1.04] focus-visible:ring-2 focus-visible:ring-ring active:scale-95"
      >
        {children}
      </button>
      {shown && (
        <div
          ref={panel}
          role="menu"
          aria-label="Quick actions"
          className={cn(
            "absolute left-0 z-50 w-64 rounded-2xl border border-border-strong bg-elevated p-2 shadow-pop animate-in fade-in-0 zoom-in-95 duration-150",
            placement === "up" ? "bottom-full mb-2 origin-bottom-left slide-in-from-bottom-2" : "top-full mt-2 origin-top-left slide-in-from-top-2",
          )}
        >
          {view === "actions" ? (
            <>
              <div className="px-1.5 pb-2 pt-1 text-2xs font-medium uppercase tracking-[0.08em] text-subtle-foreground">Quick actions</div>
              <div className="grid grid-cols-3 gap-1.5">
                {actions.map((a, i) => {
                  const Icon = a.icon
                  return (
                    <button
                      key={a.id}
                      type="button"
                      role="menuitem"
                      data-testid={`launcher-${a.id}`}
                      style={{ "--i": i } as React.CSSProperties}
                      onClick={() => {
                        if (a.id !== "theme") close(false)
                        a.run()
                      }}
                      className="lb-rise group flex flex-col items-center gap-1.5 rounded-xl border border-transparent px-1 py-2.5 text-center text-2xs font-medium leading-tight text-muted-foreground outline-none transition-colors hover:border-border hover:bg-accent/60 hover:text-foreground focus-visible:border-ring focus-visible:bg-accent/60 focus-visible:text-foreground"
                    >
                      <span className="flex size-9 items-center justify-center rounded-full bg-brand-soft text-brand-fg transition-transform duration-200 group-hover:scale-110 group-focus-visible:scale-110">
                        <Icon className="size-4" />
                      </span>
                      {a.label}
                    </button>
                  )
                })}
              </div>
            </>
          ) : (
            <ThemeList onBack={() => setView("actions")} onPicked={() => close(true)} />
          )}
        </div>
      )}
      <ConfirmDialog request={confirm} onClose={() => setConfirm(null)} />
    </div>
  )
}

function ThemeList({ onBack, onPicked }: { onBack: () => void; onPicked: () => void }) {
  const { themes, prefs, update } = usePrefs()
  const chosen = prefs.theme === SYSTEM_THEME ? SYSTEM_THEME : resolveThemeId(prefs.theme)
  const pick = (id: string) => {
    update((p) => ({ ...p, theme: id }))
    onPicked()
  }
  const rows = [{ id: SYSTEM_THEME, name: "Match the system", swatch: "linear-gradient(135deg, oklch(0.97 0 0) 50%, oklch(0.2 0.01 265) 50%)" }].concat(
    themes.map((t) => ({ id: t.id, name: t.name, swatch: t.tokens["--brand"] ?? (t.base === "dark" ? "oklch(0.72 0.14 245)" : "oklch(0.55 0.19 255)") })),
  )
  return (
    <div>
      <button
        type="button"
        role="menuitem"
        onClick={onBack}
        className="mb-1 flex h-7 w-full items-center gap-1.5 rounded-md px-1.5 text-xs text-subtle-foreground outline-none hover:bg-accent hover:text-foreground focus-visible:bg-accent"
      >
        <ArrowLeft className="size-3.5" /> Quick actions
      </button>
      <div className="max-h-64 overflow-y-auto">
        {rows.map((t) => (
          <button
            key={t.id}
            type="button"
            role="menuitemradio"
            aria-checked={chosen === t.id}
            data-testid={`launcher-theme-${t.id}`}
            onClick={() => pick(t.id)}
            className="flex h-8 w-full items-center gap-2 rounded-md px-1.5 text-left text-sm outline-none hover:bg-accent focus-visible:bg-accent"
          >
            <span aria-hidden className="size-3.5 shrink-0 rounded-full border border-border-strong" style={{ background: t.swatch }} />
            <span className="flex-1 truncate">{t.name}</span>
            {chosen === t.id && <Check className="size-3.5 text-brand" />}
          </button>
        ))}
      </div>
    </div>
  )
}

/**
 * The sidebar mascot: the theme's mascot (or Lumi), showing what the app is
 * doing, and opening the launcher when clicked.
 */
export function SidebarMascot() {
  const { state, detail } = useActivity()
  const custom = useThemeSprite(state)
  return (
    <div className="flex justify-center pb-1 pt-3">
      <Launcher label={`Quick actions (${detail})`}>
        <span className="relative block" data-mascot-state={state}>
          <span aria-hidden className="pointer-events-none absolute inset-1 rounded-full bg-[radial-gradient(closest-side,var(--brand-soft),transparent)]" />
          <StateSprite state={state} className={cn("relative size-14", custom && "opacity-90 drop-shadow-[0_4px_16px_var(--brand-soft)]")} />
          <span className="sr-only">{spriteLabel(state)}</span>
        </span>
      </Launcher>
    </div>
  )
}
