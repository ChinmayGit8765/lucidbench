import { useState, type ReactNode } from "react"
import {
  ArrowLeft,
  ArrowRight,
  Check,
  CheckCircle2,
  ChevronDown,
  ExternalLink,
  FileText,
  GitMerge,
  GitPullRequest,
  KanbanSquare,
  Lightbulb,
  Link2,
  PenLine,
  Play,
  SquareTerminal,
  TriangleAlert,
  Vote,
  type LucideIcon,
} from "lucide-react"

import { ProviderMark, ProviderTile, providerInfo, tintVar } from "@/components/ProviderMark"
import { PageHeader, RefreshButton } from "@/components/Shell"
import { Badge, StatusPill } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card } from "@/components/ui/card"
import { ErrorState, Skeleton } from "@/components/ui/states"
import { usePoll } from "@/lib/api"
import { useApp } from "@/lib/app"
import { STATUS as COUNCIL_STATUS, tokens, VERDICT } from "@/lib/council"
import { ideaPath, IDEAS_POLL_MS, STAGE_INFO, STAGES, stageIndex, type Idea, type IdeaEvent, type IdeaSession, type Stage } from "@/lib/ideas"
import { baseName, pageRoute } from "@/lib/memory"
import { absoluteTime, relativeTime, useNow } from "@/lib/time"
import { cn, plural } from "@/lib/utils"
import { checksLabel, formatCost, PR_INFO, STATUS_INFO } from "@/lib/work"
import { BriefBody, parseBrief } from "@/pages/Council"

const STAGE_ICON: Record<Stage, LucideIcon> = {
  braindump: PenLine,
  brief: FileText,
  card: KanbanSquare,
  work: SquareTerminal,
  pr: GitPullRequest,
  merged: GitMerge,
}

export function StageIcon({ stage, className }: { stage: Stage; className?: string }) {
  const I = STAGE_ICON[stage]
  return <I className={className} />
}

const prNumber = (url: string) => `#${url.split("/").pop()}`

/** A link inside the app ("council/<id>", "memory/<path>") or out to GitHub. */
function useGo() {
  const { open, navigate } = useApp()
  return (link?: string) => {
    if (!link) return
    if (/^https?:\/\//.test(link)) {
      window.open(link, "_blank", "noreferrer")
      return
    }
    const [mod, ...rest] = link.split("/")
    if (mod === "memory") navigate(pageRoute(rest.join("/")))
    else open(mod, rest)
  }
}

export function IdeaView({ id }: { id: string }) {
  const { open } = useApp()
  const poll = usePoll<Idea>(ideaPath(id), IDEAS_POLL_MS)
  const idea = poll.data
  const now = useNow(30000)

  const back = (
    <button onClick={() => open("ideas")} className="inline-flex items-center gap-1 hover:text-foreground">
      <ArrowLeft className="size-3" /> Ideas
    </button>
  )
  if (poll.error && !idea)
    return (
      <div className="space-y-6">
        <PageHeader icon={<Lightbulb />} eyebrow={back} title="Idea" />
        <ErrorState title={poll.error.status === 404 ? "No such idea" : "Could not load the idea"} message={poll.error.message} onRetry={poll.refresh} />
      </div>
    )
  if (!idea)
    return (
      <div className="space-y-4">
        <Skeleton className="h-16 w-2/3 rounded-xl" />
        <Skeleton className="h-28 rounded-xl" />
        <div className="grid gap-4 @4xl:grid-cols-[minmax(0,1fr)_20rem]">
          <Skeleton className="h-96 rounded-xl" />
          <Skeleton className="h-96 rounded-xl" />
        </div>
      </div>
    )

  return (
    <div className="space-y-5">
      <PageHeader
        icon={<Lightbulb />}
        eyebrow={back}
        title={idea.title || "Untitled idea"}
        description={
          <span className="inline-flex flex-wrap items-center gap-x-2 gap-y-1">
            <span>{idea.status}</span>
            {idea.project && (
              <>
                <span aria-hidden>·</span>
                <span className="font-mono text-xs">{idea.project}</span>
              </>
            )}
            {idea.created && (
              <>
                <span aria-hidden>·</span>
                <span title={absoluteTime(idea.created)}>started {relativeTime(idea.created, now)}</span>
              </>
            )}
            <span aria-hidden>·</span>
            <span className="font-mono text-xs">{formatCost(idea.cost_usd)}</span>
          </span>
        }
        actions={<RefreshButton refreshing={poll.refreshing} updatedAt={poll.updatedAt} />}
      />

      <Hero idea={idea} />

      {idea.missing.length > 0 && (
        <div role="note" className="flex items-start gap-2.5 rounded-lg border border-warning/35 bg-warning-soft px-3.5 py-2.5 text-sm">
          <TriangleAlert className="mt-0.5 size-4 shrink-0 text-warning-fg" />
          <div className="min-w-0">
            <p className="font-medium text-warning-fg">Some pieces could not be read; the rest is shown.</p>
            <ul className="mt-0.5 list-disc pl-4 text-xs text-muted-foreground">
              {idea.missing.map((m) => (
                <li key={m}>{m}</li>
              ))}
            </ul>
          </div>
        </div>
      )}

      <div className="grid items-start gap-4 @4xl:grid-cols-[minmax(0,1fr)_21rem]">
        <div className="min-w-0 space-y-4">
          <Structure idea={idea} />
          <Timeline events={idea.timeline} now={now} />
          {idea.brief?.body && <BriefPanel idea={idea} />}
        </div>
        <aside className="space-y-4 @4xl:sticky @4xl:top-4">
          <Costs idea={idea} />
          {idea.card_view && <CardHistory idea={idea} now={now} />}
          <Links idea={idea} />
        </aside>
      </div>
    </div>
  )
}

