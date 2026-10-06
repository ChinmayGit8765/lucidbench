import { Component, useState, type ReactNode } from "react"
import {
  ArrowRight,
  Blocks,
  Boxes,
  ChevronRight,
  CircleCheck,
  FileDiff,
  FileText,
  GitPullRequest,
  PenLine,
  Play,
  Plus,
  ServerCog,
  SquareKanban,
  SquareTerminal,
  type LucideIcon,
} from "lucide-react"

import { RunIcon, RunnerDot } from "@/components/ci"
import { SectionGrid } from "@/components/SectionGrid"
import { PageHeader, RefreshButton } from "@/components/Shell"
import { SpriteBoard } from "@/components/ThemeArt"
import { Button } from "@/components/ui/button"
import { Card } from "@/components/ui/card"
import { LoadingArt, Skeleton } from "@/components/ui/states"
import { usePoll } from "@/lib/api"
import { useApp } from "@/lib/app"
import { DEFAULT_BOARD, READY_COLUMN, REVIEW_COLUMN, useWorkBoard } from "@/lib/boards"
import { CI_POLL_MS, formatDuration, runDuration, runState, shortRepo, type CIRunners, type CIRuns } from "@/lib/ci"
import { COUNCIL_POLL_MS, newBraindump, SESSIONS_PATH, type CouncilSummary } from "@/lib/council"
import { runHelloJob, type ClusterInfo, type Job } from "@/lib/jobs"
import { usePrefs } from "@/lib/prefs"
import { absoluteTime, relativeTime, useNow } from "@/lib/time"
import { cn, isMac } from "@/lib/utils"
import { prIsOpen, sessionsPath, WORK_POLL_MS, type WorkSession } from "@/lib/work"
import { isOpenable, moduleById, navOrder, useAttention } from "@/modules/registry"
import type { AttentionItem } from "@/modules/types"

function greeting(d: Date): string {
  const h = d.getHours()
  if (h < 5) return "Working late"
  if (h < 12) return "Good morning"
  if (h < 18) return "Good afternoon"
  return "Good evening"
}

/* ---------- needs attention ---------- */

const SEVERITY_BAR: Record<AttentionItem["severity"], string> = {
  danger: "bg-danger",
  warning: "bg-warning",
  info: "bg-info",
}

function AttentionRow({ a, i = 0 }: { a: AttentionItem; i?: number }) {
  return (
    <li className="lb-rise relative flex items-center gap-3 px-5 py-2.5 transition-colors hover:bg-accent/30" style={{ "--i": i } as React.CSSProperties}>
      <span aria-hidden className={cn("absolute inset-y-2 left-0 w-0.5 rounded-full", SEVERITY_BAR[a.severity])} />
      <span className="flex size-7 shrink-0 items-center justify-center rounded-md border bg-background/60">{a.icon}</span>
      <div className="min-w-0 flex-1">
        <div className="truncate text-sm font-medium">{a.title}</div>
        <div className="truncate text-xs text-muted-foreground">{a.meta}</div>
      </div>
      {a.action && <div className="flex shrink-0 items-center gap-1">{a.action}</div>}
    </li>
  )
}

/* ---------- today's loop ---------- */

interface Stage {
  key: string
  label: string
  icon: LucideIcon
  count: number | null
  /** The stage waits on the user, so a count reads as a call to act. */
  yours?: boolean
  hint: string
  go: () => void
}

/**
 * The core loop at a glance: braindump → brief → card → session → review →
 * PR, each with its count and a link to where it lives.
 */
