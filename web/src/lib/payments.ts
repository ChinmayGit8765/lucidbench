import { getJSON, sendJSON, usePoll, type Polled } from "@/lib/api"
import { usePrefs } from "@/lib/prefs"

/* Mirrors internal/stripe and internal/payments: keep these in step with the Go structs. */

export const PAYMENTS_POLL_MS = 60000

export type Mode = "test" | "live" | "unknown"

/** GET /api/payments/status. Never the key. */
export interface PaymentsStatus {
  configured: boolean
  key_ref: string
  mode: Mode
  writes_allowed: boolean
}

export interface Account {
  id: string
  name: string
  country?: string
  default_currency?: string
  charges_enabled: boolean
  payouts_enabled: boolean
  mode: Mode
}

export interface Amount {
  amount: number
  currency: string
}

export interface Balance {
  available: Amount[]
  pending: Amount[]
  mode: Mode
}

export interface Payment {
  id: string
  amount: number
  amount_received: number
  currency: string
  status: string
  created: number
  description?: string
}

export interface Payments {
  items: Payment[]
  has_more: boolean
  volume: Amount[]
  mode: Mode
}

export interface Price {
  id: string
  product: string
  amount: number | null
  currency: string
  type: string
  interval?: string
  interval_count?: number
  nickname?: string
}

export interface Product {
  id: string
  name: string
  description?: string
  created: number
  prices: Price[]
}

export interface Catalog {
  products: Product[]
  has_more: boolean
  mode: Mode
}

export interface PaymentLink {
  id: string
  url: string
  active: boolean
}

export interface Webhook {
  id: string
  url: string
  status: string
  enabled_events: string[]
  description?: string
}

export interface AuditEntry {
  time: string
  mode: Mode
  action: string
  plan?: string
  project?: string
  ids: string[]
  ok: boolean
  error?: string
}

export interface PriceSpec {
  amount: number
  currency: string
  interval?: string
  nickname?: string
}

export interface PlanRequest {
  project: string
  product_name: string
  description?: string
  prices: PriceSpec[]
  success_url: string
  webhook_url?: string
  webhook_events?: string[]
}

export interface PlanStep {
  id: string
  kind: string
  title: string
  detail: string
  path: string
  idempotency_key: string
  needs: string[]
}

export interface Plan {
  id: string
  digest: string
  project: string
  mode: Mode
  writable: boolean
  refusal?: string
  request: PlanRequest
  steps: PlanStep[]
  warnings: string[]
}

export interface StepResult {
  id: string
  kind: string
  title: string
  status: "created" | "failed" | "skipped"
  object_id?: string
  url?: string
  error?: string
}

export interface ExecuteResult {
  plan_id: string
  project: string
  mode: Mode
  status: "complete" | "partial" | "failed"
  steps: StepResult[]
  /** Shown once. Keep it in the result panel's state only. */
  webhook_secret?: string
  note?: string
}

export interface LinkedRun {
  plan: string
  mode: string
  created: string
  product?: string
  prices: string[]
  payment_link?: string
  payment_link_url?: string
  webhook_endpoint?: string
}

export const paymentsApi = {
  /** The plan comes back unchanged to execute; the server checks it against its digest. */
  plan: (req: PlanRequest) => sendJSON<Plan>("/api/payments/plan", "POST", req),
  execute: (plan: Plan) => sendJSON<ExecuteResult>("/api/payments/plan/execute", "POST", plan),
  linked: (project: string) => getJSON<{ runs: LinkedRun[] }>(`/api/payments/project/${encodeURIComponent(project)}`),
}

/** Whether the Payments extension is in the sidebar; its hooks stay quiet until it is. */
export function usePaymentsAdded(): boolean {
  const { prefs } = usePrefs()
  return prefs.extensions["payments"]?.added ?? false
}

export function usePaymentsStatus(enabled: boolean): Polled<PaymentsStatus> {
  return usePoll<PaymentsStatus>(enabled ? "/api/payments/status" : null, 30000)
}

/** The last seven days of payments, for the Overview tile. */
export function useWeekVolume(enabled: boolean): Polled<Payments> {
  return usePoll<Payments>(enabled ? "/api/payments/charges?days=7" : null, PAYMENTS_POLL_MS)
}

const ZERO_DECIMAL = new Set(["bif", "clp", "djf", "gnf", "jpy", "kmf", "krw", "mga", "pyg", "rwf", "ugx", "vnd", "vuv", "xaf", "xof", "xpf"])

/** Money in the smallest unit as "A$49.00" (or the plain code when the locale has no symbol). */
export function money(amount: number, currency: string): string {
  const c = currency.toUpperCase()
  const minor = ZERO_DECIMAL.has(currency.toLowerCase()) ? 1 : 100
  try {
    return new Intl.NumberFormat(undefined, { style: "currency", currency: c }).format(amount / minor)
  } catch {
    return `${(amount / minor).toFixed(minor === 1 ? 0 : 2)} ${c}`
  }
}

/** The main-unit amount a person typed, as the smallest unit. */
export function toMinor(main: string, currency: string): number {
  const v = Number(main.replace(/,/g, ""))
  if (!Number.isFinite(v)) return 0
  return Math.round(v * (ZERO_DECIMAL.has(currency.toLowerCase()) ? 1 : 100))
}

export function priceLabel(p: Price): string {
  if (p.amount === null) return "custom"
  const base = money(p.amount, p.currency)
  if (!p.interval) return base
  const n = p.interval_count ?? 1
  return n > 1 ? `${base} every ${n} ${p.interval}s` : `${base} / ${p.interval}`
}

export const PAYMENT_STATUS: Record<string, { tone: "success" | "warning" | "danger" | "info" | "neutral"; label: string }> = {
  succeeded: { tone: "success", label: "Succeeded" },
  processing: { tone: "info", label: "Processing" },
  requires_payment_method: { tone: "warning", label: "Not paid" },
  requires_confirmation: { tone: "warning", label: "Needs confirmation" },
  requires_action: { tone: "warning", label: "Needs action" },
  requires_capture: { tone: "info", label: "Needs capture" },
  canceled: { tone: "neutral", label: "Canceled" },
}