/* ---------- hero: the stage stepper and the next step ---------- */

function Hero({ idea }: { idea: Idea }) {
  const at = stageIndex(idea.stage)
  return (
    <Card className="relative overflow-hidden px-5 py-4">
      <div aria-hidden className="pointer-events-none absolute inset-x-0 top-0 h-20 bg-gradient-to-b from-brand-soft to-transparent opacity-70" />
      <div className="relative flex flex-wrap items-center justify-between gap-4">
        <ol aria-label="Stage" className="flex flex-wrap items-center gap-1.5">
          {STAGES.map((s, i) => {
            const state = i < at || (i === at && s === "merged") ? "done" : i === at ? "now" : "todo"
            const I = STAGE_ICON[s]
            return (
              <li key={s} className="flex items-center gap-1.5" aria-current={i === at ? "step" : undefined}>
                <span
                  title={STAGE_INFO[s].blurb}
                  className={cn(
                    "flex h-7 items-center gap-1.5 rounded-full border px-2.5 text-xs transition-colors",
                    state === "done" && "border-success/30 bg-success-soft text-success-fg",
                    state === "now" && "border-brand/40 bg-brand-soft font-medium text-brand-fg",
                    state === "todo" && "bg-background/50 text-subtle-foreground",
                  )}
                >
                  {state === "done" ? <Check className="size-3.5" /> : <I className="size-3.5" />}
                  {STAGE_INFO[s].label}
                </span>
                {i < STAGES.length - 1 && <span className={cn("h-px w-4", i < at ? "bg-success/40" : "bg-border")} />}
              </li>
            )
          })}
        </ol>
        <NextSteps idea={idea} />
      </div>
    </Card>
  )
}

