import { lazy, useEffect, useState } from "react"
import { ArrowRight, FileDiff, Hourglass, KanbanSquare, Play, SquareTerminal, TriangleAlert } from "lucide-react"

import type { Command } from "@/components/CommandPalette"
import { ProviderMark, providerInfo, tintVar } from "@/components/ProviderMark"
import { StatTile } from "@/components/StatTile"
import { StatusPill } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { getJSON, usePoll } from "@/lib/api"
import { useApp } from "@/lib/app"
import { useNow } from "@/lib/time"
import { plural } from "@/lib/utils"
import { elapsedOf, formatElapsed, isWaiting, needsReview, sessionsPath, WORK_POLL_MS, type WorkSession } from "@/lib/work"
import type { AttentionItem, ModuleDef } from "@/modules/types"

function WorkTile() {
  const { open } = useApp()
  const now = useNow(1000)
  const poll = usePoll<WorkSession[]>(sessionsPath, WORK_POLL_MS)
  const list = poll.data ?? []
  const running = list.filter((s) => s.status === "running")
  const review = list.filter(needsReview)
  const waiting = list.filter(isWaiting)
  return (
    <StatTile
      icon={SquareTerminal}
      label="Running sessions"
      onOpen={() => open("work")}
      loading={poll.loading && !poll.data}
      value={running.length}
      aside={
        waiting.length > 0 ? (
          <StatusPill tone="warning">{waiting.length} waiting for you</StatusPill>
        ) : review.length > 0 ? (
          <StatusPill tone="warning">{plural(review.length, "diff")} to review</StatusPill>
        ) : running.length > 0 ? (
          <StatusPill tone="info" pulse>
            working
          </StatusPill>
        ) : undefined
      }
      sub={list.length === 0 ? "No sessions yet" : `${plural(list.length, "session")} · agents in their own worktrees`}
      footer={
        running.length > 0 ? (
          <div className="space-y-1">
            {running.slice(0, 3).map((s) => (
              <div key={s.id} className="flex items-center gap-2 text-2xs text-muted-foreground">
                <span style={{ color: tintVar(s.provider) }}>
                  <ProviderMark provider={s.provider} className="size-3" />
                </span>
                <span className="min-w-0 flex-1 truncate">{s.title}</span>
                <span className="font-mono tabular-nums">{formatElapsed(elapsedOf(s, now))}</span>
              </div>
            ))}
          </div>
        ) : undefined
      }
    />
  )
}

interface ReadyCard {
  id: string
  title: string
  column: string
  project?: string
}

/** "Start work…" plus one entry per Ready card, fetched while the palette is open. */
function useWorkCommands(paletteOpen: boolean): Command[] {
  const { open } = useApp()
  const [cards, setCards] = useState<ReadyCard[] | null>(null)
  const [review, setReview] = useState<WorkSession[]>([])
  useEffect(() => {
    if (!paletteOpen) return
    let cancelled = false
    getJSON<{ cards: ReadyCard[] }>("/api/boards/work")
      .then((b) => !cancelled && setCards(b.cards.filter((c) => c.column === "Ready" || c.column === "Inbox")))
      .catch(() => !cancelled && setCards([]))
    getJSON<WorkSession[]>(sessionsPath)
      .then((l) => !cancelled && setReview(l.filter(needsReview)))
      .catch(() => undefined)
    return () => {
      cancelled = true
    }
  }, [paletteOpen])
  return [
    ...review.map(
      (s): Command => ({
        id: `work-review-${s.id}`,
        label: `Review diff: ${s.title}`,
        group: "Actions",
        icon: FileDiff,
        hint: `${providerInfo(s.provider)?.label ?? s.provider} · ${s.project}`,
        keywords: "work session diff review pr pull request",
        run: () => open("work", [s.id]),
      }),
    ),
    {
      id: "work-start",
      label: "Start work…",
      group: "Actions",
      icon: Play,
      hint: "an agent in a new worktree",
      keywords: "work session agent claude codex grok worktree prompt",
      run: () => open("work", ["new"]),
    },
    {
      id: "work-start-card",
      label: "Start work on card…",
      group: "Actions",
      icon: KanbanSquare,
      hint: cards === null ? "loading cards…" : cards.length === 0 ? "no cards in Ready" : `${cards.length} in Ready`,
      disabled: cards !== null && cards.length === 0,
      keywords: "work board card brief",
      children: (cards ?? []).map((c) => ({
        id: `work-card-${c.id}`,
        label: c.title,
        group: "Cards",
        icon: KanbanSquare,
        hint: c.project,
        run: () => open("work", ["new", c.id]),
      })),
    },
  ]
}

/**
 * Needs attention: sessions whose agent waits for your follow-up, as one
 * entry, then finished or failed sessions whose diff waits for review.
 */
function useWorkAttention(): AttentionItem[] | null {
  const { open } = useApp()
  const poll = usePoll<WorkSession[]>(sessionsPath, WORK_POLL_MS * 4)
  if (!poll.data) return poll.error ? [] : null
  const waiting = poll.data.filter(isWaiting)
  const waitingItem: AttentionItem[] = waiting.length
    ? [
        {
          key: "work-waiting",
          severity: "warning",
          icon: <Hourglass className="size-3.5 text-warning" />,
          title: <>{plural(waiting.length, "session")} waiting for you</>,
          meta: <>{waiting.slice(0, 3).map((s) => `${s.title} (${providerInfo(s.provider)?.label ?? s.provider})`).join(" · ")}</>,
          action: (
            <Button variant="ghost" size="sm" onClick={() => (waiting.length === 1 ? open("work", [waiting[0].id]) : open("work"))} data-testid="attention-waiting">
              Reply <ArrowRight />
            </Button>
          ),
        },
      ]
    : []
  return waitingItem.concat(
    poll.data
    .filter(needsReview)
    .slice(0, 6)
    .map((s): AttentionItem => {
      const d = s.diff
      const failed = s.status === "failed"
      return {
        key: `work-${s.id}`,
        severity: failed ? "danger" : "info",
        icon: failed ? <TriangleAlert className="size-3.5 text-danger" /> : <FileDiff className="size-3.5 text-info" />,
        title: (
          <>
            {failed
              ? "Session failed"
              : d && d.files.length === 0 && d.uncommitted.length === 0
                ? "Session finished without changes: read its answer"
                : "Session finished: review the diff"}{" "}
            · {s.title}
          </>
        ),
        meta: (
          <>
            {providerInfo(s.provider)?.label ?? s.provider} · {s.project}
            {d && ` · +${d.added} −${d.deleted} · ${d.commits.length} ${d.commits.length === 1 ? "commit" : "commits"}`}
          </>
        ),
        action: (
          <Button variant="ghost" size="sm" onClick={() => open("work", [s.id])}>
            Review <ArrowRight />
          </Button>
        ),
      }
    }),
  )
}

export const work: ModuleDef = {
  id: "work",
  title: "Work",
  icon: SquareTerminal,
  route: "/work",
  section: "workspace",
  kind: "core",
  order: 1,
  defaultEnabled: true,
  description: "Prompt Claude Code, Codex or Grok on a task, each in its own git worktree, and review the diff.",
  keywords: "chat agent prompt task environment worktree session claude codex grok terminal",
  component: lazy(() => import("@/pages/Work")),
  useCommands: useWorkCommands,
  overviewTile: WorkTile,
  useAttention: useWorkAttention,
}
