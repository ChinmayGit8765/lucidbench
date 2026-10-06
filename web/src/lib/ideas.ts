import type { Tone } from "@/components/ui/badge"
import type { Card } from "@/lib/boards"
import type { CouncilSession } from "@/lib/council"
import type { PRChecks, PRState, WorkUsage } from "@/lib/work"

/* Mirrors internal/ideas: keep these in step with the Go structs. */

export type Stage = "braindump" | "brief" | "card" | "work" | "pr" | "merged"
export const STAGES: Stage[] = ["braindump", "brief", "card", "work", "pr", "merged"]

export const STAGE_INFO: Record<Stage, { label: string; tone: Tone; blurb: string }> = {
  braindump: { label: "Braindump", tone: "neutral", blurb: "The council has it, or it stopped before a brief" },
  brief: { label: "Brief", tone: "warning", blurb: "A brief waits for approval" },
  card: { label: "Card", tone: "info", blurb: "On the board, no agent yet" },
  work: { label: "Work", tone: "info", blurb: "An agent ran on it" },
  pr: { label: "PR", tone: "success", blurb: "A pull request is open" },
  merged: { label: "Merged", tone: "success", blurb: "The PR is merged" },
}

export interface IdeaSummary {
  id: string
  title: string
  stage: Stage
  status: string
  project?: string
  created?: string
  updated?: string
  cost_usd: number
  council?: string
  card?: string
  sessions: number
  pr_url?: string
  pr_state?: PRState
}

export interface IdeaBrief {
  path: string
  title?: string
  status?: string
  body?: string
  exists: boolean
  confidential?: boolean
}

export interface CardMove {
  time: string
  column: string
  by: string
}

export interface IdeaCard extends Card {
  board: string
  board_title: string
  history: CardMove[]
  history_note: string
}

export interface IdeaSession {
  id: string
  title: string
  status: "running" | "done" | "failed" | "stopped"
  provider: string
  model?: string
  branch: string
  started: string
  ended?: string
  diff_stat?: string
  added: number
  deleted: number
  files: number
  commits: { sha: string; subject: string }[]
  cost_usd: number
  pr_url?: string
  pr_state?: PRState
  pr_checks?: PRChecks
  pr_checked?: string
  usage?: WorkUsage
  error?: string
}

export interface IdeaEvent {
  time: string
  untimed?: boolean
  kind: "council" | "critique" | "brief" | "approve" | "card" | "work" | "pr" | "merged" | "checks"
  title: string
  detail?: string
  provider?: string
  link?: string
}

export interface IdeaCost {
  provider: string
  part: "council" | "work"
  cost_usd: number
  calls: number
  input_tokens: number
  output_tokens: number
}

export interface Idea extends IdeaSummary {
  input?: string
  council_session?: CouncilSession
  brief?: IdeaBrief
  card_view?: IdeaCard
  work: IdeaSession[]
  links: string[]
  timeline: IdeaEvent[]
  costs: IdeaCost[]
  missing: string[]
}

export const IDEAS_PATH = "/api/ideas"
export const IDEAS_POLL_MS = 15000
export const ideaPath = (id: string) => `${IDEAS_PATH}/${encodeURIComponent(id)}`

/** The idea id of a card. The daemon resolves it to the council's idea when the card came from one. */
export const cardIdeaId = (board: string, cardId: string) => `card:${board}/${cardId}`

/** How far along the loop a stage is, 0 to 5. */
export const stageIndex = (s: Stage) => STAGES.indexOf(s)
