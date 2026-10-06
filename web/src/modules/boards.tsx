import { lazy, useEffect, useState } from "react"
import { Plus, SquareKanban } from "lucide-react"

import type { Command } from "@/components/CommandPalette"
import { StatTile } from "@/components/StatTile"
import { StatusPill } from "@/components/ui/badge"
import { getJSON, usePoll } from "@/lib/api"
import { useApp } from "@/lib/app"
import {
  BOARDS_POLL_MS,
  DEFAULT_BOARD,
  READY_COLUMN,
  requestBoards,
  type Board,
  type BoardSummary,
} from "@/lib/boards"
import { plural } from "@/lib/utils"
import type { ModuleDef } from "@/modules/types"

/**
 * Cards waiting in the Ready column of the work board. The tile reads the
 * board list first, so an Overview visit never creates the board file.
 */
function ReadyTile() {
  const { open } = useApp()
  const list = usePoll<BoardSummary[]>("/api/boards", BOARDS_POLL_MS)
  const has = list.data?.some((b) => b.id === DEFAULT_BOARD) ?? false
  const [board, setBoard] = useState<Board | null>(null)
  useEffect(() => {
    if (!has) return
    let cancelled = false
    getJSON<Board>(`/api/boards/${DEFAULT_BOARD}`)
      .then((b) => !cancelled && setBoard(b))
      .catch(() => undefined)
    return () => {
      cancelled = true
    }
  }, [has, list.updatedAt])
  const cards = board?.cards ?? []
  const ready = cards.filter((c) => c.column === READY_COLUMN && !c.done)
  const doing = cards.filter((c) => c.column === "In progress" && !c.done).length
  return (
    <StatTile
      icon={SquareKanban}
      label="Ready cards"
      onOpen={() => open("boards", [DEFAULT_BOARD])}
      loading={list.loading && !list.data && !list.error}
      value={has ? ready.length : <span className="text-base font-medium text-muted-foreground">No board yet</span>}
      aside={doing > 0 ? <StatusPill tone="info">{doing} in progress</StatusPill> : undefined}
      sub={has ? `on ${board?.title ?? "Work"} · ${plural(cards.filter((c) => !c.done).length, "open card")}` : list.error ? list.error.message : "Add a card to start the work board"}
      footer={
        ready.length > 0 ? (
          <ul className="space-y-1">
            {ready.slice(0, 3).map((c) => (
              <li key={c.id} className="flex items-center gap-2 text-xs text-muted-foreground">
                <span className="size-1.5 shrink-0 rounded-full bg-brand" />
                <span className="truncate">{c.title}</span>
                {c.project && <span className="ml-auto shrink-0 font-mono text-2xs text-subtle-foreground">{c.project}</span>}
              </li>
            ))}
          </ul>
        ) : undefined
      }
    />
  )
}

/** "Add card…" and "Open board…" (a nested list of boards). */
function useBoardCommands(paletteOpen: boolean): Command[] {
  const { open } = useApp()
  const [boards, setBoards] = useState<BoardSummary[]>([])
  useEffect(() => {
    if (!paletteOpen) return
    let cancelled = false
    getJSON<BoardSummary[]>("/api/boards")
      .then((b) => !cancelled && setBoards(b))
      .catch(() => undefined)
    return () => {
      cancelled = true
    }
  }, [paletteOpen])
  const list = boards.some((b) => b.id === DEFAULT_BOARD) ? boards : [{ id: DEFAULT_BOARD, title: "Work", cards: 0, columns: 5, modified: "" }, ...boards]
  return [
    {
      id: "boards-add-card",
      label: "Add card…",
      group: "Actions",
      icon: Plus,
      keywords: "board kanban task todo ticket new",
      run: () => {
        requestBoards({ kind: "add-card", board: DEFAULT_BOARD })
        open("boards", [DEFAULT_BOARD])
      },
    },
    {
      id: "boards-open",
      label: "Open board…",
      group: "Actions",
      icon: SquareKanban,
      keywords: "board kanban tasks",
      children: list.map((b) => ({
        id: `board-${b.id}`,
        label: b.title,
        group: "Boards",
        icon: SquareKanban,
        hint: plural(b.cards, "card"),
        keywords: b.id,
        run: () => open("boards", [b.id]),
      })),
    },
  ]
}

export const boards: ModuleDef = {
  id: "boards",
  title: "Boards",
  icon: SquareKanban,
  route: "/boards",
  section: "workspace",
  kind: "core",
  order: 4,
  defaultEnabled: true,
  description: "Lucidbench's own task boards, stored as Kanban files in your Memory vault.",
  keywords: "kanban tasks tickets cards trello",
  component: lazy(() => import("@/pages/Boards")),
  useCommands: useBoardCommands,
  overviewTile: ReadyTile,
}
