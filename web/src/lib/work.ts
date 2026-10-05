import { sendJSON } from "@/lib/api"
import type { Project } from "@/lib/projects"

/* Mirrors internal/work and internal/agentexec: keep these in step with the Go structs. */

export type WorkStatus = "running" | "done" | "failed" | "stopped"
export type Harness = "mine" | "clean"
export type WorkProvider = "claude" | "codex" | "grok"

export interface WorkUsage {
  provider: string
  model?: string
  input_tokens?: number
  output_tokens?: number
  cache_read_tokens?: number
  cache_write_tokens?: number
  cost_usd?: number
  duration_ms: number
  /** How far to trust the figures, e.g. a run stopped before its final cost. */
  note?: string
}

/** Shown wherever a session is started or watched: there is no OS sandbox yet. */
export const SANDBOX_NOTICE =
  "The agent runs as you on this machine. It is asked to stay in its worktree, but this isn't enforced yet. A sandboxed container environment is coming."

export interface FileChange {
  path: string
  added: number
  deleted: number
  binary?: boolean
  patch?: string
  cut?: boolean
}

export interface Diff {
  base: string
  stat: string
  files: FileChange[]
  commits: { sha: string; subject: string }[]
  uncommitted: string[]
  added: number
  deleted: number
}

export interface WorkSession {
  id: string
  provider: WorkProvider
  profile?: string
  harness: Harness
  project: string
  repo_path: string
  repo_hint: string
  branch: string
  base_ref: string
  base_sha: string
  worktree: string
  worktree_hint: string
  title: string
  prompt: string
  card?: string
  board?: string
  brief?: string
  status: WorkStatus
  error?: string
  started: string
  ended?: string
  usage?: WorkUsage
  answer?: string
  events: number
  diff?: Diff
  pr_url?: string
  pushed?: boolean
  removed?: boolean
}

export interface WorkEvent {
  time: string
  kind: "text" | "tool" | "tool_result" | "diff" | "approval" | "error" | "done"
  title?: string
  body?: string
}

export interface StartRequest {
  card?: string
  project?: string
  prompt?: string
  provider: WorkProvider
  profile?: string
  harness: Harness
}

/** A project as /api/projects returns it, with the checkout Work needs. */
export type WorkProject = Project & { local_path?: string }

export const WORK_POLL_MS = 4000
export const WORK_PROVIDERS: WorkProvider[] = ["claude", "codex", "grok"]

export const sessionsPath = "/api/work/sessions"
export const sessionPath = (id: string) => `${sessionsPath}/${encodeURIComponent(id)}`

export const startSession = (req: StartRequest) => sendJSON<WorkSession>(sessionsPath, "POST", req)
export const stopSession = (id: string) => sendJSON<WorkSession>(`${sessionPath(id)}/stop`, "POST")
export const openPR = (id: string) => sendJSON<WorkSession>(`${sessionPath(id)}/pr`, "POST")
export const removeWorktree = (id: string, discard: boolean) =>
  sendJSON<WorkSession>(`${sessionPath(id)}/remove`, "POST", { discard })

/** The harness a provider starts with: your own for Claude, clean for the rest. */
export const defaultHarness = (p: WorkProvider): Harness => (p === "claude" ? "mine" : "clean")

export const STATUS_INFO: Record<WorkStatus, { label: string; tone: "info" | "success" | "danger" | "neutral" }> = {
  running: { label: "Running", tone: "info" },
  done: { label: "Done", tone: "success" },
  failed: { label: "Failed", tone: "danger" },
  stopped: { label: "Stopped", tone: "neutral" },
}

/** A finished session whose diff still waits for a PR or a cleanup. */
export const needsReview = (s: WorkSession) => (s.status === "done" || s.status === "failed") && !s.pr_url && !s.removed

/** "$0.04", "<$0.01", or "-" when the CLI reported no cost. */
export function formatCost(usd?: number): string {
  if (!usd) return "-"
  if (usd < 0.01) return "<$0.01"
  return `$${usd.toFixed(usd < 10 ? 2 : 0)}`
}

/** "48s", "3m 12s", "1h 04m". */
export function formatElapsed(ms: number): string {
  const s = Math.max(0, Math.round(ms / 1000))
  if (s < 60) return `${s}s`
  const m = Math.floor(s / 60)
  if (m < 60) return `${m}m ${String(s % 60).padStart(2, "0")}s`
  return `${Math.floor(m / 60)}h ${String(m % 60).padStart(2, "0")}m`
}