function TodaysLoop() {
  const { open } = useApp()
  const councilPoll = usePoll<CouncilSummary[]>(SESSIONS_PATH, COUNCIL_POLL_MS)
  const workPoll = usePoll<WorkSession[]>(sessionsPath, WORK_POLL_MS * 2)
  const wb = useWorkBoard()
  const council = councilPoll.data
  const sessions = workPoll.data
  const cards = wb.board?.cards.filter((c) => !c.done) ?? (wb.list.data ? [] : null)

  const running = council?.filter((s) => s.status === "running") ?? null
  const drafts = council?.filter((s) => s.status === "draft") ?? null
  const ready = cards?.filter((c) => c.column === READY_COLUMN) ?? null
  const working = sessions?.filter((s) => s.status === "running") ?? null
  // "Review" is the board's Review column, so this strip and the Board agree.
  const review = cards?.filter((c) => c.column === REVIEW_COLUMN) ?? null
  // Only PRs still open on GitHub (draft or open), not merged or closed ones.
  const prs = sessions?.filter(prIsOpen) ?? null
  // One item: go straight to it; several: to the list.
  const one = <T,>(xs: T[] | null, to: (x: T) => void, all: () => void) => () => (xs && xs.length === 1 ? to(xs[0]) : all())

  const stages: Stage[] = [
    {
      key: "braindumps",
      label: "Braindumps",
      icon: PenLine,
      count: running && running.length,
      hint: "being clarified",
      go: one(running, (s) => open("council", [s.id]), () => open("council")),
    },
    {
      key: "briefs",
      label: "Briefs waiting",
      icon: FileText,
      count: drafts && drafts.length,
      yours: true,
      hint: "to approve",
      go: one(drafts, (s) => open("council", [s.id]), () => open("council")),
    },
    {
      key: "ready",
      label: "Ready",
      icon: SquareKanban,
      count: ready && ready.length,
      yours: true,
      hint: ready?.length === 1 ? "card to start" : "cards to start",
      go: one(ready, (c) => open("boards", [DEFAULT_BOARD, c.id]), () => open("boards", [DEFAULT_BOARD])),
    },
    {
      key: "working",
      label: "In progress",
      icon: SquareTerminal,
      count: working && working.length,
      hint: working?.length === 1 ? "agent working" : "agents working",
      go: one(working, (s) => open("work", [s.id]), () => open("work")),
    },
    {
      key: "review",
      label: "Review",
      icon: FileDiff,
      count: review && review.length,
      yours: true,
      hint: review?.length === 1 ? "card in review" : "cards in review",
      go: one(review, (c) => open("boards", [DEFAULT_BOARD, c.id]), () => open("boards", [DEFAULT_BOARD])),
    },
    {
      key: "prs",
      label: "PRs open",
      icon: GitPullRequest,
      count: prs && prs.length,
      hint: "not yet merged",
      go: one(prs, (s) => open("work", [s.id]), () => open("work")),
    },
  ]

  return (
    <Card className="overflow-hidden">
      <div className="flex items-center justify-between gap-3 px-5 pb-1 pt-4">
        <h2 className="text-sm font-semibold">Today's loop</h2>
        <span className="text-xs text-subtle-foreground max-[720px]:hidden">braindump → brief → card → agent → review → PR</span>
      </div>
      <ol className="grid grid-cols-3 gap-px p-2 @3xl:grid-cols-6" aria-label="Today's loop">
        {stages.map((st, i) => {
          const Icon = st.icon
          const lit = (st.count ?? 0) > 0
          return (
            <li key={st.key} className="relative">
              <button
                onClick={st.go}
                aria-label={`${st.label}: ${st.count ?? "loading"}`}
                className={cn(
                  "group flex h-full w-full flex-col items-start rounded-lg px-3 py-2.5 text-left outline-none transition-colors hover:bg-accent/40 focus-visible:ring-2 focus-visible:ring-ring",
                  lit && st.yours && "bg-warning-soft/50 hover:bg-warning-soft/80",
                )}
              >
                <span className="flex items-center gap-1.5 text-2xs font-medium uppercase tracking-[0.08em] text-subtle-foreground">
                  <Icon className={cn("size-3.5", lit && (st.yours ? "text-warning" : "text-brand"))} />
                  {st.label}
                </span>
                {st.count === null ? (
                  <Skeleton className="mt-1.5 h-7 w-8" />
                ) : (
                  <span className={cn("mt-0.5 text-2xl font-semibold tabular-nums tracking-tight", !lit && "text-subtle-foreground")}>{st.count}</span>
                )}
                <span className="truncate text-2xs text-muted-foreground">{st.hint}</span>
              </button>
              {i < stages.length - 1 && (
                <ChevronRight aria-hidden className="pointer-events-none absolute -right-2 top-1/2 z-[1] size-4 -translate-y-1/2 text-border-strong max-[720px]:hidden" />
              )}
            </li>
          )
        })}
      </ol>
    </Card>
  )
}

/* ---------- activity ---------- */

interface ActivityItem {
  key: string
  at: string
  icon: ReactNode
  title: ReactNode
  meta: ReactNode
  right?: ReactNode
  href?: string
}

/* ---------- page ---------- */

