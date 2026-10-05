import { CircleCheck, CircleDashed, CircleSlash, CircleX, LoaderCircle, type LucideIcon } from "lucide-react"

import { formatDuration, RUN_STATE, runDuration, runState, type CIRun, type RunState } from "@/lib/ci"
import { cn } from "@/lib/utils"

const ICON: Record<RunState, { icon: LucideIcon; className: string }> = {
  success: { icon: CircleCheck, className: "text-success" },
  failure: { icon: CircleX, className: "text-danger" },
  cancelled: { icon: CircleSlash, className: "text-subtle-foreground" },
  skipped: { icon: CircleSlash, className: "text-subtle-foreground" },
  running: { icon: LoaderCircle, className: "text-info animate-spin [animation-duration:1.4s]" },
  queued: { icon: CircleDashed, className: "text-warning" },
  other: { icon: CircleDashed, className: "text-subtle-foreground" },
}

/** The conclusion icon of a workflow run. */
export function RunIcon({ run, className }: { run: Pick<CIRun, "status" | "conclusion">; className?: string }) {
  const st = runState(run)
  const { icon: Icon, className: tone } = ICON[st]
  return <Icon role="img" aria-label={RUN_STATE[st].label} className={cn("size-4 shrink-0", tone, className)} />
}

const BAR: Record<RunState, string> = {
  success: "bg-success/80",
  failure: "bg-danger",
  cancelled: "bg-neutral/50",
  skipped: "bg-neutral/40",
  running: "bg-info",
  queued: "bg-warning/80",
  other: "bg-neutral/50",
}

/**
 * Recent runs as a bar strip, oldest on the left. Bar height follows run
 * time; colour follows the result. Empty slots keep the strip a fixed width.
 */
export function RunBars({
  runs,
  slots = 24,
  now,
  className,
}: {
  runs: CIRun[]
  slots?: number
  now: number
  className?: string
}) {
  const recent = runs.slice(0, slots).reverse()
  const durations = recent.map((r) => runDuration(r, now) ?? 0)
  const max = Math.max(1, ...durations)
  const pad = Math.max(0, slots - recent.length)
  return (
    <div
      role="img"
      aria-label={`Last ${recent.length} runs: ${recent.filter((r) => runState(r) === "success").length} passed, ${recent.filter((r) => runState(r) === "failure").length} failed`}
      className={cn("flex h-8 items-end gap-[3px]", className)}
    >
      {Array.from({ length: pad }, (_, i) => (
        <span key={`pad-${i}`} className="h-1 flex-1 rounded-sm bg-muted" />
      ))}
      {recent.map((r, i) => {
        const st = runState(r)
        const h = 28 + Math.round((durations[i] / max) * 72)
        return (
          <span
            key={`${r.repo}-${r.id}`}
            title={`${r.repo} · ${r.name} · ${RUN_STATE[st].label} · ${formatDuration(runDuration(r, now))}`}
            style={{ height: `${h}%` }}
            className={cn("flex-1 rounded-sm transition-[height] duration-500", BAR[st])}
          />
        )
      })}
    </div>
  )
}

/** A runner's status dot: green online, azure busy (pulsing), grey offline. */
export function RunnerDot({ status, busy, className }: { status: string; busy?: boolean; className?: string }) {
  const tone = status !== "online" ? "bg-neutral" : busy ? "bg-info text-info" : "bg-success text-success"
  return (
    <span
      aria-hidden
      className={cn("relative inline-block size-2 shrink-0 rounded-full", tone, busy && status === "online" && "pulse-ring", className)}
    />
  )
}
