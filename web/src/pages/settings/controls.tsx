import type { ReactNode } from "react"

import { cn } from "@/lib/utils"

/** A settings section: a card with a title row. */
export function Section({
  id,
  title,
  description,
  actions,
  children,
  className,
}: {
  id?: string
  title: string
  description?: ReactNode
  actions?: ReactNode
  children: ReactNode
  className?: string
}) {
  return (
    <section id={id} className={cn("scroll-mt-6 rounded-xl border bg-card text-card-foreground shadow-card", className)}>
      <div className="flex flex-wrap items-start justify-between gap-3 px-5 pb-3 pt-4">
        <div className="min-w-0">
          <h2 className="text-sm font-semibold">{title}</h2>
          {description && <p className="mt-0.5 max-w-2xl text-sm text-muted-foreground">{description}</p>}
        </div>
        {actions && <div className="flex shrink-0 items-center gap-2">{actions}</div>}
      </div>
      {children}
    </section>
  )
}

/** One labelled control in a settings list. */
export function Row({ label, hint, children }: { label: string; hint?: ReactNode; children: ReactNode }) {
  return (
    <div className="flex flex-wrap items-center justify-between gap-x-6 gap-y-2 border-t px-5 py-3">
      <div className="min-w-0">
        <div className="text-sm font-medium">{label}</div>
        {hint && <div className="mt-0.5 text-xs text-muted-foreground">{hint}</div>}
      </div>
      <div className="flex shrink-0 items-center gap-2">{children}</div>
    </div>
  )
}

/** A segmented single choice. */
export function Segmented<T extends string>({
  value,
  options,
  onChange,
  label,
}: {
  value: T
  options: { id: T; label: string; title?: string }[]
  onChange: (v: T) => void
  label: string
}) {
  return (
    <div role="radiogroup" aria-label={label} className="flex items-center rounded-lg border bg-muted/50 p-0.5">
      {options.map((o) => {
        const on = o.id === value
        return (
          <button
            key={o.id}
            role="radio"
            aria-checked={on}
            title={o.title}
            onClick={() => onChange(o.id)}
            className={cn(
              "h-6 rounded-md px-2.5 text-xs transition-colors",
              on ? "bg-elevated font-medium text-foreground shadow-card" : "text-muted-foreground hover:text-foreground",
            )}
          >
            {o.label}
          </button>
        )
      })}
    </div>
  )
}

/** An on/off switch. */
export function Switch({ checked, onChange, label }: { checked: boolean; onChange: (v: boolean) => void; label: string }) {
  return (
    <button
      role="switch"
      aria-checked={checked}
      aria-label={label}
      onClick={() => onChange(!checked)}
      className={cn(
        "relative h-5 w-9 shrink-0 rounded-full border transition-colors focus-visible:ring-2 focus-visible:ring-ring",
        checked ? "border-transparent bg-primary" : "bg-muted",
      )}
    >
      <span
        className={cn(
          "absolute top-0.5 size-3.5 rounded-full shadow-card transition-transform duration-200",
          checked ? "translate-x-[18px] bg-white" : "translate-x-0.5 bg-foreground/60",
        )}
      />
    </button>
  )
}
