import { useEffect, useState } from "react"

import { ApiError, getJSON, sendJSON, usePoll } from "@/lib/api"
import { memoryApi } from "@/lib/memory"

/** A card. Only these fields go to the API: it rejects unknown ones. */
export interface Card {
  id: string
  title: string
  column: string
  project?: string
  memory?: string
  council?: string
  work?: string
  due?: string
  labels?: string[]
  done: boolean
}

export interface Board {
  id: string
  title: string
  columns: string[]
  cards: Card[]
}

export interface BoardSummary {
  id: string
  title: string
  columns: number
  cards: number
  modified: string
}

export const DEFAULT_BOARD = "work"
export const READY_COLUMN = "Ready"
export const BOARDS_POLL_MS = 20000

const q = encodeURIComponent

export type CardFields = Partial<Omit<Card, "id">>

export const boardsApi = {
  list: () => getJSON<BoardSummary[]>("/api/boards"),
  get: (id: string) => getJSON<Board>(`/api/boards/${q(id)}`),
  add: (board: string, c: CardFields & { title: string }) => sendJSON<Card>(`/api/boards/${q(board)}/cards`, "POST", c),
  update: (board: string, id: string, c: CardFields) => sendJSON<Card>(`/api/boards/${q(board)}/cards/${q(id)}`, "PUT", c),
  move: (board: string, id: string, column: string, index: number) =>
    sendJSON<Card>(`/api/boards/${q(board)}/cards/${q(id)}`, "PUT", { column, index }),
}

/**
 * The default work board, followed by polling. It reads the board list
 * first, so a visit that only looks (Overview) never creates the board
 * file. board stays null until the board exists and has loaded.
 */
export function useWorkBoard(intervalMs = BOARDS_POLL_MS) {
  const list = usePoll<BoardSummary[]>("/api/boards", intervalMs)
  const exists = list.data?.some((b) => b.id === DEFAULT_BOARD) ?? false
  const [board, setBoard] = useState<Board | null>(null)
  useEffect(() => {
    if (!exists) return
    let cancelled = false
    boardsApi
      .get(DEFAULT_BOARD)
      .then((b) => !cancelled && setBoard(b))
      .catch(() => undefined)
    return () => {
      cancelled = true
    }
  }, [exists, list.updatedAt])
  return { list, exists, board: exists ? board : null, loading: list.loading && !list.data && !list.error }
}

/** A board id from a title: lowercase letters, digits and dashes. */
export function boardId(title: string): string {
  return (
    title
      .toLowerCase()
      .normalize("NFKD")
      .replace(/[^a-z0-9]+/g, "-")
      .replace(/^-+|-+$/g, "")
      .slice(0, 48) || "board"
  )
}

/**
 * Creates a board as an Obsidian Kanban file in the vault's Boards folder.
 * The boards API has no create route; the file format is the contract.
 */
export async function createBoard(title: string, columns: string[]): Promise<string> {
  const id = boardId(title)
  const path = `Boards/${id}.md`
  try {
    await memoryApi.page(path)
    throw new ApiError(409, `A board named “${id}” already exists.`)
  } catch (e) {
    if (!(e instanceof ApiError) || e.status !== 404) throw e
  }
  const body = "\n" + columns.map((c) => `## ${c}\n\n`).join("") + "\n%% kanban:settings\n```\n{\"kanban-plugin\":\"basic\"}\n```\n%%\n"
  await memoryApi.save(path, { front: { "kanban-plugin": "basic", board: id, title }, body })
  return id
}

/** Colour for a label, stable across sessions. */
const LABEL_HUES = [255, 295, 155, 70, 25, 200, 330, 110]
export function labelColor(label: string): string {
  let h = 0
  for (const ch of label) h = (h * 31 + ch.charCodeAt(0)) >>> 0
  return `oklch(0.66 0.13 ${LABEL_HUES[h % LABEL_HUES.length]})`
}

/** A due date relative to today: overdue, today, soon or later. */
export function dueState(due?: string): "overdue" | "today" | "soon" | "later" | null {
  if (!due || !/^\d{4}-\d{2}-\d{2}$/.test(due)) return null
  const today = new Date()
  today.setHours(0, 0, 0, 0)
  const d = new Date(`${due}T00:00:00`)
  const days = Math.round((d.getTime() - today.getTime()) / 86400000)
  if (days < 0) return "overdue"
  if (days === 0) return "today"
  if (days <= 3) return "soon"
  return "later"
}

export function formatDue(due: string): string {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(due)) return due
  const d = new Date(`${due}T00:00:00`)
  return d.toLocaleDateString(undefined, { month: "short", day: "numeric" })
}

/** Requests made from the palette, taken when the Boards page mounts. */
export type BoardsIntent = { kind: "add-card"; board: string }
let pending: BoardsIntent | null = null
const INTENT_EVENT = "lucidbench:boards-intent"

export function requestBoards(intent: BoardsIntent) {
  pending = intent
  window.dispatchEvent(new Event(INTENT_EVENT))
}

export function takeBoardsIntent(): BoardsIntent | null {
  const p = pending
  pending = null
  return p
}

export const onBoardsIntent = (fn: () => void) => {
  window.addEventListener(INTENT_EVENT, fn)
  return () => window.removeEventListener(INTENT_EVENT, fn)
}
