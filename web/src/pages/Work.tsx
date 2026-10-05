import { type ReactNode } from "react"
import { FileDiff, GitBranch, GitPullRequest, Plus, SquareTerminal } from "lucide-react"

import { ProviderTile, providerInfo } from "@/components/ProviderMark"
import { PageHeader, RefreshButton } from "@/components/Shell"
import { Badge, StatusPill } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card } from "@/components/ui/card"
import { EmptyState, ErrorState, Skeleton } from "@/components/ui/states"
import { usePoll } from "@/lib/api"
import { useApp } from "@/lib/app"
import { absoluteTime, relativeTime, useNow } from "@/lib/time"
import { cn } from "@/lib/utils"
import {
  elapsedOf,
  formatCost,
  formatElapsed,
  needsReview,
  sessionsPath,
  STATUS_INFO,
  WORK_POLL_MS,
  type WorkSession,
} from "@/lib/work"
import type { ModulePageProps } from "@/modules/types"
import { NewSession } from "@/pages/work/NewSession"
import { SessionView } from "@/pages/work/SessionView"

/** /work lists sessions, /work/new starts one, /work/<id> shows one. */
export default function Work({ subpath }: ModulePageProps) {
  // /work/new, /work/new/<card id>, or /work/new/project/<project id>.
  if (subpath[0] === "new") {
    return subpath[1] === "project" ? <NewSession project={subpath[2]} /> : <NewSession card={subpath[1]} />
  }
  if (subpath[0]) return <SessionView id={subpath[0]} />
  return <SessionList />
}

function Cell({ label, value, sub }: { label: string; value: ReactNode; sub?: ReactNode }) {
  return (
    <div className="min-w-0 px-4 py-3.5">
      <div className="text-2xs font-medium uppercase tracking-[0.08em] text-subtle-foreground">{label}</div>
      <div className="mt-1 truncate text-xl font-semibold tabular-nums tracking-tight">{value}</div>
      {sub && <div className="mt-0.5 truncate text-xs text-muted-foreground">{sub}</div>}
    </div>
  )
}

