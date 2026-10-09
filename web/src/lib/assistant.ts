import { getJSON, request, sendJSON } from "@/lib/api"

/* Mirrors internal/assistant: keep these in step with the Go structs. */

export type AssistantProvider = "claude" | "codex" | "grok"
export const ASSISTANT_PROVIDERS: AssistantProvider[] = ["claude", "codex", "grok"]

export type ActionKind =
  | "create_card"
  | "create_project"
  | "create_idea"
  | "create_page"
  | "start_council"
  | "start_work"
  | "add_needs"
  | "link_builds_into"

export interface CatalogEntry {
  kind: ActionKind
  label: string
  args: string
  spends: boolean
}

export interface Catalog {
  actions: CatalogEntry[]
  providers: AssistantProvider[]
  types: ItemType[]
  next: NextStep[]
  sprites: string[]
  prompts: Record<string, string>
}

export interface ApiRequest {
  method: "POST" | "PUT"
  path: string
  body?: unknown
}

export type ProposalStatus = "pending" | "applied" | "skipped" | "failed"

export interface Proposal {
  action: ActionKind | string
  args: Record<string, unknown>
  summary: string
  page?: string
  valid: boolean
  problems: string[]
  spends?: boolean
  requests?: ApiRequest[]
  snippet?: string
  status: ProposalStatus
  note?: string
}

export interface AssistantUsage {
  provider: string
  model?: string
  input_tokens?: number
  output_tokens?: number
  cost_usd?: number
  duration_ms: number
}

export interface Message {
  role: "user" | "assistant"
  text: string
  time: string
  provider?: string
  model?: string
  bot?: string
  project?: string
  page?: string
  prompt_version?: string
  proposals?: Proposal[]
  usage?: AssistantUsage
  error?: string
  note?: string
}

export interface Conversation {
  id: string
  title: string
  project?: string
  bot?: string
  created: string
  updated: string
  messages: Message[]
}

export interface ConversationSummary {
  id: string
  title: string
  project?: string
  bot?: string
  created: string
  updated: string
  messages: number
  pending: number
  cost_usd: number
}

export interface TurnRequest {
  conversation?: string
  message: string
  provider?: string
  model?: string
  bot?: string
  project?: string
  page?: string
}

export interface Bot {
  id: string
  name: string
  provider: AssistantProvider
  profile?: string
  model?: string
  persona: string
  allowed_actions: ActionKind[]
  avatar?: string
  source?: string
}

export interface ImportCandidate {
  key: string
  source: string
  provider: AssistantProvider
  name: string
  description?: string
  model?: string
  has_body: boolean
  file: string
}

export interface ImportList {
  candidates: ImportCandidate[]
  notes: { provider: AssistantProvider; found: number; note: string }[]
}

export type ItemType = "idea" | "feature" | "bug" | "chore" | "question" | "process"
export type NextStep = "council" | "card" | "idea" | "park"

export interface BraindumpItem {
  quote: string
  quote_found: boolean
  restatement: string
  type: ItemType
  project: string
  new_project?: boolean
  next: NextStep
  similar?: { kind: "card" | "idea"; title: string; ref: string }
}

export interface BraindumpResult {
  items: BraindumpItem[]
  provider: string
  model?: string
  prompt_version: string
  usage: AssistantUsage
  notes: string[]
}

export const ASSISTANT_PATH = "/api/assistant"
export const CONVERSATIONS_PATH = `${ASSISTANT_PATH}/conversations`
export const BOTS_PATH = `${ASSISTANT_PATH}/bots`
export const IMPORT_PATH = `${BOTS_PATH}/import`
export const CATALOG_PATH = `${ASSISTANT_PATH}/catalog`

export const conversationPath = (id: string) => `${CONVERSATIONS_PATH}/${encodeURIComponent(id)}`

