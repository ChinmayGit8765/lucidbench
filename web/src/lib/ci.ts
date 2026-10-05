import { toast } from "sonner"

import { errorMessage, postAction } from "@/lib/api"
import type { Tone } from "@/components/ui/badge"

/* Mirrors internal/ci: keep these in step with the Go structs. */

export interface CIRunner {
  repo: string
  id: number
  name: string
  os: string
  status: "online" | "offline" | string
  busy: boolean
  labels: string[]
  container?: string
}

export interface CIContainer {
  id: string
  name: string
  image: string
  state: string
  status: string
  started_at?: string
  compose_project?: string
  compose_service?: string
  repo_url?: string
  runner_name?: string
}

export interface CIRun {
  repo: string
  id: number
  run_number: number
  name: string
  title: string
  branch: string
  event: string
  status: string
  conclusion: string
  created_at: string
  run_started_at: string
  updated_at: string
  html_url: string
  actor?: string
}

export interface SourceError {
  source: string
  message: string
}

export type TokenSource = "env" | "gh" | "none"

export interface CISummary {
  configured: boolean
  repos: string[]
  token_source: TokenSource
  runners: { total: number; online: number; busy: number; offline: number }
  containers: { total: number; up: number; down: number }
  runs_24h: { total: number; in_progress: number; by_conclusion: Record<string, number>; pass_rate: number | null }
  failing_repos: string[]
  errors: SourceError[]
  generated_at: string
}

export interface CIRunners {
  repos: string[]
  token_source: TokenSource
  runners: CIRunner[]
  containers: CIContainer[]
  errors: SourceError[]
}

export interface CIRuns {
  repos: string[]
  token_source: TokenSource
  runs: CIRun[]
  errors: SourceError[]
}

export const CI_POLL_MS = 15000

/** Failure conclusions, as counted by the daemon. */
export const isFailure = (r: Pick<CIRun, "status" | "conclusion">) =>
  r.status === "completed" && ["failure", "timed_out", "startup_failure"].includes(r.conclusion)

export const isActive = (r: Pick<CIRun, "status">) => r.status !== "completed"

export type RunState = "success" | "failure" | "cancelled" | "skipped" | "running" | "queued" | "other"

export function runState(r: Pick<CIRun, "status" | "conclusion">): RunState {
  if (r.status === "in_progress") return "running"
  if (r.status !== "completed") return "queued"
  if (r.conclusion === "success") return "success"
  if (isFailure(r)) return "failure"
  if (r.conclusion === "cancelled") return "cancelled"
  if (r.conclusion === "skipped" || r.conclusion === "neutral") return "skipped"
  return "other"
}

export const RUN_STATE: Record<RunState, { tone: Tone; label: string }> = {
  success: { tone: "success", label: "Passed" },
  failure: { tone: "danger", label: "Failed" },
  cancelled: { tone: "neutral", label: "Cancelled" },
  skipped: { tone: "neutral", label: "Skipped" },
  running: { tone: "info", label: "Running" },
  queued: { tone: "warning", label: "Queued" },
  other: { tone: "neutral", label: "Done" },
}

/** Run time in ms: start to last update, or to now while still running. */
export function runDuration(r: CIRun, now: number): number | null {
  const start = Date.parse(r.run_started_at || r.created_at)
  const end = r.status === "completed" ? Date.parse(r.updated_at) : now
  if (Number.isNaN(start) || Number.isNaN(end) || end < start) return null
  return end - start
}

/** "42s", "3m 05s", "1h 12m". */
export function formatDuration(ms: number | null): string {
  if (ms === null) return "-"
  const s = Math.round(ms / 1000)
  if (s < 60) return `${s}s`
  const m = Math.floor(s / 60)
  if (m < 60) return `${m}m ${String(s % 60).padStart(2, "0")}s`
  return `${Math.floor(m / 60)}h ${String(m % 60).padStart(2, "0")}m`
}

/** Repository name without the owner, for dense rows. */
export const shortRepo = (repo: string) => repo.split("/")[1] ?? repo

/**
 * Failed runs that still need attention: the newest completed run of each
 * repository + workflow + branch, when it failed.
 */
export function failingRuns(runs: CIRun[]): CIRun[] {
  const latest = new Map<string, CIRun>()
  for (const r of runs) {
    if (r.status !== "completed") continue
    const key = `${r.repo}\u0000${r.name}\u0000${r.branch}`
    const cur = latest.get(key)
    if (!cur || r.created_at > cur.created_at) latest.set(key, r)
  }
  return [...latest.values()].filter(isFailure).sort((a, b) => b.created_at.localeCompare(a.created_at))
}

/** Re-runs the failed jobs of a run, with toasts. Resolves true on success. */
export async function rerunFailed(run: Pick<CIRun, "repo" | "id" | "name">): Promise<boolean> {
  const id = toast.loading(`Re-running failed jobs of ${run.name}`)
  try {
    await postAction(`/api/ci/runs/${encodeURIComponent(run.repo)}/${run.id}/rerun`)
    toast.success("Re-run requested", { id, description: `${run.repo} · run ${run.id}` })
    return true
  } catch (e) {
    toast.error("Could not re-run", { id, description: errorMessage(e) })
    return false
  }
}

export type ContainerAction = "start" | "stop" | "restart"

/** Starts, stops or restarts a runner container, with toasts. */
export async function containerAction(name: string, action: ContainerAction): Promise<boolean> {
  const verb = { start: "Starting", stop: "Stopping", restart: "Restarting" }[action]
  const done = { start: "started", stop: "stopped", restart: "restarted" }[action]
  const id = toast.loading(`${verb} ${name}`)
  try {
    await postAction(`/api/ci/containers/${encodeURIComponent(name)}/${action}`)
    toast.success(`Container ${done}`, { id, description: name })
    return true
  } catch (e) {
    toast.error(`Could not ${action} the container`, { id, description: errorMessage(e) })
    return false
  }
}
