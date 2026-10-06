import { useEffect, useRef, useState } from "react"

import type { Tone } from "@/components/ui/badge"
import { ApiError, errorMessage, getJSON, sendJSON } from "@/lib/api"

/* Mirrors internal/council: keep these in step with the Go structs. */

export type CouncilProvider = "claude" | "codex" | "grok"
export const COUNCIL_PROVIDERS: CouncilProvider[] = ["claude", "codex", "grok"]

export type Verdict = "ok" | "concerns" | "blocker"
export type Severity = "blocker" | "major" | "minor"
export type CouncilStatus = "running" | "draft" | "approved" | "failed"
export type Stage = "propose" | "critique" | "synthesise" | "write" | "done"

export interface CouncilPoint {
  severity: Severity
  text: string
}

export interface CouncilUsage {
  provider: string
  model?: string
  input_tokens?: number
  output_tokens?: number
  cache_read_tokens?: number
  cache_write_tokens?: number
  cost_usd?: number
  duration_ms: number
}

export interface CouncilStep {
  provider: CouncilProvider
  role: "propose" | "critique" | "self-critique" | "synthesise"
  running: boolean
  text?: string
  verdict?: Verdict
  points?: CouncilPoint[]
  skipped?: boolean
  error?: string
  usage?: CouncilUsage
  started: string
  ended: string
}

export interface CouncilRound {
  n: number
  notes?: string
  proposal?: CouncilStep
  critiques: CouncilStep[]
  synthesis?: CouncilStep
}

export interface CouncilEvent {
  time: string
  kind: "stage" | "thinking" | "critique" | "skipped" | "round" | "done" | "error"
  round?: number
  provider?: string
  text: string
}

export interface BoardCard {
  id: string
  title: string
  column: string
  project?: string
  memory?: string
  council?: string
}

export interface CouncilSession {
  id: string
  input: string
  project?: string
  title?: string
  proposer: CouncilProvider
  critics: CouncilProvider[]
  mode: "council" | "self-critique"
  max_rounds: number
  rounds: CouncilRound[]
  stopped_early: boolean
  /** The brief was approved while the latest critiques still held a blocker. */
  approved_with_blockers?: boolean
  stage: Stage
  thinking: string[]
  notes: string[]
  warnings: string[]
  log: CouncilEvent[]
  brief?: string
  brief_path?: string
  status: CouncilStatus
  error?: string
  card?: BoardCard
  usage: CouncilUsage[]
  prompts: Record<string, string>
  created: string
  updated: string
}

export interface CouncilSummary {
  id: string
  title: string
  input: string
  project?: string
  status: CouncilStatus
  stage: Stage
  mode: "council" | "self-critique"
  proposer: CouncilProvider
  critics: CouncilProvider[]
  rounds: number
  stopped_early: boolean
  brief_path?: string
  card?: string
  input_tokens: number
  output_tokens: number
  cost_usd: number
  error?: string
  created: string
  updated: string
}

export interface StartRequest {
  input: string
  project?: string
  proposer?: CouncilProvider
  critics?: CouncilProvider[]
  rounds?: number
}

export const COUNCIL_POLL_MS = 15000
export const SESSIONS_PATH = "/api/council/sessions"

/** Asks the Council page to focus its composer (the palette's "New braindump…"). */
export const COMPOSE_EVENT = "lucidbench:council-compose"

/** Opens Council with the composer focused, from anywhere in the app. */
export function newBraindump(open: (id: string, sub?: string[]) => void) {
  open("council")
  // The page may already be open; ask it to focus the composer.
  setTimeout(() => window.dispatchEvent(new Event(COMPOSE_EVENT)), 50)
}

export const STATUS: Record<CouncilStatus, { tone: Tone; label: string }> = {
  running: { tone: "info", label: "Running" },
  draft: { tone: "warning", label: "Waiting for approval" },
  approved: { tone: "success", label: "Approved" },
  failed: { tone: "danger", label: "Failed" },
}

export const VERDICT: Record<Verdict, { tone: Tone; label: string }> = {
  ok: { tone: "success", label: "Looks good" },
  concerns: { tone: "warning", label: "Concerns" },
  blocker: { tone: "danger", label: "Blocker" },
}

export const SEVERITY: Record<Severity, { label: string; dot: string; text: string }> = {
  blocker: { label: "Blocker", dot: "bg-danger", text: "text-danger-fg" },
  major: { label: "Major", dot: "bg-warning", text: "text-warning-fg" },
  minor: { label: "Minor", dot: "bg-neutral", text: "text-neutral-fg" },
}

export const startCouncil = (req: StartRequest) => sendJSON<CouncilSession>(SESSIONS_PATH, "POST", req)
/** anyway accepts a blocker still open in the latest critiques; the server refuses with 409 without it. */
export const approveBrief = (id: string, project?: string, anyway = false) =>
  sendJSON<BoardCard>(`${SESSIONS_PATH}/${id}/approve`, "POST", {
    ...(project ? { project } : {}),
    ...(anyway ? { approved_with_blockers: true } : {}),
  })