export const sendTurn = (req: TurnRequest) => sendJSON<{ conversation: Conversation }>(`${ASSISTANT_PATH}/turn`, "POST", req)
export const deleteConversation = (id: string) => sendJSON<null>(conversationPath(id), "DELETE")
export const recordOutcome = (id: string, message: number, proposal: number, status: Exclude<ProposalStatus, "pending">, note?: string) =>
  sendJSON<Conversation>(`${conversationPath(id)}/outcome`, "POST", { message, proposal, status, note })
export const parseBraindump = (text: string, provider?: string, model?: string) =>
  sendJSON<BraindumpResult>(`${ASSISTANT_PATH}/braindump`, "POST", { text, provider: provider || undefined, model: model || undefined })
export const saveBot = (b: Bot) => sendJSON<Bot>(`${BOTS_PATH}/${encodeURIComponent(b.id)}`, "PUT", b)
export const deleteBot = (id: string) => sendJSON<null>(`${BOTS_PATH}/${encodeURIComponent(id)}`, "DELETE")
export const importBot = (key: string, persona: boolean) => sendJSON<Bot>(IMPORT_PATH, "POST", { key, persona })
export const loadCatalog = () => getJSON<Catalog>(CATALOG_PATH)

/** Checks an action again against what is there now (no confirm header: it changes nothing). */
export async function checkAction(action: string, args: Record<string, unknown>, bot?: string): Promise<Proposal> {
  const res = await request(`${ASSISTANT_PATH}/check`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ action: { action, args }, bot: bot || undefined }),
  })
  return (await res.json()) as Proposal
}

/**
 * Sends a checked proposal's requests in order: the same routes the rest of
 * the app uses. Returns the last answer (the card, the session, …).
 */
export async function runRequests(p: Proposal): Promise<unknown> {
  let last: unknown = null
  for (const r of p.requests ?? []) last = await sendJSON<unknown>(r.path, r.method, r.body)
  return last
}

/** A short note for an applied proposal: what was made, where. */
export function appliedNote(p: Proposal, answer: unknown): string {
  const a = (answer ?? {}) as Record<string, unknown>
  switch (p.action) {
    case "create_card":
    case "create_idea":
      return typeof a.id === "string" ? `card ${a.id} on the work board` : "added to the work board"
    case "create_page":
      return `page ${String(p.args.path ?? "")}`
    case "create_project":
      return "appended to projects.yaml"
    case "start_council":
      return typeof a.id === "string" ? `council ${a.id}` : "council started"
    case "start_work":
      return typeof a.id === "string" ? `work ${a.id}` : "session started"
    default:
      return ""
  }
}

export const ACTION_LABEL: Record<string, string> = {
  create_card: "Card",
  create_project: "Project",
  create_idea: "Idea",
  create_page: "Page",
  start_council: "Council",
  start_work: "Work session",
  add_needs: "Needs",
  link_builds_into: "Builds into",
}

export const ALL_ACTIONS: ActionKind[] = [
  "create_card",
  "create_project",
  "create_idea",
  "create_page",
  "start_council",
  "start_work",
  "add_needs",
  "link_builds_into",
]

/** The cheapest model each provider answers with when none is chosen. */
export const DEFAULT_MODEL: Record<string, string> = { claude: "haiku", codex: "", grok: "" }

/** The action a braindump item becomes when applied as a card, an idea or a council braindump. */
export function itemAction(it: BraindumpItem, as: "card" | "idea" | "council"): { action: ActionKind; args: Record<string, unknown> } {
  const quote = it.quote ? `> ${it.quote.replace(/\n/g, "\n> ")}\n` : ""
  if (as === "card") return { action: "create_card", args: { project: it.project, title: it.restatement, body: quote, column: "Inbox" } }
  if (as === "idea") return { action: "create_idea", args: { project: it.project, title: it.restatement, body: quote } }
  return { action: "start_council", args: { project: it.project, braindump: `${it.restatement}\n\nIn my words: ${it.quote || it.restatement}` } }
}

/** What an item becomes by default, from its suggested next step. */
export const defaultAs = (n: NextStep): "card" | "idea" | "council" => (n === "council" ? "council" : n === "card" ? "card" : "idea")
