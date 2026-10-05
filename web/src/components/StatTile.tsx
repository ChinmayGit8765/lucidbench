import type { ReactNode } from "react"
import { ArrowUpRight, type LucideIcon } from "lucide-react"

import { Card } from "@/components/ui/card"
import { Skeleton } from "@/components/ui/states"

/** An Overview tile: a headline number for one module, linking to it. */
export function StatTile({
  icon: Icon,
  label,
  onOpen,
  loading,
  value,
  sub,
  aside,
  footer,
}: {
  icon: LucideIcon
  label: string
  onOpen: () => void
  loading?: boolean
  value: ReactNode
  sub?: ReactNode
  aside?: ReactNode
  footer?: ReactNode
}) {
  return (
    <Card className="group relative overflow-hidden transition-[border-color,box-shadow] duration-200 focus-within:border-border-strong hover:border-border-strong">
      <div aria-hidden className="tile-glow pointer-events-none absolute inset-0" />
      <button
        onClick={onOpen}
        aria-label={`${label}: open`}
        className="absolute inset-0 z-10 rounded-xl outline-none focus-visible:ring-2 focus-visible:ring-ring"
      />
      <div className="relative flex h-full flex-col p-4">
        <div className="flex items-center justify-between gap-2">
          <span className="flex items-center gap-2 text-xs font-medium text-muted-foreground">
            <Icon className="size-3.5 text-subtle-foreground" />
            {label}
          </span>
          <ArrowUpRight className="size-3.5 text-subtle-foreground opacity-0 transition-opacity group-hover:opacity-100" />
        </div>
        {loading ? (
          <div className="mt-3 space-y-2">
            <Skeleton className="h-7 w-20" />
            <Skeleton className="h-3.5 w-32" />
            <Skeleton className="mt-3 h-6 w-full" />
          </div>
        ) : (
          <>
            <div className="mt-2 flex items-end justify-between gap-2">
              <div className="truncate text-2xl font-semibold tabular-nums tracking-tight">{value}</div>
              {aside}
            </div>
            {sub && <div className="mt-0.5 truncate text-xs text-muted-foreground">{sub}</div>}
            {footer && <div className="mt-auto pt-3">{footer}</div>}
          </>
        )}
      </div>
    </Card>
  )
}