export default function Overview() {
  const { open, addAccount: onAddAccount } = useApp()
  const { prefs, label } = usePrefs()
  const ciOn = isOpenable(moduleById("runners")!, prefs)
  const tiles = navOrder(prefs).filter((m) => m.overviewTile && isOpenable(m, prefs))
  const now = useNow(5000)
  const cluster = usePoll<ClusterInfo>("/api/cluster", 15000)
  const jobs = usePoll<Job[]>("/api/jobs", 15000)
  const runners = usePoll<CIRunners>("/api/ci/runners", CI_POLL_MS)
  const runsPoll = usePoll<CIRuns>("/api/ci/runs", CI_POLL_MS)
  const { items: attention, loading: attentionLoading, pending: attentionPending } = useAttention(prefs)
  const [helloBusy, setHelloBusy] = useState(false)

  const all = [cluster, jobs, runners, runsPoll]
  const refreshing = all.some((p) => p.refreshing)
  const updated = all.map((p) => p.updatedAt).filter((t): t is number => t !== null)

  const runs = runsPoll.data?.runs ?? []
  const fleet = runners.data?.runners ?? []
  const containers = runners.data?.containers ?? []
  const c = cluster.data
  const jobList = jobs.data ?? []

  const hello = async () => {
    setHelloBusy(true)
    await runHelloJob()
    setHelloBusy(false)
  }

  /* activity */
  const activity: ActivityItem[] = [
    ...(ciOn ? runs : []).slice(0, 12).map(
      (r): ActivityItem => ({
        key: `run-${r.repo}-${r.id}`,
        at: r.created_at,
        icon: <RunIcon run={r} />,
        title: (
          <>
            <span className="font-medium">{r.name}</span>
            <span className="text-muted-foreground"> · {r.title || r.event}</span>
          </>
        ),
        meta: (
          <>
            {shortRepo(r.repo)} · <span className="font-mono">{r.branch}</span>
          </>
        ),
        right: runState(r) === "queued" ? "queued" : formatDuration(runDuration(r, now)),
        href: r.html_url,
      }),
    ),
    ...jobList.map(
      (j): ActivityItem => ({
        key: `job-${j.name}`,
        at: j.createdAt,
        icon: <Boxes className={cn("size-4", j.status === "Failed" ? "text-danger" : j.status === "Completed" ? "text-success" : "text-info")} />,
        title: (
          <>
            <span className="font-medium">Job</span>
            <span className="text-muted-foreground"> · {j.name}</span>
          </>
        ),
        meta: <>cluster · {j.status.toLowerCase() || "unknown"}</>,
      }),
    ),
  ]
    .sort((a, b) => Date.parse(b.at) - Date.parse(a.at))
    .slice(0, 8)

  const today = new Date(now)
  return (
    <div className="space-y-6">
      <PageHeader
        eyebrow={today.toLocaleDateString(undefined, { weekday: "long", day: "numeric", month: "long" })}
        title={label("overview_title", greeting(today))}
        description={
          attentionLoading
            ? "Checking your workspace…"
            : attention.length === 0
              ? attentionPending
                ? "Checking your workspace…"
                : "Everything is running. Nothing needs you right now."
              : `${attention.length} ${attention.length === 1 ? "thing needs" : "things need"} your attention.`
        }
        actions={
          <>
            <RefreshButton refreshing={refreshing} updatedAt={updated.length ? Math.min(...updated) : null} />
            <Button size="sm" onClick={() => newBraindump(open)}>
              <PenLine /> New braindump
            </Button>
          </>
        }
      />

      <TodaysLoop />

      <div className="grid grid-cols-2 gap-3 @3xl:grid-cols-3">
        {tiles.map((m, i) => {
          const Tile = m.overviewTile!
          // Each tile loads, and fails, on its own.
          return (
            <div key={m.id} className="lb-rise [&>*]:h-full" style={{ "--i": i } as React.CSSProperties}>
              <TileBoundary title={m.title}>
                <Tile />
              </TileBoundary>
            </div>
          )
        })}
      </div>

      <SectionGrid placement="overview" />

      <SpriteBoard />

      <div className="grid gap-4 @4xl:grid-cols-[minmax(0,1.55fr)_minmax(0,1fr)]">
        <div className="space-y-4">
          <Card className="overflow-hidden">
            <div className="flex items-center justify-between px-5 pb-3 pt-4">
              <h2 className="flex items-center gap-2 text-sm font-semibold">
                {label("attention", "Needs attention")}
                {attention.length > 0 && (
                  <span
                    className={cn(
                      "rounded-full px-1.5 text-2xs tabular-nums",
                      attention[0].severity === "danger"
                        ? "bg-danger-soft text-danger-fg"
                        : attention[0].severity === "warning"
                          ? "bg-warning-soft text-warning-fg"
                          : "bg-info-soft text-info-fg",
                    )}
                  >
                    {attention.length}
                  </span>
                )}
              </h2>
            </div>
            {attentionLoading ? (
              <div className="space-y-2 border-t p-5">
                <Skeleton className="h-9" />
                <Skeleton className="h-9" />
              </div>
            ) : attention.length === 0 && attentionPending ? (
              <div className="space-y-2 border-t p-5">
                <Skeleton className="h-9" />
              </div>
            ) : attention.length === 0 ? (
              <div className="flex items-center gap-3 border-t px-5 py-5">
                <span className="flex size-8 items-center justify-center rounded-full bg-success-soft text-success">
                  <CircleCheck className="size-4" />
                </span>
                <div className="min-w-0 flex-1">
                  <div className="text-sm font-medium">{label("all_clear", "All clear")}</div>
                  <div className="text-xs text-muted-foreground">No briefs to approve, diffs to review, usage limits, failed runs or blocked needs. Got an idea? Start the loop with a braindump.</div>
                </div>
                <Button size="sm" variant="secondary" onClick={() => newBraindump(open)} title="New braindump (n)">
                  <PenLine /> Braindump
                </Button>
              </div>
            ) : (
              <ul className="divide-y border-t">
                {attention.map((a, i) => (
                  <AttentionRow key={a.key} a={a} i={i} />
                ))}
              </ul>
            )}
          </Card>

          <Card className="overflow-hidden">
            <div className="flex items-center justify-between px-5 pb-3 pt-4">
              <h2 className="text-sm font-semibold">Recent activity</h2>
              {ciOn && (
                <Button variant="ghost" size="sm" onClick={() => open("runners")}>
                  All runs <ArrowRight />
                </Button>
              )}
            </div>
            {runsPoll.loading && !runsPoll.data ? (
              <LoadingArt label="Reading CI runs and cluster jobs…" className="h-[168px] border-t" />
            ) : activity.length === 0 ? (
              <p className="border-t px-5 py-6 text-center text-sm text-muted-foreground">
                No runs or jobs yet. CI runs show up here once you add a repository under Runners & CI; cluster jobs once you run one.
              </p>
            ) : (
              <ol className="relative border-t py-1.5">
                <span aria-hidden className="absolute bottom-4 left-[29px] top-4 w-px bg-border" />
                {activity.map((it) => {
                  const body = (
                    <>
                      <span className="relative z-[1] flex size-6 shrink-0 items-center justify-center rounded-full bg-card">
                        {it.icon}
                      </span>
                      <div className="min-w-0 flex-1">
                        <div className="truncate text-sm">{it.title}</div>
                        <div className="truncate text-xs text-subtle-foreground">{it.meta}</div>
                      </div>
                      {it.right && (
                        <span className="shrink-0 font-mono text-2xs tabular-nums text-subtle-foreground">{it.right}</span>
                      )}
                      <time
                        dateTime={it.at}
                        title={absoluteTime(it.at)}
                        className="w-16 shrink-0 text-right text-xs tabular-nums text-muted-foreground"
                      >
                        {relativeTime(it.at, now)}
                      </time>
                    </>
                  )
                  return (
                    <li key={it.key}>
                      {it.href ? (
                        <a
                          href={it.href}
                          target="_blank"
                          rel="noreferrer"
                          className="flex items-center gap-3 px-4 py-1.5 outline-none transition-colors hover:bg-accent/30 focus-visible:bg-accent/50"
                        >
                          {body}
                        </a>
                      ) : (
                        <div className="flex items-center gap-3 px-4 py-1.5">{body}</div>
                      )}
                    </li>
                  )
                })}
              </ol>
            )}
          </Card>
        </div>

        <div className="space-y-4">
          <Card className="p-4">
            <h2 className="mb-3 text-sm font-semibold">Quick actions</h2>
            <div className="grid gap-2">
              <QuickAction
                icon={SquareTerminal}
                title="Start work"
                hint="An agent on a card or a prompt, in its own worktree"
                onClick={() => open("work", ["new"])}
              />
              <QuickAction
                icon={Play}
                title="Run hello job"
                hint={c?.running ? "Check the cluster end to end" : "Needs a running cluster"}
                disabled={!c?.running || helloBusy}
                onClick={() => void hello()}
              />
              <QuickAction icon={Plus} title="Add account" hint="Sign in another provider profile" onClick={onAddAccount} />
              {ciOn ? (
                <QuickAction
                  icon={ServerCog}
                  title="Open Runners & CI"
                  hint="Fleet, containers and every run"
                  onClick={() => open("runners")}
                />
              ) : (
                <QuickAction
                  icon={Blocks}
                  title="Browse extensions"
                  hint="Add Runners & CI, Containers and more"
                  onClick={() => open("settings", ["extensions"])}
                />
              )}
            </div>
            <p className="mt-3 flex items-center gap-1.5 text-xs text-subtle-foreground">
              Everything else:
              <kbd className="rounded border bg-muted px-1 font-sans text-2xs">{isMac() ? "⌘" : "Ctrl"}</kbd>
              <kbd className="rounded border bg-muted px-1 font-sans text-2xs">K</kbd>
            </p>
          </Card>

          {ciOn && (
          <Card className="overflow-hidden">
            <div className="flex items-center justify-between px-4 pb-2 pt-4">
              <h2 className="text-sm font-semibold">Fleet</h2>
              <span className="text-xs tabular-nums text-subtle-foreground">
                {containers.filter((x) => x.state === "running").length}/{containers.length} containers up
              </span>
            </div>
            {runners.loading && !runners.data ? (
              <div className="space-y-2 p-4 pt-1">
                <Skeleton className="h-7" />
                <Skeleton className="h-7" />
              </div>
            ) : containers.length === 0 && fleet.length === 0 ? (
              <p className="px-4 pb-4 text-xs text-muted-foreground">No self-hosted runners or runner containers found.</p>
            ) : (
              <ul className="pb-2">
                {containers.map((ct) => {
                  const r = fleet.find((x) => x.container === ct.name)
                  return (
                    <li key={ct.name} className="flex items-center gap-2.5 px-4 py-1.5 text-sm">
                      <RunnerDot status={r ? r.status : ct.state === "running" ? "unknown" : "offline"} busy={r?.busy} />
                      <span className="min-w-0 flex-1 truncate font-mono text-xs" title={ct.name}>
                        {r?.name ?? ct.runner_name ?? ct.name}
                      </span>
                      <span className="shrink-0 text-xs text-subtle-foreground">
                        {r ? (r.busy ? "busy" : r.status === "online" ? "idle" : "offline") : ct.state === "running" ? "up · not watched" : "stopped"}
                      </span>
                    </li>
                  )
                })}
                {fleet
                  .filter((r) => !r.container)
                  .map((r) => (
                    <li key={`${r.repo}-${r.id}`} className="flex items-center gap-2.5 px-4 py-1.5 text-sm">
                      <RunnerDot status={r.status} busy={r.busy} />
                      <span className="min-w-0 flex-1 truncate font-mono text-xs">{r.name}</span>
                      <span className="shrink-0 text-xs text-subtle-foreground">{r.busy ? "busy" : r.status}</span>
                    </li>
                  ))}
              </ul>
            )}
          </Card>
          )}
        </div>
      </div>
    </div>
  )
}