function SessionList() {
  const { open } = useApp()
  const now = useNow(1000)
  const poll = usePoll<WorkSession[]>(sessionsPath, WORK_POLL_MS)
  const list = poll.data ?? []
  const running = list.filter((s) => s.status === "running")
  const review = list.filter(needsReview)
  const prs = list.filter((s) => s.pr_url)
  const spent = list.reduce((n, s) => n + (s.usage?.cost_usd ?? 0), 0)
  return (
    <div className="space-y-6">
      <PageHeader
        icon={<SquareTerminal />}
        title="Work"
        description="Hand a task to Claude Code, Codex or Grok. Each session gets its own git worktree and branch; you review the diff and open the PR."
        actions={
          <>
            <RefreshButton refreshing={poll.refreshing} updatedAt={poll.updatedAt} />
            <Button onClick={() => open("work", ["new"])}>
              <Plus /> New session
            </Button>
          </>
        }
      />

      {poll.error && !poll.data && <ErrorState title="Could not load sessions" message={poll.error.message} onRetry={poll.refresh} />}

      {poll.loading && !poll.data ? (
        <div className="space-y-3">
          <Skeleton className="h-20 rounded-xl" />
          <Skeleton className="h-64 rounded-xl" />
        </div>
      ) : list.length === 0 ? (
        <Card>
          <EmptyState
            icon={<SquareTerminal />}
            satellites={[
              <ProviderTile key="c" provider="claude" size="sm" />,
              <ProviderTile key="x" provider="codex" size="sm" />,
              <ProviderTile key="g" provider="grok" size="sm" />,
              <span key="b" className="flex size-6 items-center justify-center rounded-md border bg-elevated text-subtle-foreground">
                <GitBranch className="size-3.5" />
              </span>,
            ]}
            title="No sessions yet"
            description="Start a session on a project with a local checkout. The agent works in a fresh worktree on its own branch, so your checkout stays untouched."
          >
            <Button onClick={() => open("work", ["new"])}>
              <Plus /> Start a session
            </Button>
          </EmptyState>
        </Card>
      ) : (
        <>
          <Card className="grid grid-cols-2 divide-x divide-y overflow-hidden @3xl:grid-cols-4 @3xl:divide-y-0">
            <Cell label="Running" value={running.length} sub={running.length ? running.map((s) => s.title).join(" · ") : "no agent working"} />
            <Cell label="To review" value={review.length} sub="finished, no PR yet" />
            <Cell label="Draft PRs" value={prs.length} sub="opened from Work" />
            <Cell label="Spent" value={formatCost(spent)} sub="as the CLIs report it" />
          </Card>

          <Card className="overflow-hidden">
            <div className="overflow-x-auto">
              <table className="w-full table-fixed text-sm">
                <colgroup>
                  <col />
                  <col className="w-28" />
                  <col className="w-32" />
                  <col className="w-[24%]" />
                  <col className="w-24" />
                  <col className="w-20" />
                </colgroup>
                <thead>
                  <tr className="border-b bg-muted/40 text-left text-2xs font-medium uppercase tracking-[0.06em] text-subtle-foreground">
                    <th className="px-5 py-2 font-medium">Task</th>
                    <th className="px-3 py-2 font-medium">Status</th>
                    <th className="px-3 py-2 font-medium">Agent</th>
                    <th className="px-3 py-2 font-medium">Branch</th>
                    <th className="px-3 py-2 text-right font-medium">Started</th>
                    <th className="px-3 py-2 pr-5 text-right font-medium">Cost</th>
                  </tr>
                </thead>
                <tbody>
                  {list.map((s) => {
                    const st = STATUS_INFO[s.status]
                    return (
                      <tr
                        key={s.id}
                        onClick={() => open("work", [s.id])}
                        className="group cursor-pointer border-b transition-colors last:border-b-0 hover:bg-accent/40"
                      >
                        <td className="px-5 py-2.5">
                          <button
                            onClick={(e) => {
                              e.stopPropagation()
                              open("work", [s.id])
                            }}
                            className="block max-w-full text-left outline-none focus-visible:underline"
                          >
                            <span className="block truncate font-medium">{s.title}</span>
                            <span className="flex items-center gap-1.5 truncate text-xs text-muted-foreground">
                              {s.project}
                              {s.diff && (s.diff.added > 0 || s.diff.deleted > 0) && (
                                <span className="font-mono text-2xs">
                                  <span className="text-success-fg">+{s.diff.added}</span> <span className="text-danger-fg">−{s.diff.deleted}</span>
                                </span>
                              )}
                              {s.pr_url && (
                                <span className="inline-flex items-center gap-0.5 text-2xs text-success-fg">
                                  <GitPullRequest className="size-3" /> PR
                                </span>
                              )}
                              {needsReview(s) && s.status === "done" && (
                                <span className="inline-flex items-center gap-0.5 text-2xs text-warning-fg">
                                  <FileDiff className="size-3" /> review
                                </span>
                              )}
                            </span>
                          </button>
                        </td>
                        <td className="px-3 py-2.5">
                          <StatusPill tone={st.tone} pulse={s.status === "running"}>
                            {s.status === "running" ? formatElapsed(elapsedOf(s, now)) : st.label}
                          </StatusPill>
                        </td>
                        <td className="px-3 py-2.5">
                          <span className="flex items-center gap-2">
                            <ProviderTile provider={s.provider} size="sm" />
                            <span className="min-w-0">
                              <span className="block truncate text-xs">{providerInfo(s.provider)?.label ?? s.provider}</span>
                              <span className="block truncate text-2xs text-subtle-foreground">{s.harness === "mine" ? "your harness" : "clean"}</span>
                            </span>
                          </span>
                        </td>
                        <td className="px-3 py-2.5">
                          <Badge className={cn("max-w-full font-mono", s.removed && "line-through opacity-60")} title={s.removed ? "worktree removed" : s.branch}>
                            <GitBranch />
                            <span className="truncate">{s.branch}</span>
                          </Badge>
                        </td>
                        <td className="px-3 py-2.5 text-right text-xs tabular-nums text-muted-foreground" title={absoluteTime(s.started)}>
                          {relativeTime(s.started, now)}
                        </td>
                        <td className="px-3 py-2.5 pr-5 text-right font-mono text-xs tabular-nums">{formatCost(s.usage?.cost_usd)}</td>
                      </tr>
                    )
                  })}
                </tbody>
              </table>
            </div>
          </Card>
        </>
      )}
    </div>
  )
}
