import { sendJSON, usePoll, type Polled } from "@/lib/api"
import { usePrefs } from "@/lib/prefs"

/* Mirrors internal/browser: keep these in step with the Go structs. */

export interface BrowserTab {
  id: string
  title: string
  url: string
}

export interface BrowserStatus {
  state: "sleeping" | "starting" | "running"
  image: string
  idle_minutes: number
  version?: string
  /** The browser-level DevTools address on 127.0.0.1, what an attached Work session gets. */
  cdp?: string
  tabs: BrowserTab[]
  /** A running Work session keeps it awake. */
  held?: boolean
  note?: string
}

export interface BrowserFrame {
  jpeg: string
  /** The page's size in CSS pixels; pointer positions go back in them. */
  width: number
  height: number
}

export type BrowserInput =
  | { type: "move" | "down" | "up" | "click"; x: number; y: number; button?: "left" | "middle" | "right"; modifiers?: number }
  | { type: "scroll"; x: number; y: number; dx: number; dy: number }
  | { type: "key"; key: string; code?: string; modifiers?: number; action?: "down" | "up" | "press" }
  | { type: "text"; text: string }

export interface BrowserShot {
  target: string
  url: string
  title: string
  png: string
  bytes: number
}

/** Matches Containers: each read costs a few docker calls. */
export const BROWSER_POLL_MS = 10000

/** While it starts, a quick look shows it as soon as it is up. */
export const BROWSER_STARTING_POLL_MS = 2000

/** Whether the Live browser extension is in the sidebar; its hooks stay quiet until it is. */
export function useBrowserAdded(): boolean {
  const { prefs } = usePrefs()
  return prefs.extensions["browser"]?.added ?? false
}

/** The agent browser's state and tabs. Reading it never wakes it or keeps it awake. */
export function useBrowser(enabled = true, fast = false): Polled<BrowserStatus> {
  return usePoll<BrowserStatus>(enabled ? "/api/browser" : null, fast ? BROWSER_STARTING_POLL_MS : BROWSER_POLL_MS)
}

export const startBrowser = () => sendJSON<BrowserStatus>("/api/browser/start", "POST")
export const stopBrowser = () => sendJSON<BrowserStatus>("/api/browser/stop", "POST")

export interface NavigateRequest {
  target?: string
  url?: string
  action?: "go" | "back" | "forward" | "reload" | "close" | "new"
  new_tab?: boolean
}

export const navigateBrowser = (req: NavigateRequest) => sendJSON<{ target: string; url?: string; error_text?: string }>("/api/browser/navigate", "POST", req)

/** Input is only sent while the user has switched take-over on. */
export const sendInput = (target: string | undefined, input: BrowserInput) =>
  sendJSON<{ ok: boolean }>("/api/browser/input", "POST", { takeover: true, target, ...input })

export const takeScreenshot = (target?: string) => sendJSON<BrowserShot>("/api/browser/screenshot", "POST", { target })

/**
 * What a person typed in the address bar, as a URL. Only http and https are
 * ever opened (the daemon refuses the rest); a bare host gets https, and a
 * bare localhost or port gets http because dev servers rarely have TLS.
 */
export function normalizeUrl(raw: string): string {
  const s = raw.trim()
  if (!s) return s
  if (/^[a-z][a-z0-9+.-]*:\/\//i.test(s) || /^(about|chrome|devtools|file|data|javascript|view-source):/i.test(s)) return s
  if (/^:?\d{2,5}(\/.*)?$/.test(s)) return `http://localhost${s.startsWith(":") ? "" : ":"}${s}`
  if (/^(localhost|127\.0\.0\.1|\[::1\]|0\.0\.0\.0)(:\d+)?(\/|$)/i.test(s)) return `http://${s}`
  return `https://${s}`
}

/** A localhost address opens as host.docker.internal inside the container; the bar shows what the user meant. */
export const showUrl = (u: string) => u.replace(/^(https?:\/\/)host\.docker\.internal(?=[:/]|$)/i, "$1localhost")

/** The CDP modifier bit set of a keyboard or pointer event. */
export const modifiersOf = (e: { altKey: boolean; ctrlKey: boolean; metaKey: boolean; shiftKey: boolean }) =>
  (e.altKey ? 1 : 0) | (e.ctrlKey ? 2 : 0) | (e.metaKey ? 4 : 0) | (e.shiftKey ? 8 : 0)
