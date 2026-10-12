import { sendJSON, usePoll } from "@/lib/api"

/* Mirrors internal/usage: keep these in step with the Go structs. */

export interface UsageTotals {
  input: number
  output: number
  cache_read: number
  cache_write: number
  total: number
  /** Only when the source reports a cost; never estimated. */
  cost_usd?: number
}

export interface UsageDay extends UsageTotals {
  /** Local calendar day, YYYY-MM-DD. */
  date: string
}

export interface UsageBucket extends UsageTotals {
  name: string
  runs?: number
}

export interface UsageWindow {
  name: string
  label: string
  used_percent: number | null
  window_minutes: number | null
  resets_at: string | null
  /** The window has reset since it was seen, so used_percent is out of date. */
  stale?: boolean
  observed_at: string
}

export interface UsageProvider {
  id: string
  label: string
  status: "ok" | "empty" | "missing" | "unavailable" | "disabled"
  note?: string
  sources: number
  daily: UsageDay[]
  totals: UsageTotals
  models: UsageBucket[]
  projects: UsageBucket[]
  windows: UsageWindow[]
  window_note?: string
}

export interface UsageOwn {
  runs: number
  daily: UsageDay[]
  totals: UsageTotals
  sources: UsageBucket[]
  providers: UsageBucket[]
}

export interface UsageSummary {
  generated_at: string
  days: number
  providers: UsageProvider[]
  lucidbench: UsageOwn
}

export const USAGE_POLL_MS = 60000
export const USAGE_WARN_PERCENT = 80

export const usageURL = (days: number) => `/api/usage/summary?days=${days}`

/** 1.2M, 34.5k, 980. */
export function fmtTokens(n: number): string {
  const a = Math.abs(n)
  if (a >= 1e9) return `${(n / 1e9).toFixed(a >= 1e10 ? 0 : 1)}B`
  if (a >= 1e6) return `${(n / 1e6).toFixed(a >= 1e7 ? 0 : 1)}M`
  if (a >= 1e3) return `${(n / 1e3).toFixed(a >= 1e4 ? 0 : 1)}k`
  return String(Math.round(n))
}

/** "2h 10m", "3d 4h", "now" for a reset time. */
export function resetsIn(iso: string | null, now: number): string | null {
  if (!iso) return null
  const t = new Date(iso).getTime()
  if (Number.isNaN(t)) return null
  const mins = Math.round((t - now) / 60000)
  if (mins <= 0) return "now"
  const d = Math.floor(mins / 1440)
  const h = Math.floor((mins % 1440) / 60)
  const m = mins % 60
  if (d > 0) return `${d}d ${h}h`
  if (h > 0) return `${h}h ${m}m`
  return `${m}m`
}

/** A window's used percent when it still describes the current window. */
export const livePercent = (w: UsageWindow): number | null => (w.stale || w.used_percent === null ? null : w.used_percent)

export interface UsageAlert {
  provider: string
  providerLabel: string
  window: UsageWindow
  percent: number
}

/** Windows at or above the warning level. */
export function usageAlerts(s: UsageSummary | null): UsageAlert[] {
  const out: UsageAlert[] = []
  for (const p of s?.providers ?? []) {
    for (const w of p.windows) {
      const pct = livePercent(w)
      if (pct !== null && pct >= USAGE_WARN_PERCENT) out.push({ provider: p.id, providerLabel: p.label, window: w, percent: pct })
    }
  }
  return out
}

/** A gauge bar's colour for a used percent. */
export const gaugeTone = (pct: number) => (pct >= USAGE_WARN_PERCENT ? "bg-danger" : pct >= 60 ? "bg-warning" : "bg-brand")

/* ---------- planning: mirrors internal/usage/plan.go ---------- */

export type PlanKind = "council" | "work" | "section" | "rank" | "assistant"
export const PLAN_KINDS: { id: PlanKind; label: string }[] = [
  { id: "work", label: "Work turn" },
  { id: "council", label: "Council" },
  { id: "section", label: "Describe a section" },
  { id: "rank", label: "Next up ranking" },
  { id: "assistant", label: "Assistant turn" },
]
export const PLAN_PROVIDERS = ["claude", "codex", "grok"] as const

export interface Pace {
  provider: string
  account: string
  window: string
  /** reported by the CLI, or estimated from the logs. */
  basis: "reported" | "estimated"
  starts_at?: string
  resets_at?: string
  used_tokens: number
  capacity_tokens?: number
  used_percent: number | null
  burn_per_hour: number
  hours: number
  runs_out_at?: string
  verdict: "runs_out" | "lasts" | "idle" | "unknown"
  summary: string
}

export interface PlanItem {
  id?: string
  title: string
  kind: PlanKind
  provider?: string
  model?: string
  project?: string
  source?: "nextup"
  nextup_id?: string
}

export interface Estimate {
  tokens: number
  cost_usd?: number
  runs: number
  basis: "model" | "provider" | "kind" | "none"
}

export interface PlannedItem extends PlanItem {
  id: string
  use: string
  use_model?: string
  suggested: boolean
  reason?: string
  estimate: Estimate
  verdict: "fits" | "after_reset" | "unknown" | "too_big"
  fit: string
  window?: string
  resets_at?: string
  warnings: string[]
}

export interface WindowPlan {
  provider: string
  window: string
  resets_at?: string
  remaining_tokens: number
  planned_tokens: number
  items: number
  fit: number
  summary: string
}

export interface PlanView {
  generated_at: string
  hours: number
  paces: Pace[]
  items: PlannedItem[]
  windows: WindowPlan[]
  updated?: string
}

export const planURL = (hours: number) => `/api/usage/plan?hours=${hours}`

/** Saves the plan; only the fields the daemon stores are sent. */
export const savePlan = (items: PlanItem[], hours: number) =>
  sendJSON<PlanView>(planURL(hours), "PUT", {
    items: items.map(({ id, title, kind, provider, model, project, source, nextup_id }) => ({ id, title, kind, provider, model, project, source, nextup_id })),
  })

/** The 7-day summary, shared by the Overview tile, needs-attention and the page. */
export function useUsageSummary() {
  return usePoll<UsageSummary>(usageURL(7), USAGE_POLL_MS)
}
