import * as React from "react"

import { cn } from "@/lib/utils"

/** A neutral chip for metadata such as a location or an image name. */
function Badge({ className, ...props }: React.HTMLAttributes<HTMLSpanElement>) {
  return (
    <span
      className={cn(
        "inline-flex h-5 items-center gap-1 rounded-md border bg-muted/60 px-1.5 text-2xs font-medium text-muted-foreground [&_svg]:size-3 [&_svg]:shrink-0",
        className,
      )}
      {...props}
    />
  )
}

export type Tone = "success" | "warning" | "danger" | "info" | "neutral"

const TONE: Record<Tone, string> = {
  success: "bg-success-soft text-success-fg [--dot:var(--success)]",
  warning: "bg-warning-soft text-warning-fg [--dot:var(--warning)]",
  danger: "bg-danger-soft text-danger-fg [--dot:var(--danger)]",
  info: "bg-info-soft text-info-fg [--dot:var(--info)]",
  neutral: "bg-neutral-soft text-neutral-fg [--dot:var(--neutral)]",
}

/** A status pill: coloured dot plus label on a tinted surface. */
function StatusPill({
  tone,
  pulse = false,
  className,
  children,
}: {
  tone: Tone
  pulse?: boolean
  className?: string
  children: React.ReactNode
}) {
  return (
    <span
      className={cn(
        "inline-flex h-5 shrink-0 items-center gap-1.5 rounded-full px-2 text-2xs font-medium whitespace-nowrap",
        TONE[tone],
        className,
      )}
    >
      <span className={cn("relative size-1.5 rounded-full bg-[var(--dot)] text-[var(--dot)]", pulse && "pulse-ring")} />
      {children}
    </span>
  )
}

export { Badge, StatusPill }