export function elapsedOf(s: WorkSession, now: number): number {
  const end = s.ended ? Date.parse(s.ended) : now
  return end - Date.parse(s.started)
}

/** Replaces a home folder at the start of a path with "~". */
export function homeHint(p: string): string {
  return p
    .replace(/^[A-Za-z]:[\\/]+Users[\\/]+[^\\/]+/i, "~")
    .replace(/^\/(Users|home)\/[^/]+/, "~")
}

/** "lucid/<id>-add-a-readme-line": the branch a title would get, for previews. */
export function branchPreview(title: string): string {
  const slug =
    title
      .toLowerCase()
      .replace(/[^a-z0-9]+/g, "-")
      .replace(/^-+|-+$/g, "")
      .slice(0, 40)
      .replace(/-+$/, "") || "task"
  return `lucid/<id>-${slug}`
}

/* ---------- from events to steps ---------- */

export type ToolKind = "read" | "search" | "edit" | "command" | "plan" | "web" | "agent" | "other"

export interface ToolItem {
  name: string
  kind: ToolKind
  /** The file, command, pattern or URL the call was about. */
  target: string
  input: Record<string, unknown> | null
  result?: string
  failed?: boolean
  diff?: string
  done: boolean
  time: string
}

export type Step =
  | { type: "message"; key: string; text: string; time: string }
  | { type: "tools"; key: string; kind: ToolKind; items: ToolItem[] }
  | { type: "error"; key: string; title?: string; text: string; time: string }
  | { type: "done"; key: string; ok: boolean; time: string }

function kindOf(name: string): ToolKind {
  const n = name.toLowerCase()
  if (n === "todowrite" || n.includes("todo") || n.includes("plan")) return "plan"
  if (n === "task" || n.includes("agent")) return "agent"
  if (n.includes("web") || n.includes("fetch")) return "web"
  if (n === "read" || n.includes("read") || n === "view" || n === "cat") return "read"
  if (/(edit|write|replace|patch|create|notebook|apply)/.test(n)) return "edit"
  if (/(bash|shell|command|terminal|exec|run)/.test(n)) return "command"
  if (/(grep|glob|search|find|list|^ls$)/.test(n)) return "search"
  return "other"
}

function parseInput(body?: string): Record<string, unknown> | null {
  if (!body) return null
  try {
    const v: unknown = JSON.parse(body)
    return v && typeof v === "object" && !Array.isArray(v) ? (v as Record<string, unknown>) : null
  } catch {
    return null
  }
}

const str = (v: unknown) => (typeof v === "string" ? v : "")

function targetOf(kind: ToolKind, input: Record<string, unknown> | null, body: string): string {
  if (!input) return body.trim()
  const pick = (...ks: string[]) => ks.map((k) => str(input[k])).find(Boolean) ?? ""
  switch (kind) {
    case "command":
      return pick("command", "cmd", "description")
    case "search":
      return pick("pattern", "query", "glob", "path", "dir")
    case "web":
      return pick("url", "query")
    case "agent":
      return pick("description", "prompt")
    default:
      return pick("file_path", "path", "filePath", "target_file", "notebook_path", "file")
  }
}

/** Strips the trailing " (error)", " (failed)" or " (exit 1)" a result title carries. */
const baseTitle = (t: string) => t.replace(/ \((error|failed|exit \d+)\)$/, "")

/** Makes a path relative to the worktree, with forward slashes. */
export function relPath(p: string, worktree: string): string {
  const norm = (s: string) => s.replace(/\\/g, "/").replace(/\/+$/, "")
  const pp = norm(p)
  const w = norm(worktree)
  if (w && pp.toLowerCase().startsWith(w.toLowerCase() + "/")) return pp.slice(w.length + 1)
  return pp
}

/**
 * Turns the normalised event stream into plain-language steps: text becomes
 * messages, runs of reads or searches fold into one row, each edit and each
 * command is its own row with its diff or output, errors stand out.
 */
