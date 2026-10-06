import { getJSON, sendJSON } from "@/lib/api"
import type { Card } from "@/lib/boards"

export const LINEAR_POLL_MS = 60000

/** GET /api/linear/status: whether the connector can work. Never the key. */
export interface LinearStatus {
  configured: boolean
  token_ref: string
  mcp: boolean
}

export interface LinearState {
  id: string
  name: string
  type: "triage" | "backlog" | "unstarted" | "started" | "completed" | "canceled"
  color?: string
  position: number
}

export interface LinearPerson {
  id: string
  name: string
}

export interface LinearCycle {
  id: string
  number: number
  name?: string
  starts_at: string
  ends_at: string
  progress: number
}

export interface LinearTeam {
  id: string
  key: string
  name: string
  states: LinearState[]
  active_cycle?: LinearCycle
}

export interface LinearProject {
  id: string
  name: string
  state?: string
  team_ids: string[]
}

export interface LinearMeta {
  viewer: LinearPerson
  teams: LinearTeam[]
  projects: LinearProject[]
}

export interface LinearIssue {
  id: string
  identifier: string
  title: string
  url: string
  priority: number
  priority_label?: string
  updated_at?: string
  state: LinearState
  team_id: string
  team_key: string
  project_id?: string
  project_name?: string
  assignee?: LinearPerson
  labels: { name: string; color?: string }[]
}

export interface LinearIssues {
  issues: LinearIssue[]
  truncated: boolean
}

export interface LinearFilter {
  team: string
  project: string
  me: boolean
  done: boolean
}

export function issuesPath(f: LinearFilter): string {
  const q = new URLSearchParams()
  if (f.team) q.set("team", f.team)
  if (f.project) q.set("project", f.project)
  if (f.me) q.set("me", "1")
  if (f.done) q.set("done", "1")
  const s = q.toString()
  return `/api/linear/issues${s ? `?${s}` : ""}`
}

export interface LinkResult {
  card: Card
  issue: LinearIssue
}

export const linearApi = {
  status: () => getJSON<LinearStatus>("/api/linear/status"),
  meta: () => getJSON<LinearMeta>("/api/linear/meta"),
  promote: (board: string, card: string, team: string, project: string) =>
    sendJSON<LinkResult>("/api/linear/promote", "POST", { board, card, team, ...(project ? { project } : {}) }),
  link: (board: string, card: string, identifier: string) => sendJSON<LinkResult>("/api/linear/link", "POST", { board, card, identifier }),
}

/** Columns left to right: the order Linear's own board uses. */
const TYPE_RANK: Record<string, number> = { triage: 0, backlog: 1, unstarted: 2, started: 3, completed: 4, canceled: 5 }

export interface StateColumn {
  key: string
  name: string
  type: string
  color?: string
  issues: LinearIssue[]
}

/**
 * Groups issues by workflow state. Teams may name states differently, so
 * columns are keyed by name and ordered by type, then position. States of the
 * selected teams with no issue still get a column, so the board keeps its shape.
 */
export function columnsFor(issues: LinearIssue[], teams: LinearTeam[], includeDone: boolean): StateColumn[] {
  const cols = new Map<string, StateColumn & { position: number }>()
  const add = (s: LinearState) => {
    if (!includeDone && (s.type === "completed" || s.type === "canceled")) return null
    const key = s.name.toLowerCase()
    let c = cols.get(key)
    if (!c) {
      c = { key, name: s.name, type: s.type, color: s.color, issues: [], position: s.position }
      cols.set(key, c)
    }
    return c
  }
  for (const t of teams) for (const s of t.states) add(s)
  for (const i of issues) add(i.state)?.issues.push(i)
  return [...cols.values()]
    .sort((a, b) => (TYPE_RANK[a.type] ?? 9) - (TYPE_RANK[b.type] ?? 9) || a.position - b.position)
    .map(({ position: _p, ...c }) => c)
}

export const PRIORITY_TONE: Record<number, string> = {
  1: "var(--danger)",
  2: "var(--warning)",
  3: "var(--info)",
  4: "var(--neutral)",
}