/** The blocker points of the newest round's critiques: what a critic still wants changed. */
export function latestBlockers(s: CouncilSession): { provider: CouncilProvider; text: string }[] {
  const last = s.rounds[s.rounds.length - 1]
  if (!last) return []
  return last.critiques
    .filter((c) => !c.skipped)
    .flatMap((c) => (c.points ?? []).filter((p) => p.severity === "blocker").map((p) => ({ provider: c.provider, text: p.text })))
}

/**
 * "2 rounds", or "3 rounds (incl. Ask again)": the automatic loop stops at
 * max_rounds, and each Ask again adds one round after it.
 */
export function roundsLabel(s: { rounds: number; max_rounds?: number }): string {
  const n = s.rounds
  return `${n} ${n === 1 ? "round" : "rounds"}${s.max_rounds !== undefined && n > s.max_rounds ? " (incl. Ask again)" : ""}`
}
export const askAgain = (id: string, notes: string) => sendJSON<CouncilSession>(`${SESSIONS_PATH}/${id}/again`, "POST", { notes })

/** How many model calls a run can make: the draft, then per round each critic plus the revision. */
export function maxCalls(critics: number, rounds = 2): number {
  return 1 + rounds * (Math.max(critics, 1) + 1)
}

export interface Totals {
  calls: number
  input: number
  output: number
  cost: number
  ms: number
  /** Providers that reported no cost. */
  uncosted: string[]
}

export function totals(usage: CouncilUsage[]): Totals {
  const t: Totals = { calls: usage.length, input: 0, output: 0, cost: 0, ms: 0, uncosted: [] }
  for (const u of usage) {
    t.input += (u.input_tokens ?? 0) + (u.cache_read_tokens ?? 0) + (u.cache_write_tokens ?? 0)
    t.output += u.output_tokens ?? 0
    t.cost += u.cost_usd ?? 0
    t.ms += u.duration_ms
    if (!u.cost_usd && !t.uncosted.includes(u.provider)) t.uncosted.push(u.provider)
  }
  return t
}

/** 1234 → "1.2k". */
export function tokens(n: number): string {
  if (n < 1000) return String(n)
  if (n < 1_000_000) return `${(n / 1000).toFixed(n < 10_000 ? 1 : 0)}k`
  return `${(n / 1_000_000).toFixed(1)}M`
}

export function usd(n: number): string {
  if (n === 0) return "$0"
  return n < 0.01 ? "<$0.01" : `$${n.toFixed(2)}`
}

export function seconds(ms: number): string {
  const s = Math.round(ms / 1000)
  if (s < 60) return `${s}s`
  return `${Math.floor(s / 60)}m ${String(s % 60).padStart(2, "0")}s`
}

/**
 * One session, kept live: fetched once, then followed over the events
 * stream while it runs. When the stream drops, it falls back to polling.
 */
export function useCouncilSession(id: string) {
  const [session, setSession] = useState<CouncilSession | null>(null)
  const [error, setError] = useState<ApiError | null>(null)
  const [nonce, setNonce] = useState(0)
  const alive = useRef(true)

  useEffect(() => {
    alive.current = true
    setSession(null)
    setError(null)
    getJSON<CouncilSession>(`${SESSIONS_PATH}/${id}`)
      .then((s) => alive.current && setSession(s))
      .catch((e) => alive.current && setError(e instanceof ApiError ? e : new ApiError(0, errorMessage(e))))
    return () => {
      alive.current = false
    }
  }, [id])

  const running = session?.status === "running"
  useEffect(() => {
    if (!running) return
    let poll: ReturnType<typeof setInterval> | undefined
    const es = new EventSource(`${SESSIONS_PATH}/${id}/events`)
    es.addEventListener("session", (e) => {
      try {
        setSession(JSON.parse((e as MessageEvent<string>).data) as CouncilSession)
      } catch {
        // ignore a torn message; the next one is whole
      }
    })
    es.addEventListener("end", () => es.close())
    es.onerror = () => {
      es.close()
      poll ??= setInterval(() => {
        getJSON<CouncilSession>(`${SESSIONS_PATH}/${id}`)
          .then((s) => alive.current && setSession(s))
          .catch(() => undefined)
      }, 2500)
    }
    return () => {
      es.close()
      if (poll) clearInterval(poll)
    }
  }, [id, running, nonce])

  return {
    session,
    error,
    /** Replaces the session (after approve or ask again) and re-follows it. */
    update: (s: CouncilSession) => {
      setSession(s)
      setNonce((n) => n + 1)
    },
  }
}

/** A draft brief that waits for the user, as a "Needs attention" entry. */
export interface CouncilAttention {
  key: string
  id: string
  title: string
  meta: string
}

/** Needs-attention entries for Overview: one per brief waiting for approval. */
export function councilAttention(list: CouncilSummary[]): CouncilAttention[] {
  return list
    .filter((s) => s.status === "draft")
    .map((s) => ({
      key: `council-${s.id}`,
      id: s.id,
      title: `Brief waiting for approval: ${s.title || "untitled"}`,
      meta: `${s.project ? `${s.project} · ` : ""}${s.rounds} ${s.rounds === 1 ? "round" : "rounds"}${s.brief_path ? ` · ${s.brief_path}` : ""}`,
    }))
}
