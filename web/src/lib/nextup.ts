import { useEffect } from "react"
import {
  CircleAlert,
  FileText,
  GitPullRequest,
  KanbanSquare,
  Waypoints,
  Puzzle,
  SquareTerminal,
  LayoutList,
  Vote,
  type LucideIcon,
} from "lucide-react"

import { sendJSON } from "@/lib/api"

export type Kind = "card" | "brief" | "session" | "pr" | "ci" | "linear" | "trello" | "need"
export type ActionKind =
  | "start_work"
  | "fix_ci"
  | "run_council"
  | "resume_work"
  | "open_session"
  | "open_pr"
  | "open_card"
  | "open_brief"
  | "open_link"
  | "open_project"
export type Factor = "urgency" | "unblocking" | "staleness" | "focus" | "effort" | "blocked" | "headroom" | "set_aside"

export interface Part {
  factor: Factor
  points: number
  why: string
}

export interface WorkRequest {
  card?: string
  project: string
  prompt?: string
  provider?: string
  model?: string
  profile?: string
}

export interface Action {
  kind: ActionKind
  label: string
  spends: boolean
  explain: string
  route?: string
  url?: string
  work?: WorkRequest
  council?: { input: string; project?: string }
  note?: string
}

export interface NextItem {
  id: string
  kind: Kind
  source: string
  title: string
  context?: string
  project?: string
  project_name?: string
  confidential: boolean
  due?: string
  labels?: string[]
  updated?: string
  link: { kind: string; label: string; route?: string; url?: string }
  score: number
  parts: Part[]
  action: Action
  agent?: { rank: number; reason?: string; suggested_action?: string; suggested_prompt?: string }
}

export interface Settings {
  focus_project?: string
  pinned: string[]
  schedule_hours: number
  provider?: string
  model?: string
}

export interface NextView {
  items: NextItem[]
  hidden: { id: string; title: string; kind: Kind; until?: string; reason?: string }[]
  errors: { source: string; error: string }[]
  settings: Settings
  ranked?: { at: string; provider: string; model?: string; prompt_version: string; cost_usd: number; ranked: number }
  generated_at: string
}

export const NEXTUP_PATH = "/api/nextup"
export const NEXTUP_POLL_MS = 60_000

export const KIND_INFO: Record<Kind, { label: string; icon: LucideIcon }> = {
  card: { label: "Card", icon: KanbanSquare },
  brief: { label: "Brief", icon: Vote },
  session: { label: "Session", icon: SquareTerminal },
  pr: { label: "Pull request", icon: GitPullRequest },
  ci: { label: "CI", icon: CircleAlert },
  linear: { label: "Linear", icon: Waypoints },
  trello: { label: "Trello", icon: LayoutList },
  need: { label: "Need", icon: Puzzle },
}

export const FACTOR_LABEL: Record<Factor, string> = {
  urgency: "Urgency",
  unblocking: "Unblocks",
  staleness: "Stale",
  focus: "Focus",
  effort: "Effort",
  blocked: "Blocked",
  headroom: "Usage",
  set_aside: "Set aside",
}

export const REASONS: { id: string; label: string }[] = [
  { id: "not-important", label: "Not important" },
  { id: "blocked", label: "Blocked" },
  { id: "someone-else", label: "Someone else's" },
  { id: "later", label: "Later" },
  { id: "other", label: "Other" },
]

export const SCHEDULES = [0, 2, 4, 8, 24]

export const rank = (body: { provider?: string; model?: string } = {}) =>
  sendJSON<NextView & { usage: { cost_usd: number; provider: string; model?: string } }>(`${NEXTUP_PATH}/rank`, "POST", body)
export const snooze = (id: string, days: 1 | 7) => sendJSON<{ id: string; until: string }>(`${NEXTUP_PATH}/snooze`, "POST", { id, days })
export const dismiss = (id: string, reason: string) => sendJSON(`${NEXTUP_PATH}/dismiss`, "POST", { id, reason })
export const restore = (id: string) => sendJSON(`${NEXTUP_PATH}/restore`, "POST", { id })
export const saveSettings = (s: Settings) => sendJSON<Settings>(`${NEXTUP_PATH}/settings`, "PUT", s)

/** The icon for an item's source. */
export const kindIcon = (k: Kind) => KIND_INFO[k]?.icon ?? FileText

/** Whether a scheduled ranking is due: the schedule is on and the last ranking is older than it. */
export function rankDue(v: NextView | null): boolean {
  if (!v || !v.settings.schedule_hours || v.items.length === 0) return false
  if (!v.ranked) return true
  return Date.now() - new Date(v.ranked.at).getTime() >= v.settings.schedule_hours * 3600_000
}

/** When the last scheduled ranking was tried in this window, so the tile and the page do not both ask. */
let autoTried = 0

/**
 * The opt-in schedule: while the app is open, re-rank every N hours (Next up
 * settings; off by default). It never runs when the schedule is off.
 */
export function useScheduledRank(v: NextView | null | undefined, refresh: () => void) {
  useEffect(() => {
    if (!rankDue(v ?? null) || Date.now() - autoTried < 10 * 60_000) return
    autoTried = Date.now()
    rank()
      .then(refresh)
      .catch(() => undefined)
  }, [v?.generated_at]) // once per listing
}
