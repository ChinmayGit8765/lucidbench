import * as React from "react"
import { CornerDownLeft, Search, type LucideIcon } from "lucide-react"

import { cn } from "@/lib/utils"

export interface Command {
  id: string
  label: string
  group: "Go to" | "Actions"
  icon: LucideIcon
  hint?: string
  keywords?: string
  disabled?: boolean
  run: () => void
}

/** Every query word must appear in the label, hint or keywords. */
function matches(c: Command, q: string): boolean {
  const hay = `${c.label} ${c.hint ?? ""} ${c.keywords ?? ""}`.toLowerCase()
  return q
    .toLowerCase()
    .split(/\s+/)
    .filter(Boolean)
    .every((w) => hay.includes(w))
}

/** Opens on ⌘K / Ctrl+K anywhere and toggles closed on a second press. */
export function useCommandPaletteHotkey(toggle: () => void) {
  React.useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && !e.altKey && e.key.toLowerCase() === "k") {
        e.preventDefault()
        toggle()
      }
    }
    window.addEventListener("keydown", onKey)
    return () => window.removeEventListener("keydown", onKey)
  }, [toggle])
}

/**
 * A light command palette: a filter box over grouped commands. Arrow keys
 * move, Enter runs, Escape closes. Focus returns to where it was.
 */
export function CommandPalette({
  open,
  onClose,
  commands,
}: {
  open: boolean
  onClose: () => void
  commands: Command[]
}) {
  const [query, setQuery] = React.useState("")
  const [active, setActive] = React.useState(0)
  const input = React.useRef<HTMLInputElement>(null)
  const list = React.useRef<HTMLDivElement>(null)
  const restore = React.useRef<HTMLElement | null>(null)
  const listId = React.useId()

  React.useEffect(() => {
    if (!open) return
    restore.current = document.activeElement as HTMLElement | null
    setQuery("")
    setActive(0)
    requestAnimationFrame(() => input.current?.focus())
    return () => restore.current?.focus?.()
  }, [open])

  const shown = commands.filter((c) => matches(c, query))
  const runnable = shown.filter((c) => !c.disabled)
  const current = runnable[Math.min(active, runnable.length - 1)]

  React.useEffect(() => {
    if (!current) return
    list.current?.querySelector(`[data-id="${CSS.escape(current.id)}"]`)?.scrollIntoView({ block: "nearest" })
  }, [current])

  if (!open) return null

  const run = (c: Command | undefined) => {
    if (!c || c.disabled) return
    onClose()
    // Let the dialog unmount (and focus return) before the command acts.
    setTimeout(c.run, 0)
  }

  const onKey = (e: React.KeyboardEvent) => {
    if (e.key === "Escape") {
      e.preventDefault()
      onClose()
    } else if (e.key === "ArrowDown" || (e.key === "n" && e.ctrlKey)) {
      e.preventDefault()
      setActive((i) => (runnable.length ? (Math.min(i, runnable.length - 1) + 1) % runnable.length : 0))
    } else if (e.key === "ArrowUp" || (e.key === "p" && e.ctrlKey)) {
      e.preventDefault()
      setActive((i) => (runnable.length ? (Math.min(i, runnable.length - 1) - 1 + runnable.length) % runnable.length : 0))
    } else if (e.key === "Enter") {
      e.preventDefault()
      run(current)
    } else if (e.key === "Tab") {
      e.preventDefault() // keep focus in the palette
    }
  }

  const groups = (["Go to", "Actions"] as const)
    .map((g) => ({ g, items: shown.filter((c) => c.group === g) }))
    .filter((x) => x.items.length > 0)

  return (
    <div className="fixed inset-0 z-50 flex items-start justify-center px-4 pt-[14vh]" onKeyDown={onKey}>
      <div className="absolute inset-0 bg-black/45 backdrop-blur-[2px] animate-in fade-in-0 duration-150" onClick={onClose} />
      <div
        role="dialog"
        aria-modal="true"
        aria-label="Command palette"
        className="relative w-full max-w-xl overflow-hidden rounded-xl border border-border-strong bg-elevated shadow-pop animate-in fade-in-0 zoom-in-[0.98] slide-in-from-top-2 duration-150"
      >
        <div className="flex items-center gap-2.5 border-b px-4">
          <Search className="size-4 shrink-0 text-subtle-foreground" />
          <input
            ref={input}
            value={query}
            onChange={(e) => {
              setQuery(e.target.value)
              setActive(0)
            }}
            placeholder="Type a command or search…"
            role="combobox"
            aria-expanded="true"
            aria-controls={listId}
            aria-activedescendant={current ? `${listId}-${current.id}` : undefined}
            className="h-12 flex-1 bg-transparent text-base outline-none placeholder:text-subtle-foreground"
          />
          <kbd className="rounded border bg-muted px-1.5 py-0.5 text-2xs text-subtle-foreground">Esc</kbd>
        </div>
        <div ref={list} id={listId} role="listbox" aria-label="Commands" className="max-h-[min(60vh,420px)] overflow-y-auto p-1.5">
          {groups.length === 0 && <p className="px-3 py-8 text-center text-sm text-muted-foreground">No matching commands.</p>}
          {groups.map(({ g, items }) => (
            <div key={g} role="group" aria-label={g} className="pb-1">
              <div className="px-2.5 pb-1 pt-2 text-2xs font-medium uppercase tracking-[0.08em] text-subtle-foreground">{g}</div>
              {items.map((c) => {
                const on = c === current
                const Icon = c.icon
                return (
                  <div
                    key={c.id}
                    id={`${listId}-${c.id}`}
                    data-id={c.id}
                    role="option"
                    aria-selected={on}
                    aria-disabled={c.disabled || undefined}
                    onMouseMove={() => !c.disabled && setActive(runnable.indexOf(c))}
                    onClick={() => run(c)}
                    className={cn(
                      "flex h-10 cursor-pointer items-center gap-3 rounded-lg px-2.5 text-sm",
                      on && "bg-accent",
                      c.disabled && "cursor-not-allowed opacity-45",
                    )}
                  >
                    <span
                      className={cn(
                        "flex size-6 shrink-0 items-center justify-center rounded-md border bg-background/60 text-muted-foreground",
                        on && "border-brand/40 text-brand-fg",
                      )}
                    >
                      <Icon className="size-3.5" />
                    </span>
                    <span className="min-w-0 flex-1 truncate">{c.label}</span>
                    {c.hint && <span className="max-w-[45%] shrink-0 truncate text-xs text-subtle-foreground">{c.hint}</span>}
                    {on && <CornerDownLeft className="size-3.5 shrink-0 text-subtle-foreground" />}
                  </div>
                )
              })}
            </div>
          ))}
        </div>
        <div className="flex items-center gap-3 border-t bg-muted/30 px-4 py-2 text-2xs text-subtle-foreground">
          <span className="flex items-center gap-1">
            <kbd className="rounded border bg-background px-1">↑</kbd>
            <kbd className="rounded border bg-background px-1">↓</kbd> move
          </span>
          <span className="flex items-center gap-1">
            <kbd className="rounded border bg-background px-1">↵</kbd> run
          </span>
          <span className="ml-auto">Lucidbench</span>
        </div>
      </div>
    </div>
  )
}
