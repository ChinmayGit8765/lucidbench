import { lazy, useEffect, useState, type ReactNode } from "react"
import { ArrowRight, FileDiff, KanbanSquare, Play, SquareTerminal, TriangleAlert } from "lucide-react"

import type { Command } from "@/components/CommandPalette"
import { ProviderMark, providerInfo, tintVar } from "@/components/ProviderMark"
import { StatTile } from "@/components/StatTile"
import { StatusPill } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { getJSON, usePoll } from "@/lib/api"
import { useApp } from "@/lib/app"
import { useNow } from "@/lib/time"
import { elapsedOf, formatElapsed, needsReview, sessionsPath, WORK_POLL_MS, type WorkSession } from "@/lib/work"
import type { ModuleDef } from "@/modules/types"

function WorkTile() {
  const { open } = useApp()
  const now = useNow(1000)
  const poll = usePoll<WorkSession[]>(sessionsPath, WORK_POLL_MS)
  const list = poll.data ?? []
  const running = list.filter((s) => s.status === "running")
  const review = list.filter(needsReview)
  return (
    <StatTile
      icon={SquareTerminal}
      label="Running sessions"
      onOpen={() => open("work")}
      loading={poll.loading && !poll.data}
      value={running.length}
      aside={
        review.length > 0 ? (
          <StatusPill tone="warning">{review.length} to review</StatusPill>
        ) : running.length > 0 ? (
          <StatusPill tone="info" pulse>
            working
          </StatusPill>
        ) : undefined
      }
      sub={list.length === 0 ? "No sessions yet" : `${list.length} ${list.length === 1 ? "session" : "sessions"} · agents in their own worktrees`}
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
  useEffect(() => {
    if (!paletteOpen) return
    let cancelled = false
    getJSON<{ cards: ReadyCard[] }>("/api/boards/work")
      .then((b) => !cancelled && setCards(b.cards.filter((c) => c.column === "Ready" || c.column === "Inbox")))
      .catch(() => !cancelled && setCards([]))
    return () => {
      cancelled = true
    }
  }, [paletteOpen])
  return [
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

export interface WorkAttention {
  key: string
  icon: ReactNode
  title: ReactNode
  meta: ReactNode
  action?: ReactNode
}

/** Needs attention: finished sessions whose diff waits for review. */
export function useWorkAttention(): WorkAttention[] {
  const { open } = useApp()
  const poll = usePoll<WorkSession[]>(sessionsPath, WORK_POLL_MS * 4)
  return (poll.data ?? [])
    .filter(needsReview)
    .slice(0, 6)
    .map((s) => {
      const d = s.diff
      const failed = s.status === "failed"
      return {
        key: `work-${s.id}`,
        icon: failed ? <TriangleAlert className="size-3.5 text-danger" /> : <FileDiff className="size-3.5 text-info" />,
        title: (
          <>
            {failed ? "Session failed" : "Session finished: review diff"} · {s.title}
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
    })
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
}
