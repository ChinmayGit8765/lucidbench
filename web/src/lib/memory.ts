import { getJSON, sendJSON } from "@/lib/api"

/** One row of a folder listing (GET /api/memory/tree). */
export interface Entry {
  name: string
  path: string
  dir: boolean
  size: number
  modified: string
  confidential: boolean
  title?: string
  icon?: string
}

/** A Markdown page: front matter plus body. */
export interface Page {
  path: string
  title: string
  front: Record<string, unknown>
  body: string
  modified: string
  confidential: boolean
}

export interface Hit {
  path: string
  title: string
  snippet: string
  confidential: boolean
}

export interface VaultInfo {
  root: string
  pages: number
}

/** A folder's own page, holding its icon and confidential flag. Hidden in the tree. */
export const FOLDER_FILE = "_folder.md"

const q = encodeURIComponent

export const memoryApi = {
  info: () => getJSON<VaultInfo>("/api/memory/info"),
  tree: (dir = "") => getJSON<Entry[]>(`/api/memory/tree?dir=${q(dir)}`),
  page: (path: string) => getJSON<Page>(`/api/memory/page?path=${q(path)}`),
  save: (path: string, p: { title?: string; front: Record<string, unknown>; body: string }) =>
    sendJSON<Page>(`/api/memory/page?path=${q(path)}`, "PUT", p),
  trash: (path: string) => sendJSON<null>(`/api/memory/page?path=${q(path)}`, "DELETE"),
  move: (from: string, to: string) => sendJSON<{ path: string }>("/api/memory/move", "POST", { from, to }),
  /** Brings the newest trashed copy of path back (undo for trash). */
  restore: (path: string) => sendJSON<{ path: string }>("/api/memory/restore", "POST", { path }),
  search: (text: string, limit = 20) => getJSON<Hit[]>(`/api/memory/search?q=${q(text)}&limit=${limit}`),
  backlinks: (path: string) => getJSON<string[]>(`/api/memory/backlinks?path=${q(path)}`),
}

/** "Inbox/My idea.md" → "My idea". */
export const baseName = (path: string) => (path.split("/").pop() ?? path).replace(/\.md$/i, "")

/** "Inbox/My idea.md" → "Inbox" ("" at the root). */
export const dirOf = (path: string) => (path.includes("/") ? path.slice(0, path.lastIndexOf("/")) : "")

export const joinPath = (dir: string, name: string) => (dir ? `${dir}/${name}` : name)

/** A file name from a title: no slashes or characters Windows or Obsidian refuse. */
export function safeName(title: string): string {
  const s = title.replace(/[\\/:*?"<>|#^[\]]/g, " ").replace(/\s+/g, " ").trim().replace(/^\.+/, "")
  return s.slice(0, 120) || "Untitled"
}

/** The page's URL inside the app. */
export const pageRoute = (path: string) => `/memory/${path.split("/").map(q).join("/")}`

/**
 * Requests made from outside the Memory page (the palette) wait here until
 * the page mounts and takes them; a page already open hears the event.
 */
export type MemoryIntent = { kind: "new-page" } | { kind: "search" }
let pending: MemoryIntent | null = null
const INTENT_EVENT = "lucidbench:memory-intent"

export function requestMemory(intent: MemoryIntent) {
  pending = intent
  window.dispatchEvent(new Event(INTENT_EVENT))
}

export function takeMemoryIntent(): MemoryIntent | null {
  const p = pending
  pending = null
  return p
}

export const onMemoryIntent = (fn: () => void) => {
  window.addEventListener(INTENT_EVENT, fn)
  return () => window.removeEventListener(INTENT_EVENT, fn)
}

/** Every page in the vault, walking the tree folder by folder (bounded). */
export async function allPages(max = 400): Promise<Entry[]> {
  const out: Entry[] = []
  const queue = [""]
  while (queue.length > 0 && out.length < max) {
    const dir = queue.shift()!
    let ents: Entry[]
    try {
      ents = await memoryApi.tree(dir)
    } catch {
      continue
    }
    for (const e of ents) {
      if (e.dir) {
        if (dir === "" && e.name === "Boards") continue
        queue.push(e.path)
      } else if (e.name !== FOLDER_FILE) {
        out.push(e)
      }
    }
  }
  return out.slice(0, max)
}