/** The next move, through the page that already does it: nothing here acts by itself. */
function NextSteps({ idea }: { idea: Idea }) {
  const { open } = useApp()
  const last: IdeaSession | undefined = idea.work[idea.work.length - 1]
  const cv = idea.card_view
  const s = idea.council_session
  const buttons: ReactNode[] = []
  if (s && (s.status === "draft" || s.status === "running" || s.status === "failed") && idea.stage === "brief") {
    buttons.push(
      <Button key="approve" size="sm" onClick={() => open("council", [s.id])}>
        <CheckCircle2 /> Review and approve
      </Button>,
    )
  }
  if (s && idea.stage === "braindump") {
    buttons.push(
      <Button key="council" size="sm" variant="secondary" onClick={() => open("council", [s.id])}>
        <Vote /> Open the council
      </Button>,
    )
  }
  if (cv && idea.stage === "card") {
    buttons.push(
      <Button
        key="start"
        size="sm"
        onClick={() => open(cv.board === "work" ? "work" : "boards", cv.board === "work" ? ["new", cv.id] : [cv.board, cv.id])}
        title={cv.board === "work" ? "Pick an agent and start it on this card" : "Work starts from cards on the work board"}
      >
        <Play /> Start work
      </Button>,
    )
  }
  if (last && idea.stage === "work") {
    buttons.push(
      <Button key="pr" size="sm" onClick={() => open("work", [last.id])}>
        {last.status === "running" ? <SquareTerminal /> : <GitPullRequest />}
        {last.status === "running"
          ? "Watch the agent"
          : last.status === "waiting"
            ? "Reply to the agent"
            : last.status === "done"
              ? "Review the diff and open a PR"
              : "Open the session"}
      </Button>,
    )
  }
  if (idea.pr_url) {
    buttons.push(
      <Button key="gh" size="sm" variant={idea.stage === "merged" ? "default" : "secondary"} asChild>
        <a href={idea.pr_url} target="_blank" rel="noreferrer">
          <ExternalLink /> View on GitHub
        </a>
      </Button>,
    )
  }
  if (last && (idea.stage === "pr" || idea.stage === "merged")) {
    buttons.push(
      <Button key="session" size="sm" variant="ghost" onClick={() => open("work", [last.id])}>
        Session <ArrowRight />
      </Button>,
    )
  }
  if (buttons.length === 0) return null
  return <div className="flex flex-wrap items-center gap-2">{buttons}</div>
}

/* ---------- the structure: council → brief → card → sessions → PRs ---------- */

function Node({
  icon: Icon,
  tint,
  title,
  meta,
  pill,
  onClick,
  href,
  missing,
  children,
}: {
  icon: LucideIcon
  tint?: string
  title: ReactNode
  meta?: ReactNode
  pill?: ReactNode
  onClick?: () => void
  href?: string
  missing?: boolean
  children?: ReactNode
}) {
  const inner = (
    <>
      <span
        className={cn("flex size-7 shrink-0 items-center justify-center rounded-md border bg-elevated", missing ? "border-dashed text-subtle-foreground" : "text-brand")}
        style={tint ? { color: tint } : undefined}
      >
        <Icon className="size-3.5" />
      </span>
      <span className="min-w-0 flex-1">
        <span className={cn("flex items-center gap-2 text-sm", missing ? "text-muted-foreground" : "font-medium")}>
          <span className="truncate">{title}</span>
          {pill}
        </span>
        {meta && <span className="block truncate text-xs text-muted-foreground">{meta}</span>}
      </span>
      {(onClick || href) && <ArrowRight className="size-3.5 shrink-0 text-subtle-foreground opacity-0 transition-opacity group-hover:opacity-100" />}
    </>
  )
  const cls = cn(
    "group flex w-full items-center gap-3 rounded-lg px-2.5 py-2 text-left outline-none transition-colors",
    (onClick || href) && "hover:bg-accent/50 focus-visible:bg-accent/50",
    missing && "border border-dashed",
  )
  return (
    <li className="relative">
      {href ? (
        <a href={href} target="_blank" rel="noreferrer" className={cls}>
          {inner}
        </a>
      ) : onClick ? (
        <button type="button" onClick={onClick} className={cls}>
          {inner}
        </button>
      ) : (
        <div className={cls}>{inner}</div>
      )}
      {children && <ul className="ml-[1.45rem] space-y-0.5 border-l pl-3">{children}</ul>}
    </li>
  )
}