/** Keeps one broken tile from taking the Overview down with it. */
class TileBoundary extends Component<{ title: string; children: ReactNode }, { failed: boolean }> {
  state = { failed: false }
  static getDerivedStateFromError() {
    return { failed: true }
  }
  render() {
    if (!this.state.failed) return this.props.children
    return (
      <Card className="flex h-full items-center p-4 text-xs text-muted-foreground">
        {this.props.title} could not be shown. Reload the page to try again.
      </Card>
    )
  }
}

function QuickAction({
  icon: Icon,
  title,
  hint,
  onClick,
  disabled,
}: {
  icon: LucideIcon
  title: string
  hint: string
  onClick: () => void
  disabled?: boolean
}) {
  return (
    <button
      onClick={onClick}
      disabled={disabled}
      className="group flex items-center gap-3 rounded-lg border bg-background/40 px-3 py-2.5 text-left outline-none transition-[border-color,background-color] duration-150 hover:border-border-strong hover:bg-accent/40 focus-visible:ring-2 focus-visible:ring-ring disabled:cursor-not-allowed disabled:opacity-55"
    >
      <span className="flex size-8 shrink-0 items-center justify-center rounded-md bg-brand-soft text-brand-fg">
        <Icon className="size-4" />
      </span>
      <span className="min-w-0 flex-1">
        <span className="block text-sm font-medium">{title}</span>
        <span className="block truncate text-xs text-muted-foreground">{hint}</span>
      </span>
      <ArrowRight className="size-4 text-subtle-foreground transition-transform group-hover:translate-x-0.5 group-disabled:hidden" />
    </button>
  )
}

