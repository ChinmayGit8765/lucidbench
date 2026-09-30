import type { ReactNode } from "react"
import { AlertTriangle, RotateCw } from "lucide-react"

import { Button } from "@/components/ui/button"
import { cn } from "@/lib/utils"

/** A shimmering placeholder block. */
export function Skeleton({ className }: { className?: string }) {
  return <div aria-hidden className={cn("skeleton rounded-md", className)} />
}

/**
 * A friendly empty state: a centre icon on a lit tile, with optional small
 * satellite tiles around it, over a faded grid.
 */
export function EmptyState({
  icon,
  satellites = [],
  title,
  description,
  children,
  className,
}: {
  icon: ReactNode
  satellites?: ReactNode[]
  title: string
  description?: ReactNode
  children?: ReactNode
  className?: string
}) {
  const spots = [
    "left-0 top-3 -rotate-12",
    "right-0 top-1 rotate-12",
    "right-3 bottom-0 rotate-6",
    "left-4 bottom-0 -rotate-6",
  ]
  return (
    <div className={cn("flex flex-col items-center px-6 py-10 text-center", className)}>
      <div className={cn("relative mb-5 w-40", satellites.length > 0 ? "h-24" : "h-16")}>
        <div className="bg-grid absolute inset-0 [mask-image:radial-gradient(closest-side,black,transparent)] opacity-70" />
        <div className="absolute inset-0 flex items-center justify-center">
          <div className="relative flex size-14 items-center justify-center rounded-2xl border border-border-strong bg-elevated text-brand shadow-pop [&_svg]:size-6">
            <div className="absolute inset-0 rounded-2xl bg-gradient-to-b from-brand-soft to-transparent" />
            <span className="relative">{icon}</span>
          </div>
        </div>
        {satellites.slice(0, 4).map((s, i) => (
          <div key={i} className={cn("absolute opacity-80", spots[i])}>
            {s}
          </div>
        ))}
      </div>
      <h3 className="text-base font-semibold tracking-tight">{title}</h3>
      {description && <p className="mt-1 max-w-sm text-sm text-muted-foreground">{description}</p>}
      {children && <div className="mt-5 flex flex-wrap items-center justify-center gap-2">{children}</div>}
    </div>
  )
}

export function ErrorState({
  title,
  message,
  onRetry,
  className,
}: {
  title: string
  message?: string
  onRetry?: () => void
  className?: string
}) {
  return (
    <div
      role="alert"
      className={cn("flex items-start gap-3 rounded-lg border border-danger/30 bg-danger-soft p-3.5", className)}
    >
      <span className="mt-px flex size-6 shrink-0 items-center justify-center rounded-md bg-danger/15 text-danger-fg">
        <AlertTriangle className="size-3.5" />
      </span>
      <div className="min-w-0 flex-1 text-sm">
        <div className="font-medium">{title}</div>
        {message && <div className="mt-0.5 break-words text-muted-foreground">{message}</div>}
      </div>
      {onRetry && (
        <Button variant="secondary" size="sm" onClick={onRetry}>
          <RotateCw /> Retry
        </Button>
      )}
    </div>
  )
}
