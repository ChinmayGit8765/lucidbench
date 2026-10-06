import { getJSON, sendJSON } from "@/lib/api"
import type { Card } from "@/lib/boards"

export const TRELLO_POLL_MS = 60000

/** GET /api/trello/status. Never the key or the token. */
export interface TrelloStatus {
  configured: boolean
  key_set: boolean
  token_set: boolean
  key_ref: string
  token_ref: string
}

export interface TrelloBoard {
  id: string
  name: string
  url: string
  last_activity?: string
}

export interface TrelloList {
  id: string
  name: string
  pos: number
}

export interface TrelloCard {
  id: string
  name: string
  desc?: string
  list_id: string
  url: string
  due?: string
  pos: number
  labels: { name: string; color?: string }[]
}

export interface TrelloBoardView {
  board: TrelloBoard
  lists: TrelloList[]
  cards: TrelloCard[]
}

export const trelloApi = {
  status: () => getJSON<TrelloStatus>("/api/trello/status"),
  boards: () => getJSON<TrelloBoard[]>("/api/trello/boards"),
  board: (id: string) => getJSON<TrelloBoardView>(`/api/trello/boards/${encodeURIComponent(id)}`),
  add: (list: string, name: string) => sendJSON<TrelloCard>("/api/trello/cards", "POST", { list, name }),
  move: (id: string, list: string) => sendJSON<TrelloCard>(`/api/trello/cards/${encodeURIComponent(id)}`, "PUT", { list }),
  link: (board: string, card: string, trello: string) =>
    sendJSON<{ card: Card; remote: TrelloCard }>("/api/trello/link", "POST", { board, card, trello }),
}

/** Trello's label colours, softened to sit with the app's palette. */
const LABEL_COLORS: Record<string, string> = {
  green: "oklch(0.68 0.14 155)",
  yellow: "oklch(0.78 0.14 90)",
  orange: "oklch(0.72 0.15 55)",
  red: "oklch(0.64 0.18 25)",
  purple: "oklch(0.64 0.15 305)",
  blue: "oklch(0.64 0.14 250)",
  sky: "oklch(0.74 0.11 225)",
  lime: "oklch(0.78 0.15 125)",
  pink: "oklch(0.7 0.15 350)",
  black: "oklch(0.5 0.01 260)",
}

export const trelloLabelColor = (c?: string) => (c && LABEL_COLORS[c.replace(/_(dark|light)$/, "")]) || "oklch(0.62 0.02 260)"
