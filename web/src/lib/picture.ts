import { getJSON, sendJSON, usePoll, type Polled } from "@/lib/api"
import { usePrefs } from "@/lib/prefs"

/* Mirrors internal/picture: keep these in step with the Go structs. */

export type PictureKind = "mermaid" | "excalidraw"

export interface PictureItem {
  name: string
  kind: PictureKind
  where: "repo" | "memory"
  path: string
  size: number
  modified: string
}

export interface PictureSummary {
  project: string
  name: string
  where: "repo" | "memory"
  count: number
  confidential: boolean
}

export interface PictureListing {
  project: PictureSummary
  where: "repo" | "memory"
  dir: string
  items: PictureItem[]
  can_draft: boolean
  draft_why?: string
}

export interface PictureLive {
  mermaid: string
  notes: string[]
}

export interface PictureDraft {
  mermaid: string
  provider: string
  model?: string
  usage: { cost_usd?: number; input_tokens?: number; output_tokens?: number; duration_ms: number; model?: string }
}

const q = encodeURIComponent
const base = (project: string) => `/api/picture/${q(project)}`

export const pictureApi = {
  list: (project: string) => getJSON<PictureListing>(base(project)),
  read: (project: string, name: string, kind: PictureKind) =>
    getJSON<{ content: string }>(`${base(project)}/item?name=${q(name)}&kind=${kind}`),
  target: (project: string, name: string, kind: PictureKind) =>
    getJSON<{ where: string; path: string }>(`${base(project)}/target?name=${q(name)}&kind=${kind}`),
  write: (project: string, name: string, kind: PictureKind, content: string) =>
    sendJSON<PictureItem>(`${base(project)}/item?name=${q(name)}&kind=${kind}`, "PUT", { content }),
  live: (project: string) => getJSON<PictureLive>(`${base(project)}/live`),
  draft: (project: string, provider: string, model: string) =>
    sendJSON<PictureDraft>(`${base(project)}/draft`, "POST", { provider, model }),
}

/** A picture name the daemon accepts: a-z, 0-9, - and _, starting with a letter or digit. */
export function slugName(s: string): string {
  return s
    .toLowerCase()
    .replace(/[^a-z0-9_-]+/g, "-")
    .replace(/^[-_]+|[-_]+$/g, "")
    .slice(0, 63)
}

/** Whether the Picture extension is in the sidebar; its hooks stay quiet until it is. */
export function usePictureAdded(): boolean {
  const { prefs } = usePrefs()
  return prefs.extensions["picture"]?.added ?? false
}

/** Every project with where its pictures live and how many there are. */
export function usePictureSummary(enabled: boolean): Polled<PictureSummary[]> {
  return usePoll<PictureSummary[]>(enabled ? "/api/picture" : null, 30000)
}