export function buildSteps(events: WorkEvent[], worktree: string): Step[] {
  const steps: Step[] = []
  const open: ToolItem[] = [] // calls still waiting for their result
  const last = () => steps[steps.length - 1]
  events.forEach((e, i) => {
    const key = `e${i}`
    switch (e.kind) {
      case "text": {
        const text = (e.body ?? "").trim()
        if (text) steps.push({ type: "message", key, text, time: e.time })
        break
      }
      case "tool": {
        const name = e.title ?? "tool"
        // Codex names a command call "command" and puts the command in the body.
        const kind = name === "command" ? "command" : kindOf(name)
        const input = name === "command" ? { command: e.body ?? "" } : parseInput(e.body)
        let target = targetOf(kind, input, e.body ?? "")
        if (kind === "read" || kind === "edit") target = relPath(target, worktree)
        const item: ToolItem = { name, kind, target, input, done: false, time: e.time }
        open.push(item)
        const prev = last()
        if (prev?.type === "tools" && prev.kind === kind && (kind === "read" || kind === "search")) prev.items.push(item)
        else steps.push({ type: "tools", key, kind, items: [item] })
        break
      }
      case "tool_result": {
        const t = baseTitle(e.title ?? "")
        let idx = -1
        for (let j = open.length - 1; j >= 0; j--) {
          if (open[j].name === t || (open[j].kind === "command" && open[j].target === t)) {
            idx = j
            break
          }
        }
        if (idx < 0) idx = open.length - 1
        if (idx < 0) break
        const item = open.splice(idx, 1)[0]
        item.done = true
        item.result = e.body ?? ""
        item.failed = /\((error|failed|exit \d+)\)$/.test(e.title ?? "")
        break
      }
      case "diff": {
        const path = relPath(e.title ?? "", worktree)
        // An edit call's diff follows the call; Codex reports file changes on their own.
        for (let j = steps.length - 1; j >= Math.max(0, steps.length - 4); j--) {
          const s = steps[j]
          if (s.type !== "tools" || s.kind !== "edit") continue
          const it = s.items.find((x) => !x.diff && (x.target === path || path.endsWith(x.target) || x.target.endsWith(path)))
          if (it) {
            it.diff = e.body ?? ""
            return
          }
        }
        steps.push({
          type: "tools",
          key,
          kind: "edit",
          items: [{ name: "file_change", kind: "edit", target: path, input: null, diff: e.body ?? "", done: true, time: e.time }],
        })
        break
      }
      case "error":
        steps.push({ type: "error", key, title: e.title, text: e.body ?? "", time: e.time })
        break
      case "done":
        steps.push({ type: "done", key, ok: e.title === "ok", time: e.time })
        break
    }
  })
  return steps
}

/** One plain-language line for a run of tool calls. */
export function stepLabel(kind: ToolKind, items: ToolItem[]): { verb: string; object: string } {
  const n = items.length
  const one = items[0]
  const plural = (w: string) => `${n} ${w}${n === 1 ? "" : "s"}`
  switch (kind) {
    case "read":
      return n === 1 ? { verb: "Read", object: one.target || "a file" } : { verb: "Read", object: plural("file") }
    case "search":
      if (n > 1) return { verb: "Searched", object: `${n} times` }
      if (/^(ls|list)/i.test(one.name)) return { verb: "Listed", object: one.target || "the folder" }
      if (/glob|find/i.test(one.name)) return { verb: "Looked for files matching", object: one.target }
      return { verb: "Searched for", object: one.target }
    case "edit": {
      const wrote = one.name === "Write" || one.name === "write" || one.name === "create"
      return { verb: wrote ? "Wrote" : "Edited", object: one.target || "a file" }
    }
    case "command": {
      const [first, ...rest] = one.target.split("\n")
      return { verb: "Ran", object: rest.some((l) => l.trim()) ? `${first} …` : first }
    }
    case "plan":
      return { verb: "Updated", object: "the plan" }
    case "web":
      return { verb: "Looked up", object: one.target }
    case "agent":
      return { verb: "Asked a helper agent", object: one.target }
    default:
      return { verb: "Used", object: one.name }
  }
}

/** Counts the added and removed lines of a diff body. */
export function diffCounts(diff: string): { added: number; deleted: number } {
  let added = 0
  let deleted = 0
  for (const l of diff.split("\n")) {
    if (l.startsWith("+++") || l.startsWith("---")) continue
    if (l.startsWith("+")) added++
    else if (l.startsWith("-")) deleted++
  }
  return { added, deleted }
}

/** The todo list a TodoWrite call carries, if any. */
export function todosOf(input: Record<string, unknown> | null): { content: string; status: string }[] {
  const t = input?.todos
  if (!Array.isArray(t)) return []
  return t.map((x: Record<string, unknown>) => ({ content: str(x.content) || str(x.activeForm), status: str(x.status) }))
}
