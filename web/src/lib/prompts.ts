import { sendJSON } from "@/lib/api"

/* Mirrors internal/prompts: keep these in step with the Go structs. */

export type SectionId = "role" | "context" | "contract" | "task" | "constraints" | "verify" | "report"
export type Target = "work" | "council" | "copy"
export type Severity = "error" | "warning" | "info"
export type SourceKind = "project" | "page" | "rules" | "repo" | "card" | "decisions"

export interface Section {
  id: SectionId
  body: string
}

export interface Template {
  id: string
  name: string
  description?: string
  target: Target
  order: number
  version: number
  sections: Section[]
  source: "builtin" | "council" | "user"
  read_only: boolean
  overrides?: boolean
  builtin?: Template
  note?: string
}

export interface SectionMeta {
  id: SectionId
  title: string
  hint: string
}

export interface Variable {
  Name: string
  Hint: string
}

export interface TemplateList {
  templates: Template[]
  sections: SectionMeta[]
  variables: Variable[]
}

export interface Snippet {
  id: string
  name: string
  section?: SectionId
  body: string
  updated: string
}

export interface Ref {
  kind: SourceKind
  project?: string
  path?: string
  board?: string
  id?: string
}

export interface Source extends Ref {
  label: string
  text: string
  format: "text" | "code"
  chars: number
  tokens: number
  confidential: boolean
  truncated?: boolean
  files?: string[]
}

export interface Finding {
  severity: Severity
  code: string
  message: string
  section?: SectionId
}

export interface Rendered {
  text: string
  chars: number
  tokens: number
  tokens_note: string
  unfilled: string[]
  lint: Finding[]
  blocked: boolean
  sources: (Ref & { label: string; chars: number; tokens: number; confidential: boolean; truncated?: boolean })[]
}

export interface ImproveResult {
  sections: Section[]
  text: string
  provider: string
  model?: string
  usage: { provider: string; model?: string; cost_usd?: number; input_tokens?: number; output_tokens?: number; duration_ms: number }
}

export const TEMPLATES_PATH = "/api/prompts/templates"
export const SNIPPETS_PATH = "/api/prompts/snippets"

export const contextPath = (r: Ref) => {
  const q = new URLSearchParams({ kind: r.kind })
  for (const k of ["project", "path", "board", "id"] as const) if (r[k]) q.set(k, r[k] as string)
  return `/api/prompts/context?${q}`
}

export const renderPrompt = (body: { sections: Section[]; context: Ref[]; project?: string; target: Target }) =>
  sendJSON<Rendered>("/api/prompts/render", "POST", body)

export const improvePrompt = (body: { sections: Section[]; provider?: string; model?: string; project?: string }) =>
  sendJSON<ImproveResult>("/api/prompts/improve", "POST", body)

export const saveTemplate = (t: Pick<Template, "id" | "name" | "description" | "target" | "order" | "version" | "sections">) =>
  sendJSON<Template>(`${TEMPLATES_PATH}/${encodeURIComponent(t.id)}`, "PUT", {
    name: t.name,
    description: t.description,
    target: t.target,
    order: t.order,
    version: t.version,
    sections: t.sections,
  })

export const deleteTemplate = (id: string) => sendJSON<null>(`${TEMPLATES_PATH}/${encodeURIComponent(id)}`, "DELETE")

export const saveSnippet = (s: Pick<Snippet, "id" | "name" | "section" | "body">) =>
  sendJSON<Snippet>(`${SNIPPETS_PATH}/${encodeURIComponent(s.id)}`, "PUT", { name: s.name, section: s.section, body: s.body })

export const deleteSnippet = (id: string) => sendJSON<null>(`${SNIPPETS_PATH}/${encodeURIComponent(id)}`, "DELETE")

/** The same id the daemon accepts: lowercase letters, digits and dashes. */
export function slugId(name: string): string {
  return (
    name
      .toLowerCase()
      .replace(/[^a-z0-9]+/g, "-")
      .replace(/^-+|-+$/g, "")
      .slice(0, 60) || "prompt"
  )
}

/** Rough size: four characters to a token. The daemon's count is the same estimate. */
export const estimateTokens = (chars: number) => Math.ceil(chars / 4)

export function formatTokens(n: number): string {
  if (n < 1000) return `${n}`
  if (n < 10000) return `${(n / 1000).toFixed(1)}k`
  return `${Math.round(n / 1000)}k`
}

export const TARGET_INFO: Record<Target, { label: string; blurb: string }> = {
  work: { label: "Work", blurb: "Starts a Work session with this as its task. Work adds its own role and safety rules." },
  council: { label: "Council", blurb: "Convenes the council with this as the braindump." },
  copy: { label: "Copy", blurb: "Copies the text; nothing leaves this machine." },
}

export const SOURCE_INFO: Record<SourceKind, { label: string; needs: "project" | "page" | "card" }> = {
  project: { label: "Project", needs: "project" },
  rules: { label: "Project rules", needs: "project" },
  repo: { label: "Repo map", needs: "project" },
  decisions: { label: "Council decisions", needs: "project" },
  page: { label: "Memory page", needs: "page" },
  card: { label: "Card", needs: "card" },
}

export const refKey = (r: Ref) => [r.kind, r.project ?? "", r.path ?? "", r.board ?? "", r.id ?? ""].join("|")

/*
 * Handing a prompt between Studio, the Council composer and Work's New
 * Session: one sessionStorage entry, written by the sender and taken once by
 * the page that opens next.
 */
export const HANDOFF_KEY = "lucidbench:prompt-handoff"

export interface Handoff {
  /** Where it is going: the page that should take it. */
  to: "studio" | "council" | "work"
  text: string
  project?: string
  /** Studio only: start from this template. */
  template?: string
  from?: "studio" | "council" | "work" | "palette"
}

export function putHandoff(h: Handoff) {
  try {
    sessionStorage.setItem(HANDOFF_KEY, JSON.stringify(h))
  } catch {
    /* storage full or blocked: the target page just opens empty */
  }
}

/** Takes the handoff meant for page `to`, once. */
export function takeHandoff(to: Handoff["to"]): Handoff | null {
  try {
    const raw = sessionStorage.getItem(HANDOFF_KEY)
    if (!raw) return null
    const h = JSON.parse(raw) as Handoff
    if (h.to !== to) return null
    sessionStorage.removeItem(HANDOFF_KEY)
    return h
  } catch {
    return null
  }
}
