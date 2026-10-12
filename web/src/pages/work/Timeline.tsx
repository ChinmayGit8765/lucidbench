import { useMemo, useState, type ReactNode } from "react"
import {
  Bot,
  Camera,
  ChevronRight,
  CircleCheck,
  CircleX,
  FilePen,
  FilePlus2,
  FileText,
  GitMerge,
  Globe,
  Hourglass,
  Info,
  ListTodo,
  OctagonX,
  Search,
  SquareTerminal,
  TriangleAlert,
  User,
  Wrench,
  type LucideIcon,
} from "lucide-react"

import { BriefBody, parseBrief } from "@/components/Brief"
import { ProviderTile, providerInfo } from "@/components/ProviderMark"
import { cn, plural } from "@/lib/utils"
import {
  diffCounts,
  elapsedOf,
  formatCost,
  formatElapsed,
  stepLabel,
  todosOf,
  turnElapsed,
  turnModeLabel,
  type Step,
  type ToolItem,
  type ToolKind,
  type WorkSession,
  type WorkTurn,
} from "@/lib/work"

/* ---------- small renderers ---------- */

/** Inline `code` and **bold**. */
function Inline({ text }: { text: string }) {
  const parts = text.split(/(`[^`\n]+`|\*\*[^*\n]+\*\*)/g)
  return (
    <>
      {parts.map((p, i) =>
        p.startsWith("`") && p.endsWith("`") && p.length > 1 ? (
          <code key={i} className="rounded bg-muted px-1 py-px font-mono text-[0.85em]">
            {p.slice(1, -1)}
          </code>
        ) : p.startsWith("**") && p.endsWith("**") && p.length > 4 ? (
          <strong key={i} className="font-semibold">
            {p.slice(2, -2)}
          </strong>
        ) : (
          <span key={i}>{p}</span>
        ),
      )}
    </>
  )
}

/** A light Markdown view: fenced code, bullet lists, headings and paragraphs. */
export function Prose({ text, className }: { text: string; className?: string }) {
  const blocks: ReactNode[] = []
  const chunks = text.split(/```[^\n]*\n([\s\S]*?)```/g)
  chunks.forEach((chunk, ci) => {
    if (ci % 2 === 1) {
      blocks.push(
        <pre key={`c${ci}`} className="overflow-x-auto rounded-lg border bg-background px-3 py-2 font-mono text-xs leading-5">
          {chunk.replace(/\n$/, "")}
        </pre>,
      )
      return
    }
    chunk.split(/\n{2,}/).forEach((para, pi) => {
      const lines = para.split("\n").filter((l) => l.trim() !== "")
      if (lines.length === 0) return
      const key = `p${ci}-${pi}`
      if (lines.every((l) => /^\s*([-*]|\d+\.)\s+/.test(l))) {
        blocks.push(
          <ul key={key} className="ml-4 list-disc space-y-0.5 marker:text-subtle-foreground">
            {lines.map((l, i) => (
              <li key={i}>
                <Inline text={l.replace(/^\s*([-*]|\d+\.)\s+/, "")} />
              </li>
            ))}
          </ul>,
        )
      } else if (/^#{1,4}\s/.test(lines[0]) && lines.length === 1) {
        blocks.push(
          <p key={key} className="font-semibold">
            <Inline text={lines[0].replace(/^#+\s*/, "")} />
          </p>,
        )
      } else {
        blocks.push(
          <p key={key} className="whitespace-pre-wrap">
            <Inline text={lines.join("\n")} />
          </p>,
        )
      }
    })
  })
  return <div className={cn("space-y-2 break-words text-sm leading-6", className)}>{blocks}</div>
}

/** A diff with added and removed lines coloured: agent edits ("- "/"+ ") or git patches. */
export function DiffView({ text, className }: { text: string; className?: string }) {
  let lines = text.replace(/\n$/, "").split("\n")
  // A git patch: the file is already named above, so start at the first hunk.
  const hunk = lines.findIndex((l) => l.startsWith("@@"))
  if (lines[0]?.startsWith("diff --git") && hunk > 0) lines = lines.slice(hunk)
  return (
    <pre className={cn("max-h-96 overflow-auto rounded-lg border bg-background py-1.5 font-mono text-xs leading-5", className)}>
      {lines.map((l, i) => {
        const meta = /^(diff --git|index |--- |\+\+\+ |new file|deleted file|similarity|rename )/.test(l)
        const tone = meta
          ? "text-subtle-foreground"
          : l.startsWith("@@")
            ? "bg-info-soft/60 text-info-fg"
            : l.startsWith("+")
              ? "bg-success-soft/70 text-success-fg"
              : l.startsWith("-")
                ? "bg-danger-soft/70 text-danger-fg"
                : "text-muted-foreground"
        return (
          <div key={i} className={cn("whitespace-pre px-3", tone)}>
            {l || " "}
          </div>
        )
      })}
    </pre>
  )
}

function Output({ text, failed }: { text: string; failed?: boolean }) {
  const t = text.replace(/\n$/, "")
  if (!t.trim()) return <p className="px-1 text-2xs text-subtle-foreground">No output.</p>
  return (
    <pre
      className={cn(
        "max-h-72 overflow-auto whitespace-pre-wrap break-all rounded-lg border bg-background px-3 py-2 font-mono text-xs leading-5 text-muted-foreground",
        failed && "border-danger/40 text-danger-fg",
      )}
    >
      {t}
    </pre>
  )
}

/* ---------- tool rows ---------- */

const ICON: Record<ToolKind, LucideIcon> = {
  read: FileText,
  search: Search,
  edit: FilePen,
  command: SquareTerminal,
  plan: ListTodo,
  web: Globe,
  agent: Bot,
  other: Wrench,
}

function ToolDetail({ item }: { item: ToolItem }) {
  switch (item.kind) {
    case "edit":
      return item.diff ? <DiffView text={item.diff} /> : item.result ? <Output text={item.result} failed={item.failed} /> : null
    case "command":
      return (
        <div className="space-y-1.5">
          <pre className="overflow-x-auto whitespace-pre-wrap break-all rounded-lg border bg-background px-3 py-2 font-mono text-xs leading-5">
            <span className="select-none text-subtle-foreground">$ </span>
            {item.target}
          </pre>
          {item.result !== undefined && <Output text={item.result} failed={item.failed} />}
        </div>
      )
    case "plan": {
      const todos = todosOf(item.input)
      return (
        <ul className="space-y-1 rounded-lg border bg-background px-3 py-2 text-xs">
          {todos.map((t, i) => (
            <li key={i} className={cn("flex items-start gap-2", t.status === "completed" && "text-subtle-foreground line-through")}>
              <span
                className={cn(
                  "mt-1 size-2.5 shrink-0 rounded-full border",
                  t.status === "completed" ? "border-success bg-success" : t.status === "in_progress" ? "border-info bg-info-soft" : "border-border-strong",
                )}
              />
              {t.content}
            </li>
          ))}
        </ul>
      )
    }
    default:
      return item.result !== undefined ? <Output text={item.result} failed={item.failed} /> : null
  }
}

function ToolRow({ kind, items, running }: { kind: ToolKind; items: ToolItem[]; running: boolean }) {
  const failed = items.some((x) => x.failed)
  const [open, setOpen] = useState(failed)
  const { verb, object } = stepLabel(kind, items)
  const one = items[0]
  const wrote = kind === "edit" && (one.name === "Write" || one.name === "write" || one.name === "create")
  const Icon = wrote ? FilePlus2 : ICON[kind]
  const pending = running && items.some((x) => !x.done)
  const counts = kind === "edit" && one.diff ? diffCounts(one.diff) : null
  const multi = items.length > 1
  const expandable = multi || one.diff || one.result !== undefined || kind === "command" || kind === "plan"
  return (
    <li className="relative pl-9">
      <span
        className={cn(
          "absolute left-[7px] top-1.5 z-[1] flex size-[22px] items-center justify-center rounded-full border bg-card",
          failed ? "border-danger/50 text-danger" : kind === "edit" ? "border-brand/40 text-brand" : "text-subtle-foreground",
        )}
      >
        <Icon className="size-3" />
      </span>
      <button
        onClick={() => expandable && setOpen((o) => !o)}
        aria-expanded={expandable ? open : undefined}
        className={cn(
          "group flex w-full min-w-0 items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm outline-none transition-colors focus-visible:ring-2 focus-visible:ring-ring",
          expandable && "hover:bg-accent/40",
          failed && "bg-danger-soft/40",
        )}
      >
        <span className={cn("shrink-0 font-medium", failed ? "text-danger-fg" : "text-foreground")}>{verb}</span>
        <span className={cn("min-w-0 truncate", kind === "command" || kind === "search" || kind === "read" || kind === "edit" ? "font-mono text-xs" : "", "text-muted-foreground")} title={object}>
          {object}
        </span>
        {counts && (
          <span className="shrink-0 font-mono text-2xs tabular-nums">
            <span className="text-success-fg">+{counts.added}</span> <span className="text-danger-fg">−{counts.deleted}</span>
          </span>
        )}
        {failed && <span className="shrink-0 rounded bg-danger-soft px-1.5 text-2xs font-medium text-danger-fg">{kind === "command" ? "failed" : "error"}</span>}
        {pending && <span className="busy-shimmer h-1.5 w-10 shrink-0 rounded-full bg-muted" aria-label="running" />}
        <span className="flex-1" />
        {expandable && <ChevronRight className={cn("size-3.5 shrink-0 text-subtle-foreground transition-transform", open && "rotate-90")} />}
      </button>
      {open && (
        <div className="mb-1.5 mt-1 space-y-2 pl-2 animate-in fade-in-0 duration-150">
          {multi && kind !== "edit" ? (
            <ul className="space-y-1">
              {items.map((it, i) => (
                <li key={i}>
                  <div className="truncate font-mono text-xs text-muted-foreground" title={it.target}>
                    {it.target || it.name}
                    {it.failed && <span className="ml-2 text-danger-fg">error</span>}
                  </div>
                  {it.failed && it.result && <Output text={it.result} failed />}
                </li>
              ))}
            </ul>
          ) : (
            <ToolDetail item={one} />
          )}
        </div>
      )}
    </li>
  )
}

/* ---------- the timeline ---------- */

function Avatar({ provider }: { provider: string }) {
  return <ProviderTile provider={provider} size="sm" className="absolute left-0 top-0 z-[1]" />
}

function YouAvatar() {
  return (
    <span title="You" className="absolute left-0 top-0 z-[1] flex size-6 items-center justify-center rounded-md border border-brand/40 bg-brand-soft text-brand-fg">
      <User className="size-3.5" />
    </span>
  )
}

/** The turn an event index belongs to: the last one that starts at or before it. */
function turnAt(turns: WorkTurn[] | undefined, index: number): WorkTurn | undefined {
  let found: WorkTurn | undefined
  for (const t of turns ?? []) if (t.event <= index) found = t
  return found
}

/** "after 12s · $0.04 · 70 output tokens" for a turn or a whole session. */
function costLine(ms: number, usage?: { cost_usd?: number; output_tokens?: number }): string {
  return `after ${formatElapsed(ms)}${usage?.cost_usd ? ` · ${formatCost(usage.cost_usd)}` : ""}${usage?.output_tokens ? ` · ${usage.output_tokens.toLocaleString()} output tokens` : ""}`
}

const TURN_END: Record<WorkTurn["status"], string> = {
  running: "running",
  done: "finished",
  failed: "failed",
  stopped: "stopped",
  interrupted: "interrupted",
}

/** A file the agent changed, as a small chip: its name and the lines added and removed. */
function FileChip({ path, added, deleted, onClick }: { path: string; added?: number; deleted?: number; onClick?: () => void }) {
  const name = path.split(/[\\/]/).pop() || path
  const body = (
    <>
      <FilePen className="size-3 shrink-0 text-brand" />
      <span className="max-w-[14rem] truncate font-mono">{name}</span>
      {(added !== undefined || deleted !== undefined) && (
        <span className="font-mono tabular-nums">
          <span className="text-success-fg">+{added ?? 0}</span> <span className="text-danger-fg">−{deleted ?? 0}</span>
        </span>
      )}
    </>
  )
  const cls = "inline-flex items-center gap-1.5 rounded-full border bg-card px-2 py-0.5 text-2xs text-muted-foreground"
  return onClick ? (
    <button onClick={onClick} title={`${path}: open in Changes`} className={cn(cls, "transition-colors hover:border-border-strong hover:text-foreground")} data-testid="file-chip">
      {body}
    </button>
  ) : (
    <span title={path} className={cls} data-testid="file-chip">
      {body}
    </span>
  )
}

/** The files a run of steps edited, each once, with its lines added and removed. */
function editedFiles(steps: Step[]): { path: string; added: number; deleted: number }[] {
  const byPath = new Map<string, { path: string; added: number; deleted: number }>()
  for (const s of steps) {
    if (s.type !== "tools" || s.kind !== "edit") continue
    for (const it of s.items) {
      if (!it.target) continue
      const f = byPath.get(it.target) ?? { path: it.target, added: 0, deleted: 0 }
      if (it.diff) {
        const c = diffCounts(it.diff)
        f.added += c.added
        f.deleted += c.deleted
      }
      byPath.set(it.target, f)
    }
  }
  return [...byPath.values()]
}

/** "Read 3 files, ran 2 commands and edited README.md": a run of tool steps in one line. */
function groupSummary(steps: Extract<Step, { type: "tools" }>[]): string {
  const count: Partial<Record<ToolKind, number>> = {}
  for (const s of steps) count[s.kind] = (count[s.kind] ?? 0) + s.items.length
  const n = (k: ToolKind, one: string, many: string) => {
    const c = count[k] ?? 0
    return c === 0 ? null : c === 1 ? one : `${many.replace("#", String(c))}`
  }
  const parts = [
    n("read", "read a file", "read # files"),
    n("search", "searched once", "searched # times"),
    n("command", "ran a command", "ran # commands"),
    n("edit", "edited a file", "edited # files"),
    n("plan", "updated the plan", "updated the plan # times"),
    n("web", "looked something up", "looked up # things"),
    n("agent", "asked a helper agent", "asked # helper agents"),
    n("other", "used a tool", "used # tools"),
  ].filter(Boolean) as string[]
  const text = parts.length > 1 ? `${parts.slice(0, -1).join(", ")} and ${parts[parts.length - 1]}` : (parts[0] ?? "worked")
  return text.charAt(0).toUpperCase() + text.slice(1)
}

/**
 * Several tool steps in a row, folded into one line ("Read 3 files and ran 2
 * commands") with the files it changed as chips. Open while the agent is on
 * it; once something follows, it folds so the conversation stays readable.
 */
function ToolGroup({ steps, running, live }: { steps: Extract<Step, { type: "tools" }>[]; running: boolean; live: boolean }) {
  const failed = steps.some((s) => s.items.some((x) => x.failed))
  const [open, setOpen] = useState<boolean | null>(null)
  const shown = open ?? (live || failed)
  const files = editedFiles(steps)
  const calls = steps.reduce((n, s) => n + s.items.length, 0)
  return (
    <li className="relative pl-9" data-testid="tool-group">
      <span
        className={cn(
          "absolute left-[7px] top-1.5 z-[1] flex size-[22px] items-center justify-center rounded-full border bg-card",
          failed ? "border-danger/50 text-danger" : "text-subtle-foreground",
        )}
      >
        <Wrench className="size-3" />
      </span>
      <button
        onClick={() => setOpen(!shown)}
        aria-expanded={shown}
        className="group flex w-full min-w-0 items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm outline-none transition-colors hover:bg-accent/40 focus-visible:ring-2 focus-visible:ring-ring"
      >
        <span className="min-w-0 truncate text-muted-foreground">
          <span className="font-medium text-foreground">{groupSummary(steps)}</span>
          <span className="text-subtle-foreground"> · {plural(calls, "step")}</span>
        </span>
        {failed && <span className="shrink-0 rounded bg-danger-soft px-1.5 text-2xs font-medium text-danger-fg">error</span>}
        <span className="flex-1" />
        <ChevronRight className={cn("size-3.5 shrink-0 text-subtle-foreground transition-transform", shown && "rotate-90")} />
      </button>
      {!shown && files.length > 0 && (
        <div className="flex flex-wrap gap-1.5 px-2 pb-1">
          {files.map((f) => (
            <FileChip key={f.path} {...f} />
          ))}
        </div>
      )}
      {shown && (
        <ol className="space-y-0.5 border-l border-dashed pb-1 pl-1 animate-in fade-in-0 duration-150 [&>li]:pl-7 [&>li>span:first-child]:left-0">
          {steps.map((s) => (
            <ToolRow key={s.key} kind={s.kind} items={s.items} running={running} />
          ))}
        </ol>
      )}
    </li>
  )
}

/** A step to render: one step, or a run of tool steps folded together. */
type Row = Step | { type: "group"; key: string; steps: Extract<Step, { type: "tools" }>[] }

/** Runs of three or more tool steps in a row become one foldable group. */
function grouped(steps: Step[]): Row[] {
  const out: Row[] = []
  let run: Extract<Step, { type: "tools" }>[] = []
  const flush = () => {
    if (run.length >= 3) out.push({ type: "group", key: `g-${run[0].key}`, steps: run })
    else out.push(...run)
    run = []
  }
  for (const s of steps) {
    if (s.type === "tools") run.push(s)
    else {
      flush()
      out.push(s)
    }
  }
  flush()
  return out
}

/** The brief or prompt a session started from: a council brief as a page, anything else as text. */
function StartPrompt({ session }: { session: WorkSession }) {
  const [full, setFull] = useState(false)
  const [more, setMore] = useState(false)
  // The task only: after the "# Task" heading, before Work's verify and report sections.
  const task = (session.prompt.split(/\n# Task[^\n]*\n/)[1] ?? session.prompt).split(/\n## Verify\n/)[0]
  const parsed = useMemo(() => parseBrief(task), [task])
  const structured = parsed.sections.some((s) => s.heading !== "")
  const long = task.length > 700
  return (
    <div className="rounded-xl border bg-brand-soft/40 px-4 py-3" data-testid="start-prompt">
      <div className="mb-1.5 flex items-center gap-2 text-2xs font-medium uppercase tracking-[0.08em] text-subtle-foreground">
        {session.card ? "Card brief" : "Prompt"}
        {session.brief && <span className="truncate font-mono normal-case tracking-normal">{session.brief}</span>}
      </div>
      {full ? (
        <Prose text={session.prompt} />
      ) : (
        <div className={cn("relative", long && !more && "max-h-64 overflow-hidden")}>
          {structured ? (
            <div data-testid="brief-rendered">
              {parsed.title && <p className="mb-2 text-sm font-semibold">{parsed.title}</p>}
              <BriefBody b={parsed} compact />
            </div>
          ) : (
            <Prose text={task} />
          )}
          {long && !more && <div aria-hidden className="pointer-events-none absolute inset-x-0 bottom-0 h-16 bg-gradient-to-t from-background/90 to-transparent" />}
        </div>
      )}
      <div className="mt-2 flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground">
        {long && !full && (
          <button onClick={() => setMore((m) => !m)} className="hover:text-foreground">
            {more ? "Show less" : "Show the whole brief"}
          </button>
        )}
        <button onClick={() => setFull((s) => !s)} className="hover:text-foreground">
          {full ? "Show the brief only" : "Show the full prompt, with the worktree rules"}
        </button>
      </div>
    </div>
  )
}

/**
 * The run as a conversation: your prompt, the agent's messages, and between
 * them compact rows for what it did. Runs of tool calls fold into one line,
 * and each turn's separator sticks under the header while you read it.
 */
export function Timeline({ session, steps, now, onOpenChanges }: { session: WorkSession; steps: Step[]; now: number; onOpenChanges?: () => void }) {
  const running = session.status === "running"
  const waiting = session.status === "waiting"
  const name = providerInfo(session.provider)?.label ?? session.provider
  const turns = session.turns ?? []
  const multi = turns.length > 1
  const current = turns[turns.length - 1]
  const rows = useMemo(() => grouped(steps), [steps])
  const lastRow = rows[rows.length - 1]
  const files = session.diff?.files ?? []
  return (
    <ol className="relative space-y-1.5">
      <span aria-hidden className="absolute bottom-3 left-[17px] top-3 w-px bg-border" />

      <li className="relative pb-2 pl-9">
        <YouAvatar />
        <StartPrompt session={session} />
      </li>

      {rows.map((s, i) => {
        const prev = rows[i - 1]
        switch (s.type) {
          case "group":
            return <ToolGroup key={s.key} steps={s.steps} running={running} live={running && s === lastRow} />
          case "message": {
            // A name over the agent's first message after you spoke, as a chat does.
            const first = !prev || prev.type === "turn"
            return (
              <li key={s.key} className="relative py-2 pl-9">
                <Avatar provider={session.provider} />
                {first && <div className="mb-0.5 text-xs font-medium text-muted-foreground">{name}</div>}
                <div className={cn(!first && "pt-0.5")}>
                  <Prose text={s.text} className="text-[0.9375rem] leading-7" />
                </div>
              </li>
            )
          }
          case "tools":
            return <ToolRow key={s.key} kind={s.kind} items={s.items} running={running} />
          case "error":
            return (
              <li key={s.key} className="relative py-1.5 pl-9">
                <span className="absolute left-[7px] top-2.5 z-[1] flex size-[22px] items-center justify-center rounded-full border border-danger/50 bg-card text-danger">
                  {s.title === "warning" ? <TriangleAlert className="size-3" /> : <OctagonX className="size-3" />}
                </span>
                <div
                  role="alert"
                  className={cn(
                    "rounded-lg border px-3 py-2 text-sm",
                    s.title === "warning" ? "border-warning/40 bg-warning-soft text-warning-fg" : "border-danger/40 bg-danger-soft text-danger-fg",
                  )}
                >
                  <div className="font-medium">{s.title === "warning" ? `${name} warned` : s.title ? `Error: ${s.title}` : `${name} reported an error`}</div>
                  <div className="mt-0.5 whitespace-pre-wrap break-words font-mono text-xs opacity-90">{s.text}</div>
                </div>
              </li>
            )
          case "done": {
            // With one turn the footer says it all; with several, each turn ends with its own line.
            const t = multi ? turnAt(turns, s.index) : undefined
            if (!t || t.status === "running") return null
            return (
              <li key={s.key} className="relative py-1 pl-9" data-testid="turn-end">
                <span
                  className={cn(
                    "absolute left-[7px] top-1.5 z-[1] flex size-[22px] items-center justify-center rounded-full border bg-card",
                    t.status === "done" ? "border-success/50 text-success" : t.status === "failed" ? "border-danger/50 text-danger" : "text-subtle-foreground",
                  )}
                >
                  {t.status === "done" ? <CircleCheck className="size-3" /> : <CircleX className="size-3" />}
                </span>
                <div className="pt-1 text-xs text-muted-foreground">
                  <span className="font-medium text-foreground">
                    Turn {t.n} {TURN_END[t.status]}
                  </span>{" "}
                  {costLine(turnElapsed(t, now), t.usage)}
                </div>
              </li>
            )
          }
          case "turn": {
            const t = turns.find((x) => x.n === s.n)
            return [
              <li
                key={`${s.key}-sep`}
                className="sticky z-[5] -mx-1 bg-background/90 px-1 pb-2 pt-3 backdrop-blur-sm"
                style={{ top: "var(--session-head, 0px)" }}
                data-testid="turn-separator"
              >
                <div className="flex items-center gap-2 text-2xs font-medium uppercase tracking-[0.08em] text-subtle-foreground">
                  <span className="h-px flex-1 bg-border" aria-hidden />
                  <span className="text-foreground">Turn {s.n}</span>
                  <span className="rounded-full border bg-card px-2 py-0.5 normal-case tracking-normal" title={s.title}>
                    {t ? turnModeLabel(t.mode, session.provider) : s.title.replace(/^Turn \d+ · /, "")}
                  </span>
                  <span className="h-px flex-1 bg-border" aria-hidden />
                </div>
              </li>,
              <li key={s.key} className="relative pb-2 pl-9">
                <YouAvatar />
                <div className="rounded-xl border bg-brand-soft/40 px-4 py-3">
                  <div className="mb-1 text-2xs font-medium uppercase tracking-[0.08em] text-subtle-foreground">Follow-up</div>
                  <Prose text={s.prompt} className="text-[0.9375rem] leading-7" />
                </div>
              </li>,
            ]
          }
          case "note":
            return (
              <li key={s.key} className="relative py-1.5 pl-9">
                <span className="absolute left-[7px] top-2.5 z-[1] flex size-[22px] items-center justify-center rounded-full border border-warning/50 bg-card text-warning">
                  <Info className="size-3" />
                </span>
                <div className="rounded-lg border border-warning/30 bg-warning-soft/50 px-3 py-2 text-sm text-warning-fg">{s.text}</div>
              </li>
            )
          case "image":
            return (
              <li key={s.key} className="relative py-1.5 pl-9">
                <span className="absolute left-[7px] top-2.5 z-[1] flex size-[22px] items-center justify-center rounded-full border bg-card text-subtle-foreground">
                  <Camera className="size-3" />
                </span>
                <div className="pt-0.5 text-xs text-muted-foreground">{s.title}</div>
                <a href={s.src} target="_blank" rel="noreferrer" className="mt-1.5 block max-w-xl overflow-hidden rounded-lg border">
                  <img src={s.src} alt={s.title} loading="lazy" className="block w-full" />
                </a>
              </li>
            )
        }
      })}

      {running ? (
        <li className="relative py-2 pl-9">
          <Avatar provider={session.provider} />
          <div className="flex items-center gap-2 pt-0.5 text-sm text-muted-foreground">
            <span className="flex gap-1" aria-hidden>
              {[0, 1, 2].map((i) => (
                <span key={i} className="size-1.5 animate-pulse rounded-full bg-brand" style={{ animationDelay: `${i * 180}ms` }} />
              ))}
            </span>
            {name} is working{multi && current ? ` on turn ${current.n}` : ""} · {formatElapsed(current ? turnElapsed(current, now) : now - Date.parse(session.started))}
          </div>
        </li>
      ) : waiting ? (
        <li className="relative py-2 pl-9" data-testid="waiting-footer">
          <span className="absolute left-[7px] top-2.5 z-[1] flex size-[22px] items-center justify-center rounded-full border border-warning/50 bg-card text-warning">
            <Hourglass className="size-3" />
          </span>
          <div className="pt-1 text-sm">
            <span className="font-medium">Waiting for you</span>
            <span className="text-muted-foreground">
              {" "}
              · {turns.length === 1 ? "1 turn" : `${turns.length} turns`}, {costLine(elapsedOf(session, now), session.usage).replace(/^after /, "")}
              {multi ? " in total" : ""}
            </span>
          </div>
        </li>
      ) : (
        <li className="relative py-2 pl-9" data-testid="ended-footer">
          <span
            className={cn(
              "absolute left-[7px] top-2.5 z-[1] flex size-[22px] items-center justify-center rounded-full border bg-card",
              session.status === "done" || session.status === "merged"
                ? "border-success/50 text-success"
                : session.status === "failed"
                  ? "border-danger/50 text-danger"
                  : "text-subtle-foreground",
            )}
          >
            {session.status === "merged" ? (
              <GitMerge className="size-3.5" />
            ) : session.status === "done" ? (
              <CircleCheck className="size-3.5" />
            ) : (
              <CircleX className="size-3.5" />
            )}
          </span>
          <div className="pt-1 text-sm">
            <span className="font-medium">{ENDED[session.status] ?? "Stopped"}</span>
            <span className="text-muted-foreground">
              {" "}
              {costLine(elapsedOf(session, now), session.usage)}
              {multi ? ` over ${turns.length} turns` : ""}
            </span>
            {session.status === "failed" && session.error && <div className="mt-1 break-words text-xs text-danger-fg">{session.error}</div>}
          </div>
        </li>
      )}
      {!running && files.length > 0 && (
        <li className="relative -mt-1 pb-1 pl-9">
          <div className="flex flex-wrap items-center gap-1.5" aria-label="Files changed">
            {files.slice(0, 12).map((f) => (
              <FileChip key={f.path} path={f.path} added={f.added} deleted={f.deleted} onClick={onOpenChanges} />
            ))}
            {files.length > 12 && <span className="text-2xs text-subtle-foreground">and {files.length - 12} more</span>}
          </div>
        </li>
      )}
    </ol>
  )
}

/** How an ended session's last line reads. */
const ENDED: Partial<Record<WorkSession["status"], string>> = {
  done: "Finished",
  failed: "Failed",
  stopped: "Stopped",
  merged: "Merged",
  closed: "PR closed",
}
