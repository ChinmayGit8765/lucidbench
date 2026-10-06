/*
 * Sections: declarative widgets on Overview and on a project's page. Each is
 * JSON kept by the daemon in <DataDir>/sections (internal/sections), checked
 * there against an allowlist of read-only routes. This file reads them and
 * evaluates one against its route's answer: rows, filters, sort, limit and
 * fields. Nothing here runs code from a section; every value is shown as text.
 */
import { sendJSON } from "@/lib/api"

export type SectionView = "stat" | "list" | "table" | "bars" | "markdown"
export type Placement = "overview" | "project"
export type FieldFormat = "text" | "number" | "usd" | "percent" | "date" | "relative" | "link" | "badge"

export interface SectionField {
  path: string
  label?: string
  format?: FieldFormat
  agg?: "count" | "sum" | "avg" | "min" | "max"
}

export interface SectionFilter {
  field: string
  op: string
  value?: string | number | boolean | string[]
}

export interface Section {
  id: string
  title: string
  description?: string
  placement?: Placement
  source: { api: string; params?: Record<string, string> }
  view: SectionView
  rows?: string
  filter?: SectionFilter[]
  sort?: { field: string; desc?: boolean }
  limit?: number
  fields: SectionField[]
  refresh_s?: number
}

export interface ParamRule {
  name: string
  kind: "id" | "int" | "text"
  min?: number
  max?: number
  required?: boolean
  doc: string
}

export interface SectionAPI {
  route: string
  doc: string
  params: ParamRule[]
  rows: string[]
}

export interface Catalog {
  apis: SectionAPI[]
  templates: Section[]
  views: string[]
  formats: string[]
  ops: string[]
  prompt_version: string
}

export interface Listing {
  sections: Section[]
  broken: { file: string; error: string }[]
}

export interface Problem {
  path: string
  message: string
}

export interface Generated {
  section: Section
  provider: string
  model?: string
  prompt_version: string
  usage: { provider: string; model?: string; input_tokens?: number; output_tokens?: number; cost_usd?: number; duration_ms: number }
}

export const SECTIONS_PATH = "/api/sections"
export const CATALOG_PATH = "/api/sections/catalog"

export const addTemplate = (id: string) => sendJSON<Section>(`/api/sections/templates/${encodeURIComponent(id)}`, "POST")
export const saveSection = (s: Section) => sendJSON<Section>(`/api/sections/${encodeURIComponent(s.id)}`, "PUT", s)
export const removeSection = (id: string) => sendJSON<null>(`/api/sections/${encodeURIComponent(id)}`, "DELETE")
export const generateSection = (body: { description: string; provider?: string; profile?: string; model?: string; placement?: Placement }) =>
  sendJSON<Generated>("/api/sections/generate", "POST", body)

/**
 * The URL a section reads, or null when its route is not on the allowlist
 * the daemon sent. Path params fill {placeholders}; the rest go in the query.
 */
export function sectionURL(s: Section, apis: SectionAPI[] | null): string | null {
  const api = apis?.find((a) => a.route === s.source.api)
  if (!api) return null
  let path = api.route
  const query = new URLSearchParams()
  for (const rule of api.params) {
    const v = s.source.params?.[rule.name]
    if (v === undefined) continue
    if (path.includes(`{${rule.name}}`)) path = path.replace(`{${rule.name}}`, encodeURIComponent(v))
    else query.set(rule.name, v)
  }
  if (/[{}]/.test(path)) return null
  const q = query.toString()
  return q ? `${path}?${q}` : path
}

/** Reads a dotted path ("usage.cost_usd", "cards.0.title"). */
export function getPath(v: unknown, path: string): unknown {
  if (!path) return v
  let cur: unknown = v
  for (const key of path.split(".")) {
    if (cur === null || cur === undefined || typeof cur !== "object") return undefined
    if (!Object.prototype.hasOwnProperty.call(cur, key)) return undefined
    cur = (cur as Record<string, unknown>)[key]
  }
  return cur
}

type Row = Record<string, unknown>

