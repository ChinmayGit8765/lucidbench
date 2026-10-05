import type { LucideIcon } from "lucide-react"

import { cn } from "@/lib/utils"

export interface TabItem {
  id: string
  label: string
  icon?: LucideIcon
  count?: number
}

/** An underlined tab bar. Arrow keys move between tabs. */
export function Tabs({
  items,
  value,
  onChange,
  label,
  className,
}: {
  items: TabItem[]
  value: string
  onChange: (id: string) => void
  label: string
  className?: string
}) {
  const move = (dir: number) => {
    const i = items.findIndex((t) => t.id === value)
    onChange(items[(i + dir + items.length) % items.length].id)
  }
  return (
    <div
      role="tablist"
      aria-label={label}
      onKeyDown={(e) => {
        if (e.key === "ArrowRight") move(1)
        if (e.key === "ArrowLeft") move(-1)
      }}
      className={cn("flex items-center gap-1 border-b", className)}
    >
      {items.map(({ id, label: l, icon: Icon, count }) => {
        const on = id === value
        return (
          <button
            key={id}
            role="tab"
            aria-selected={on}
            tabIndex={on ? 0 : -1}
            onClick={() => onChange(id)}
            className={cn(
              "relative -mb-px flex h-9 items-center gap-1.5 border-b-2 px-2.5 text-sm transition-colors",
              on ? "border-brand font-medium text-foreground" : "border-transparent text-muted-foreground hover:text-foreground",
            )}
          >
            {Icon && <Icon className={cn("size-3.5", on ? "text-brand" : "text-subtle-foreground")} />}
            {l}
            {count !== undefined && (
              <span className="rounded-full bg-muted px-1.5 text-2xs tabular-nums text-muted-foreground">{count}</span>
            )}
          </button>
        )
      })}
    </div>
  )
}
