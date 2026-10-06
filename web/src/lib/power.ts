import { toast } from "sonner"

import type { Tone } from "@/components/ui/badge"
import { ApiError, errorMessage, postAction, refreshAll, usePoll, type Polled } from "@/lib/api"

/* Mirrors internal/power: keep these in step with the Go structs. */

export type PowerMode = "always" | "on-demand" | "off"
export type PowerKind = "cluster" | "runner" | "stack"
export type PowerItemState = "running" | "starting" | "stopping" | "sleeping" | "stopped" | "partial" | "missing" | "unknown"

export interface PowerItem {
  kind: PowerKind
  name: string
  mode: PowerMode
  state: PowerItemState
  repo?: string
  labels?: string[]
  busy: boolean
  detail?: string
  containers: string[]
  idle_seconds?: number
  stops_in_seconds?: number
  memory_bytes: number
  error?: string
  error_at?: string
}

export interface PowerEntry {
  at: string
  kind: PowerKind
  name: string
  action: "start" | "stop"
  auto: boolean
  reason?: string
  ok: boolean
  error?: string
  was?: string
  restarted?: boolean
  seconds?: number
}

export interface PowerState {
  modes: { cluster: PowerMode; cluster_idle_minutes: number; runners: PowerMode; runner_idle_minutes: number }
  poll_seconds: number
  checked_at?: string
  cluster: PowerItem
  runners: PowerItem[]
  stacks: PowerItem[]
  running: number
  sleeping: number
  memory: { managed_bytes: number; docker_bytes: number; known: boolean }
  activity: PowerEntry[]
  errors: string[]
}

export interface SleepOutcome {
  kind: PowerKind
  name: string
  stopped: boolean
  reason: string
}

export const POWER_POLL_MS = 10000

/**
 * Polls /api/power. A daemon without the supervisor answers 404; callers
 * treat that as "power management not available" and show what they did
 * before.
 */
export function usePower(intervalMs = POWER_POLL_MS): Polled<PowerState> & { unavailable: boolean } {
  const p = usePoll<PowerState>("/api/power", intervalMs)
  return { ...p, unavailable: p.error instanceof ApiError && p.error.status === 404 }
}

/** A stopped thing: asleep on demand, or stopped in always/off mode. */
export const isAsleep = (it: Pick<PowerItem, "state"> | undefined) => it?.state === "sleeping" || it?.state === "stopped"

export const STATE_TONE: Record<PowerItemState, Tone> = {
  running: "success",
  starting: "info",
  stopping: "info",
  sleeping: "neutral",
  stopped: "warning",
  partial: "warning",
  missing: "neutral",
  unknown: "neutral",
}

export const MODE_LABEL: Record<PowerMode, string> = {
  always: "always on",
  "on-demand": "on demand",
  off: "manual",
}

/** 734003200 as "700 MiB". */
export function formatBytes(n: number): string {
  if (n >= 1 << 30) return `${(n / (1 << 30)).toFixed(1)} GiB`
  if (n >= 1 << 20) return `${Math.round(n / (1 << 20))} MiB`
  if (n >= 1 << 10) return `${Math.round(n / (1 << 10))} KiB`
  return `${n} B`
}

/** 125 as "2 min", 30 as "30 s". */
export function formatSeconds(s: number): string {
  if (s < 60) return `${Math.max(0, Math.round(s))} s`
  if (s < 3600) return `${Math.round(s / 60)} min`
  return `${(s / 3600).toFixed(1)} h`
}

const KIND_LABEL: Record<PowerKind, string> = { cluster: "cluster", runner: "runner", stack: "stack" }

/** Starts or stops one managed thing, with toasts. A cluster start returns at once and finishes in the background. */
export async function powerAction(kind: PowerKind, name: string, action: "start" | "stop"): Promise<boolean> {
  const what = `${KIND_LABEL[kind]} ${name}`
  const id = toast.loading(`${action === "start" ? "Starting" : "Stopping"} ${what}`)
  try {
    await postAction(`/api/power/${kind}/${encodeURIComponent(name)}/${action}`)
    if (kind === "cluster" && action === "start") {
      toast.success("Cluster starting", { id, description: "It is ready when the node reports Ready, usually within a minute." })
    } else {
      toast.success(`${what[0].toUpperCase()}${what.slice(1)} ${action === "start" ? "started" : "stopped"}`, { id })
    }
    refreshAll()
    return true
  } catch (e) {
    toast.error(`Could not ${action} the ${KIND_LABEL[kind]}`, { id, description: errorMessage(e) })
    return false
  }
}

/** Sleeps every idle on-demand thing, with a toast that says what stopped. */
export async function sleepIdle(): Promise<SleepOutcome[] | null> {
  const id = toast.loading("Sleeping everything idle")
  try {
    const { outcomes } = await postAction<{ outcomes: SleepOutcome[] }>("/api/power/sleep")
    const stopped = outcomes.filter((o) => o.stopped)
    const kept = outcomes.filter((o) => !o.stopped)
    toast.success(stopped.length ? `Put ${stopped.length} to sleep` : "Nothing idle to put to sleep", {
      id,
      description: [
        stopped.map((o) => o.name).join(", "),
        kept.length ? `Kept: ${kept.map((o) => `${o.name} (${o.reason})`).join(", ")}` : "",
      ]
        .filter(Boolean)
        .join(". "),
    })
    refreshAll()
    return outcomes
  } catch (e) {
    toast.error("Could not sleep idle things", { id, description: errorMessage(e) })
    return null
  }
}

/** Every managed item, cluster first. */
export const allItems = (p: PowerState | null | undefined): PowerItem[] =>
  p ? [p.cluster, ...p.runners, ...p.stacks].filter((it) => it.state !== "missing") : []
