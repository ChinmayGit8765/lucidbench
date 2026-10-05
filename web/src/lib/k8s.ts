import type { Tone } from "@/components/ui/badge"

/* Mirrors internal/k8s: keep these in step with the Go structs. */

export interface K8sNode {
  name: string
  ready: boolean
  roles: string[]
  version: string
  os: string
  runtime: string
  cpu: string
  memory: string
  pods: number
  created_at: string
  pressure: string[]
}

export interface K8sPod {
  namespace: string
  name: string
  phase: string
  reason?: string
  ready: string
  restarts: number
  node: string
  containers: string[]
  created_at: string
  owner?: string
}

export interface K8sJob {
  namespace: string
  name: string
  status: "Running" | "Completed" | "Failed" | "Pending"
  finished: boolean
  succeeded: number
  failed: number
  image: string
  created_at: string
  completed_at?: string
}

export interface K8sEvent {
  namespace: string
  type: string
  reason: string
  object: string
  message: string
  count: number
  at: string
}

export const K8S_POLL_MS = 10000

export const PHASE_TONE: Record<string, Tone> = {
  Running: "success",
  Succeeded: "neutral",
  Pending: "warning",
  Failed: "danger",
  Unknown: "neutral",
}

export const JOB_TONE: Record<string, Tone> = {
  Completed: "success",
  Running: "info",
  Failed: "danger",
  Pending: "warning",
}

/** Waiting reasons that mean a pod is in trouble even while "Running". */
export const isTroubled = (p: K8sPod) =>
  /BackOff|Err|Invalid|Failed/.test(p.reason ?? "") || p.phase === "Failed"

/** A short age like "3d", "4h", "12m". */
export function age(iso: string, now: number): string {
  const s = Math.max(0, Math.round((now - Date.parse(iso)) / 1000))
  if (s < 60) return `${s}s`
  if (s < 3600) return `${Math.floor(s / 60)}m`
  if (s < 86400) return `${Math.floor(s / 3600)}h`
  return `${Math.floor(s / 86400)}d`
}

/** k8s quantities like "16323444Ki" as a friendly size. */
export function friendlyMemory(q: string): string {
  const m = /^(\d+(?:\.\d+)?)(Ki|Mi|Gi|Ti)?$/.exec(q)
  if (!m) return q
  const factor = { Ki: 1 / 1024 / 1024, Mi: 1 / 1024, Gi: 1, Ti: 1024 }[m[2] ?? ""] ?? 1 / 1024 ** 3
  return `${(Number(m[1]) * factor).toFixed(1)} GiB`
}
