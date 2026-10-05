import * as React from "react"
import type { LucideIcon } from "lucide-react"

import { cn } from "@/lib/utils"

export interface MenuItem {
  label: string
  icon?: LucideIcon
  onSelect: () => void
  disabled?: boolean
  danger?: boolean
}

/**
 * A small dropdown menu anchored to its trigger. Arrow keys move between
 * items, Escape and outside clicks close it, and focus returns to the trigger.
 */
export function Menu({
  label,
  trigger,
  items,
  align = "end",
}: {
  label: string
  trigger: React.ReactNode
  items: MenuItem[]
  align?: "start" | "end"
}) {
  const [open, setOpen] = React.useState(false)
  const root = React.useRef<HTMLDivElement>(null)
  const button = React.useRef<HTMLButtonElement>(null)
  const list = React.useRef<HTMLDivElement>(null)
  const menuId = React.useId()

  const close = React.useCallback((refocus: boolean) => {
    setOpen(false)
    if (refocus) button.current?.focus()
  }, [])

  React.useEffect(() => {
    if (!open) return
    const first = list.current?.querySelector<HTMLButtonElement>("button:not(:disabled)")
    first?.focus()
    const onDown = (e: MouseEvent) => {
      if (!root.current?.contains(e.target as Node)) close(false)
    }
    document.addEventListener("mousedown", onDown)
    return () => document.removeEventListener("mousedown", onDown)
  }, [open, close])

  const onKey = (e: React.KeyboardEvent) => {
    if (e.key === "Escape") {
      e.stopPropagation()
      close(true)
      return
    }
    if (e.key !== "ArrowDown" && e.key !== "ArrowUp" && e.key !== "Home" && e.key !== "End") return
    e.preventDefault()
    const btns = [...(list.current?.querySelectorAll<HTMLButtonElement>("button:not(:disabled)") ?? [])]
    if (btns.length === 0) return
    const i = btns.indexOf(document.activeElement as HTMLButtonElement)
    const next =
      e.key === "Home" ? 0 : e.key === "End" ? btns.length - 1 : (i + (e.key === "ArrowDown" ? 1 : -1) + btns.length) % btns.length
    btns[next].focus()
  }

  return (
    <div ref={root} className="relative" onKeyDown={onKey}>
      <button
        ref={button}
        type="button"
        aria-label={label}
        title={label}
        aria-haspopup="menu"
        aria-expanded={open}
        aria-controls={open ? menuId : undefined}
        onClick={() => setOpen((o) => !o)}
        className={cn(
          "inline-flex size-7 items-center justify-center rounded-md text-subtle-foreground transition-colors hover:bg-accent hover:text-foreground [&_svg]:size-4",
          open && "bg-accent text-foreground",
        )}
      >
        {trigger}
      </button>
      {open && (
        <div
          ref={list}
          id={menuId}
          role="menu"
          aria-label={label}
          className={cn(
            "absolute top-full z-40 mt-1 min-w-40 rounded-lg border border-border-strong bg-elevated p-1 shadow-pop animate-in fade-in-0 zoom-in-95 duration-150",
            align === "end" ? "right-0 origin-top-right" : "left-0 origin-top-left",
          )}
        >
          {items.map(({ label, icon: Icon, onSelect, disabled, danger }) => (
            <button
              key={label}
              type="button"
              role="menuitem"
              disabled={disabled}
              onClick={() => {
                close(true)
                onSelect()
              }}
              className={cn(
                "flex h-8 w-full items-center gap-2 rounded-md px-2 text-left text-sm outline-none transition-colors disabled:pointer-events-none disabled:opacity-40",
                danger
                  ? "text-danger-fg hover:bg-danger-soft focus-visible:bg-danger-soft"
                  : "hover:bg-accent focus-visible:bg-accent",
              )}
            >
              {Icon && <Icon className="size-3.5 shrink-0 opacity-80" />}
              {label}
            </button>
          ))}
        </div>
      )}
    </div>
  )
}
