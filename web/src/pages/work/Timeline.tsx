import { useState, type ReactNode } from "react"
import {
  Bot,
  ChevronRight,
  CircleCheck,
  CircleX,
  FilePen,
  FilePlus2,
  FileText,
  Globe,
  ListTodo,
  OctagonX,
  Search,
  SquareTerminal,
  TriangleAlert,
  User,
  Wrench,
  type LucideIcon,
} from "lucide-react"

import { ProviderTile, providerInfo } from "@/components/ProviderMark"
import { cn } from "@/lib/utils"
import {
  diffCounts,
  formatCost,
  formatElapsed,
  stepLabel,
  todosOf,
  type Step,
  type ToolItem,
  type ToolKind,
  type WorkSession,
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

/**
 * The run as a conversation: your prompt, the agent's messages, and between
 * them compact rows for what it did.
 */
export function Timeline({ session, steps, now }: { session: WorkSession; steps: Step[]; now: number }) {
  const running = session.status === "running"
  const [showPrompt, setShowPrompt] = useState(false)
  const name = providerInfo(session.provider)?.label ?? session.provider
  // The task only: after the "# Task" heading, before Work's verify and report sections.
  const brief = (session.prompt.split(/\n# Task[^\n]*\n/)[1] ?? session.prompt).split(/\n## Verify\n/)[0]
  return (
    <ol className="relative space-y-1">
      <span aria-hidden className="absolute bottom-3 left-[17px] top-3 w-px bg-border" />

      <li className="relative pb-3 pl-9">
        <span title="You" className="absolute left-0 top-0 z-[1] flex size-6 items-center justify-center rounded-md border border-brand/40 bg-brand-soft text-brand-fg">
          <User className="size-3.5" />
        </span>
        <div className="rounded-xl border bg-brand-soft/40 px-4 py-3">
          <div className="mb-1 flex items-center gap-2 text-2xs font-medium uppercase tracking-[0.08em] text-subtle-foreground">
            {session.card ? "Card brief" : "Prompt"}
            {session.brief && <span className="normal-case tracking-normal font-mono">{session.brief}</span>}
          </div>
          <Prose text={showPrompt ? session.prompt : brief} className={cn(!showPrompt && "line-clamp-[12]")} />
          <button onClick={() => setShowPrompt((s) => !s)} className="mt-2 text-xs text-muted-foreground hover:text-foreground">
            {showPrompt ? "Show the brief only" : "Show the full prompt, with the worktree rules"}
          </button>
        </div>
      </li>

      {steps.map((s) => {
        switch (s.type) {
          case "message":
            return (
              <li key={s.key} className="relative py-1.5 pl-9">
                <Avatar provider={session.provider} />
                <div className="pt-0.5">
                  <Prose text={s.text} />
                </div>
              </li>
            )
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
          case "done":
            return null
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
            {name} is working · {formatElapsed(now - Date.parse(session.started))}
          </div>
        </li>
      ) : (
        <li className="relative py-2 pl-9">
          <span
            className={cn(
              "absolute left-[7px] top-2.5 z-[1] flex size-[22px] items-center justify-center rounded-full border bg-card",
              session.status === "done" ? "border-success/50 text-success" : session.status === "failed" ? "border-danger/50 text-danger" : "text-subtle-foreground",
            )}
          >
            {session.status === "done" ? <CircleCheck className="size-3.5" /> : <CircleX className="size-3.5" />}
          </span>
          <div className="pt-1 text-sm">
            <span className="font-medium">
              {session.status === "done" ? "Finished" : session.status === "failed" ? "Failed" : "Stopped"}
            </span>
            <span className="text-muted-foreground">
              {" "}
              after {formatElapsed((session.ended ? Date.parse(session.ended) : now) - Date.parse(session.started))}
              {session.usage?.cost_usd ? ` · ${formatCost(session.usage.cost_usd)}` : ""}
              {session.usage?.output_tokens ? ` · ${session.usage.output_tokens.toLocaleString()} output tokens` : ""}
            </span>
            {session.status === "failed" && session.error && <div className="mt-1 break-words text-xs text-danger-fg">{session.error}</div>}
          </div>
        </li>
      )}
    </ol>
  )
}