/** The rows a path names; a "*" segment takes every value of a mapping (with _key). */
export function rowsOf(data: unknown, path: string | undefined): Row[] {
  let items: { v: unknown; key?: string }[] = [{ v: data }]
  for (const seg of path ? path.split(".") : []) {
    const next: { v: unknown; key?: string }[] = []
    for (const it of items) {
      if (it.v === null || typeof it.v !== "object") continue
      if (seg === "*") {
        for (const [k, v] of Object.entries(it.v as Row)) next.push({ v, key: k })
      } else if (Object.prototype.hasOwnProperty.call(it.v, seg)) {
        next.push({ v: (it.v as Row)[seg], key: it.key })
      }
    }
    items = next
  }
  const out: Row[] = []
  for (const it of items) {
    const list = Array.isArray(it.v) ? it.v : [it.v]
    for (const r of list) {
      if (r && typeof r === "object" && !Array.isArray(r)) out.push(it.key !== undefined ? { _key: it.key, ...(r as Row) } : (r as Row))
    }
  }
  return out
}

const DAY = 86_400_000

function asTime(v: unknown): number | null {
  if (typeof v !== "string" || !v) return null
  const t = Date.parse(/^\d{4}-\d{2}-\d{2}$/.test(v) ? `${v}T23:59:59` : v)
  return Number.isNaN(t) ? null : t
}

/** Whether a row passes one filter; projectId fills the {project} token. */
export function passes(row: Row, f: SectionFilter, now: number, projectId?: string): boolean {
  const v = getPath(row, f.field)
  const want = f.value === "{project}" ? projectId : f.value
  switch (f.op) {
    case "eq":
      return v === want || (typeof want === "boolean" && !want && (v === undefined || v === null))
    case "ne":
      return v !== want && !(typeof want === "boolean" && !want && (v === undefined || v === null))
    case "in":
      return Array.isArray(want) && want.includes(String(v))
    case "nin":
      return Array.isArray(want) && !want.includes(String(v))
    case "contains":
      return typeof v === "string" && typeof want === "string" && v.toLowerCase().includes(want.toLowerCase())
    case "exists":
      return v !== undefined && v !== null && v !== ""
    case "missing":
      return v === undefined || v === null || v === ""
    case "gt":
      return typeof v === "number" && typeof want === "number" && v > want
    case "lt":
      return typeof v === "number" && typeof want === "number" && v < want
    case "within_days": {
      const t = asTime(v)
      return t !== null && typeof want === "number" && t <= now + want * DAY
    }
    case "since_days": {
      const t = asTime(v)
      return t !== null && typeof want === "number" && t >= now - want * DAY
    }
  }
  return false
}

function compare(a: unknown, b: unknown): number {
  if (a === b) return 0
  if (a === undefined || a === null || a === "") return 1
  if (b === undefined || b === null || b === "") return -1
  if (typeof a === "number" && typeof b === "number") return a - b
  return String(a).localeCompare(String(b))
}

/** Rows after the section's filters, sort and limit. */
export function evaluate(s: Section, data: unknown, now: number, projectId?: string): Row[] {
  let rows = rowsOf(data, s.rows)
  for (const f of s.filter ?? []) rows = rows.filter((r) => passes(r, f, now, projectId))
  if (s.sort) {
    const { field, desc } = s.sort
    rows = [...rows].sort((a, b) => {
      const c = compare(getPath(a, field), getPath(b, field))
      return desc ? -c : c
    })
  }
  if (s.limit) rows = rows.slice(0, s.limit)
  return rows
}

/** A stat's number: an aggregate over the rows, or a value from the answer. */
export function statValue(s: Section, data: unknown, now: number, projectId?: string): unknown {
  const f = s.fields[0]
  if (!f) return undefined
  if (!f.agg) return getPath(s.rows ? rowsOf(data, s.rows)[0] : data, f.path)
  const rows = evaluate({ ...s, limit: 0 }, data, now, projectId)
  if (f.agg === "count") return rows.length
  const nums = rows.map((r) => getPath(r, f.path)).filter((x): x is number => typeof x === "number")
  if (nums.length === 0) return f.agg === "sum" ? 0 : undefined
  switch (f.agg) {
    case "sum":
      return nums.reduce((a, b) => a + b, 0)
    case "avg":
      return nums.reduce((a, b) => a + b, 0) / nums.length
    case "min":
      return Math.min(...nums)
    case "max":
      return Math.max(...nums)
  }
  return undefined
}

/** A field's label: its own, or its path's last part made readable. */
export const fieldLabel = (f: SectionField) => f.label || (f.path.split(".").pop() ?? f.path).replace(/^_key$/, "key").replace(/_/g, " ")

/** Only plain web links are ever made clickable. */
export const safeLink = (v: unknown): string | null => (typeof v === "string" && /^https?:\/\/[^\s<>"']+$/i.test(v) ? v : null)

/** A projects section filter that names {project}, made concrete for display. */
export const usesProject = (s: Section) => (s.filter ?? []).some((f) => f.value === "{project}")