function Structure({ idea }: { idea: Idea }) {
  const { open, navigate } = useApp()
  const s = idea.council_session
  const b = idea.brief
  const cv = idea.card_view
  const lastVerdicts = s?.rounds[s.rounds.length - 1]?.critiques.filter((c) => c.verdict) ?? []

  const sessions = idea.work.map((w) => (
    <Node
      key={w.id}
      icon={SquareTerminal}
      tint={tintVar(w.provider)}
      title={`${providerInfo(w.provider)?.label ?? w.provider} session`}
      pill={<StatusPill tone={STATUS_INFO[w.status].tone}>{STATUS_INFO[w.status].label}</StatusPill>}
      meta={
        <>
          <span className="font-mono">{w.branch}</span>
          {(w.added > 0 || w.deleted > 0) && (
            <span className="ml-2 font-mono">
              <span className="text-success-fg">+{w.added}</span> <span className="text-danger-fg">−{w.deleted}</span>
            </span>
          )}
          {w.commits.length > 0 && ` · ${plural(w.commits.length, "commit")}`}
          {w.cost_usd > 0 && ` · ${formatCost(w.cost_usd)}`}
        </>
      }
      onClick={() => open("work", [w.id])}
    >
      {w.pr_url ? (
        <Node
          icon={w.pr_state === "merged" ? GitMerge : GitPullRequest}
          title={`Pull request ${prNumber(w.pr_url)}`}
          pill={w.pr_state && <StatusPill tone={PR_INFO[w.pr_state].tone}>{PR_INFO[w.pr_state].label}</StatusPill>}
          meta={
            <>
              {w.pr_url.replace(/^https?:\/\/(www\.)?github\.com\//, "")}
              {w.pr_checks && w.pr_state !== "merged" && ` · checks: ${checksLabel(w.pr_checks)}`}
            </>
          }
          href={w.pr_url}
        />
      ) : (
        <Node icon={GitPullRequest} missing title="No pull request yet" meta={w.status === "done" ? "Open one from the session once the diff looks right" : undefined} />
      )}
    </Node>
  ))

  const card = cv ? (
    <Node
      icon={KanbanSquare}
      title={cv.title}
      pill={<Badge>{cv.column}</Badge>}
      meta={`${cv.board_title || cv.board} board${cv.project ? ` · ${cv.project}` : ""}`}
      onClick={() => open("boards", [cv.board, cv.id])}
    >
      {sessions.length > 0 ? sessions : <Node icon={SquareTerminal} missing title="No agent session yet" meta="Start work from the card or the button above" />}
    </Node>
  ) : (
    <Node icon={KanbanSquare} missing title={idea.stage === "brief" || idea.stage === "braindump" ? "No card yet" : "The card is gone"} meta={idea.stage === "brief" ? "Approving the brief adds one to Ready" : undefined} />
  )

  const brief = b ? (
    <Node
      icon={FileText}
      title={b.title || baseName(b.path)}
      pill={b.status && <StatusPill tone={b.status === "approved" ? "success" : "warning"}>{b.status}</StatusPill>}
      meta={<span className="font-mono">{b.path}</span>}
      onClick={b.exists ? () => navigate(pageRoute(b.path)) : undefined}
      missing={!b.exists}
    >
      {card}
    </Node>
  ) : null

  return (
    <Card className="p-4">
      <h2 className="mb-2 flex items-baseline justify-between text-xs font-semibold uppercase tracking-wider text-subtle-foreground">
        Structure
        <span className="text-2xs font-normal normal-case tracking-normal">each step opens its page</span>
      </h2>
      <ul className="space-y-0.5">
        {s ? (
          <Node
            icon={Vote}
            title="Council"
            pill={<StatusPill tone={COUNCIL_STATUS[s.status].tone}>{COUNCIL_STATUS[s.status].label}</StatusPill>}
            meta={
              <span className="inline-flex items-center gap-1.5">
                {plural(s.rounds.length, "round")}
                {lastVerdicts.map((c, i) => (
                  <span key={i} className="inline-flex items-center gap-1" title={`${providerInfo(c.provider)?.label ?? c.provider}: ${c.verdict}`}>
                    <ProviderMark provider={c.provider} className="size-3" />
                    <span className={cn(c.verdict === "blocker" ? "text-danger-fg" : c.verdict === "concerns" ? "text-warning-fg" : "text-success-fg")}>{VERDICT[c.verdict!].label}</span>
                  </span>
                ))}
              </span>
            }
            onClick={() => open("council", [s.id])}
          >
            {brief ?? <Node icon={FileText} missing title="No brief yet" meta={s.status === "running" ? "The council is still working" : undefined} />}
          </Node>
        ) : brief ? (
          brief
        ) : (
          card
        )}
      </ul>
      {!s && <p className="mt-2 text-2xs text-subtle-foreground">This card was made by hand: there is no council session behind it.</p>}
    </Card>
  )
}

/* ---------- timeline ---------- */

const KIND_DOT: Record<IdeaEvent["kind"], string> = {
  council: "bg-brand",
  critique: "bg-brand/60",
  brief: "bg-warning",
  approve: "bg-success",
  card: "bg-info",
  work: "bg-info",
  pr: "bg-success",
  merged: "bg-success",
  checks: "bg-border-strong",
}

function Timeline({ events, now }: { events: IdeaEvent[]; now: number }) {
  const go = useGo()
  const [all, setAll] = useState(false)
  const shown = all || events.length <= 12 ? events : events.slice(-12)
  return (
    <Card className="p-4">
      <h2 className="mb-3 flex items-baseline justify-between text-xs font-semibold uppercase tracking-wider text-subtle-foreground">
        Timeline
        <span className="text-2xs font-normal normal-case tracking-normal">{plural(events.length, "event")}, oldest first</span>
      </h2>
      {events.length > shown.length && (
        <button onClick={() => setAll(true)} className="mb-2 inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground">
          <ChevronDown className="size-3" /> Show {events.length - shown.length} earlier
        </button>
      )}
      <ol className="relative space-y-0.5">
        <span aria-hidden className="absolute bottom-2 left-[5px] top-2 w-px bg-border" />
        {shown.map((e, i) => (
          <li key={`${e.time}-${i}`} className="relative pl-6">
            <span className={cn("absolute left-0 top-2.5 size-[11px] rounded-full border-2 border-card", KIND_DOT[e.kind])} />
            <button
              type="button"
              onClick={() => go(e.link)}
              disabled={!e.link}
              className="flex w-full items-start gap-3 rounded-md px-2 py-1.5 text-left transition-colors enabled:hover:bg-accent/40"
            >
              <span className="min-w-0 flex-1">
                <span className="flex items-center gap-1.5 text-sm">
                  {e.provider && (
                    <span style={{ color: tintVar(e.provider) }}>
                      <ProviderMark provider={e.provider} className="size-3.5" />
                    </span>
                  )}
                  <span className="truncate">{e.title}</span>
                  {e.provider && <span className="text-xs text-subtle-foreground">{providerInfo(e.provider)?.label ?? e.provider}</span>}
                </span>
                {e.detail && <span className="block truncate font-mono text-2xs text-muted-foreground">{e.detail}</span>}
              </span>
              <time
                className="shrink-0 pt-0.5 text-2xs tabular-nums text-subtle-foreground"
                title={e.untimed ? "Lucidbench does not record when this happened; it came after the step above" : absoluteTime(e.time)}
              >
                {e.untimed ? "time not recorded" : relativeTime(e.time, now)}
              </time>
            </button>
          </li>
        ))}
      </ol>
    </Card>
  )
}

/* ---------- aside ---------- */

function Costs({ idea }: { idea: Idea }) {
  const max = Math.max(0.0001, ...idea.costs.map((c) => c.cost_usd))
  const total = idea.costs.reduce((n, c) => n + c.cost_usd, 0)
  return (
    <Card className="p-4">
      <h2 className="mb-3 flex items-baseline justify-between text-xs font-semibold uppercase tracking-wider text-subtle-foreground">
        Cost by provider
        <span className="font-mono text-sm normal-case tracking-normal text-foreground">{formatCost(total)}</span>
      </h2>
      {idea.costs.length === 0 ? (
        <p className="text-xs text-muted-foreground">No model call has reported usage yet.</p>
      ) : (
        <ul className="space-y-2.5">
          {idea.costs.map((c) => (
            <li key={`${c.provider}-${c.part}`}>
              <div className="flex items-center gap-2 text-xs">
                <ProviderTile provider={c.provider} size="sm" />
                <span className="min-w-0 flex-1">
                  <span className="font-medium">{providerInfo(c.provider)?.label ?? c.provider}</span>
                  <span className="text-subtle-foreground">
                    {" "}
                    · {c.part} × {c.calls}
                  </span>
                </span>
                <span className="font-mono tabular-nums">{c.cost_usd ? formatCost(c.cost_usd) : "n/a"}</span>
              </div>
              <div className="mt-1 flex items-center gap-2 pl-8">
                <span className="h-1 flex-1 overflow-hidden rounded-full bg-muted">
                  <span className="block h-full rounded-full" style={{ width: `${Math.max(2, (c.cost_usd / max) * 100)}%`, background: tintVar(c.provider) }} />
                </span>
                <span className="w-20 text-right font-mono text-2xs tabular-nums text-subtle-foreground" title="input / output tokens">
                  {c.input_tokens + c.output_tokens > 0 ? `${tokens(c.input_tokens)}/${tokens(c.output_tokens)}` : "-"}
                </span>
              </div>
            </li>
          ))}
        </ul>
      )}
      <p className="mt-3 text-2xs text-subtle-foreground">As each CLI reported it. A provider that reports no cost shows n/a.</p>
    </Card>
  )
}

function CardHistory({ idea, now }: { idea: Idea; now: number }) {
  const cv = idea.card_view!
  return (
    <Card className="p-4">
      <h2 className="mb-2.5 flex items-baseline justify-between text-xs font-semibold uppercase tracking-wider text-subtle-foreground">
        Card
        <Badge>now in {cv.column}</Badge>
      </h2>
      {cv.history.length > 0 && (
        <ol className="mb-2.5 space-y-1.5">
          {cv.history.map((m, i) => (
            <li key={i} className="flex items-center gap-2 text-xs">
              <span className={cn("size-1.5 shrink-0 rounded-full", i === cv.history.length - 1 ? "bg-brand" : "bg-border-strong")} />
              <span className="font-medium">{m.column}</span>
              <span className="min-w-0 flex-1 truncate text-subtle-foreground">{m.by}</span>
              <time className="shrink-0 tabular-nums text-subtle-foreground" title={absoluteTime(m.time)}>
                {relativeTime(m.time, now)}
              </time>
            </li>
          ))}
        </ol>
      )}
      <p className="text-2xs leading-4 text-subtle-foreground">{cv.history_note}</p>
    </Card>
  )
}

function Links({ idea }: { idea: Idea }) {
  const { navigate } = useApp()
  if (!idea.brief) return null
  return (
    <Card className="p-4">
      <h2 className="mb-2.5 text-xs font-semibold uppercase tracking-wider text-subtle-foreground">Pages that link the brief</h2>
      {idea.links.length === 0 ? (
        <p className="text-xs text-muted-foreground">
          None yet. Link it with <span className="font-mono">[[{baseName(idea.brief.path)}]]</span> from any page.
        </p>
      ) : (
        <ul className="space-y-1">
          {idea.links.map((l) => (
            <li key={l}>
              <button onClick={() => navigate(pageRoute(l))} className="flex w-full items-center gap-2 rounded px-1 py-1 text-left text-xs hover:bg-accent/50">
                <Link2 className="size-3 shrink-0 text-subtle-foreground" />
                <span className="truncate">{baseName(l)}</span>
                <span className="ml-auto truncate font-mono text-2xs text-subtle-foreground">{l}</span>
              </button>
            </li>
          ))}
        </ul>
      )}
    </Card>
  )
}

function BriefPanel({ idea }: { idea: Idea }) {
  const { navigate } = useApp()
  const b = idea.brief!
  const [openFull, setOpenFull] = useState(false)
  const parsed = parseBrief(b.body ?? "")
  return (
    <Card className="overflow-hidden">
      <div className="flex items-center gap-2 border-b px-4 py-2.5">
        <FileText className="size-3.5 text-brand" />
        <h2 className="min-w-0 flex-1 truncate text-sm font-semibold">{parsed.title || b.title || "Brief"}</h2>
        {b.exists && (
          <Button variant="ghost" size="sm" onClick={() => navigate(pageRoute(b.path))}>
            Open in Memory <ArrowRight />
          </Button>
        )}
      </div>
      <div className={cn("relative px-4 py-4 text-sm", !openFull && "max-h-80 overflow-hidden")}>
        <BriefBody b={parsed} compact />
        {!openFull && <div aria-hidden className="pointer-events-none absolute inset-x-0 bottom-0 h-16 bg-gradient-to-t from-card to-transparent" />}
      </div>
      <div className="flex justify-center border-t py-1.5">
        <button onClick={() => setOpenFull((o) => !o)} className="text-xs text-muted-foreground hover:text-foreground">
          {openFull ? "Show less" : "Show the whole brief"}
        </button>
      </div>
    </Card>
  )
}
