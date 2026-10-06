/*
 * The AI team (lucid-team.yaml version 1, internal/team): who fills each role
 * of the loop for a project, read from its checkout's .lucid/team.yaml, the
 * data folder, or the config default. See docs/TEAM-SPEC.md.
 */
import { sendJSON } from "@/lib/api"

export type TeamProvider = "claude" | "codex" | "grok"
export const TEAM_PROVIDERS: TeamProvider[] = ["claude", "codex", "grok"]

export interface TeamRole {
  provider: string
  profile?: string
  model?: string
  mcp_allow?: string[]
  allowed_commands?: string[]
  budget_usd?: number
}

export type RoleName = "proposer" | "critic" | "builder" | "reviewer" | "scout"

export interface Team {
  version: number
  roles: {
    proposer?: TeamRole
    critic?: TeamRole[]
    builder?: TeamRole
    reviewer?: TeamRole
    scout?: TeamRole
  }
  gates: { approve_brief?: string; open_pr?: string; merge?: string }
}

export interface TeamProblem {
  severity: "error" | "warning" | "info"
  code: string
  role?: string
  message: string
}

export interface TeamEstimate {
  role: string
  provider: string
  avg_usd: number
  runs: number
  budget_usd?: number
  over: boolean
}

export type TeamSource = "repo" | "data" | "config" | "builtin"

export interface TeamView {
  project: string
  project_name: string
  team: Team
  source: TeamSource
  path_hint?: string
  targets: { repo?: string; data: string }
  problems: TeamProblem[]
  estimates: TeamEstimate[]
  confidential: boolean
}

export interface TeamCheck {
  team: Team | null
  problems: TeamProblem[]
  estimates: TeamEstimate[]
  valid: boolean
}

/** A project's builder, as GET /api/work/defaults returns it (null without a team). */
export interface WorkBuilder {
  provider: string
  model?: string
  profile?: string
  allowed_commands?: string[]
  source: TeamSource
  budget_usd?: number
  avg_usd?: number
  runs?: number
}

/** What each role does in the loop. */
export const ROLE_INFO: Record<RoleName, string> = {
  proposer: "Drafts and revises the brief in the Council",
  critic: "Looks for what would go wrong, in the Council",
  builder: "Works on the card in Work, in its own worktree",
  reviewer: "Reviews a finished session (recorded for now)",
  scout: "Reads and reports (recorded for now)",
}

/** Model names each CLI accepts, as suggestions; any name can be typed. */
export const MODEL_HINTS: Record<TeamProvider, string[]> = {
  claude: ["haiku", "sonnet", "opus"],
  codex: ["gpt-5-codex", "gpt-5", "o3"],
  grok: ["grok-4", "grok-code-fast-1"],
}

export const SOURCE_LABEL: Record<TeamSource, string> = {
  repo: "the project's repository",
  data: "Lucidbench's data folder",
  config: "the default team in config.yaml",
  builtin: "Lucidbench's defaults",
}

export const teamPath = (project: string) => `/api/projects/${encodeURIComponent(project)}/team`
export const saveTeam = (project: string, team: Team, target: "repo" | "data") => sendJSON<TeamView>(teamPath(project), "PUT", { team, target })
export const checkTeam = (body: { yaml?: string; team?: Team; project?: string }) => sendJSON<TeamCheck>("/api/team/validate", "POST", body)

/** The seats of a team in display order, with the label the daemon uses ("critic 2"). */
export function seats(t: Team): { name: RoleName; label: string; index: number; role: TeamRole }[] {
  const out: { name: RoleName; label: string; index: number; role: TeamRole }[] = []
  if (t.roles.proposer) out.push({ name: "proposer", label: "proposer", index: 0, role: t.roles.proposer })
  const critics = t.roles.critic ?? []
  critics.forEach((r, i) => out.push({ name: "critic", label: critics.length > 1 ? `critic ${i + 1}` : "critic", index: i, role: r }))
  for (const n of ["builder", "reviewer", "scout"] as const) {
    const r = t.roles[n]
    if (r) out.push({ name: n, label: n, index: 0, role: r })
  }
  return out
}
