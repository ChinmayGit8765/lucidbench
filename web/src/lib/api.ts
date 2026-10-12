import { useCallback, useEffect, useRef, useState } from "react"

/** An API failure; status is 0 when the daemon could not be reached. */
export class ApiError extends Error {
  status: number
  constructor(status: number, message: string) {
    super(message)
    this.status = status
  }
}

export async function request(path: string, init?: RequestInit): Promise<Response> {
  // A change may alter any answer: drop the shared ones.
  if (init?.method && init.method !== "GET" && init.method !== "HEAD") recent.clear()
  let res: Response
  try {
    res = await fetch(path, init)
  } catch {
    throw new ApiError(0, "Cannot reach the Lucidbench daemon.")
  }
  if (!res.ok) {
    const text = (await res.text()).trim()
    throw new ApiError(res.status, text || res.statusText)
  }
  return res
}

const inflight = new Map<string, Promise<unknown>>()

/** Answers younger than this are shared too: the shell and the page mount a moment apart. */
const SHARE_MS = 1500
const recent = new Map<string, { at: number; body: unknown }>()

/**
 * GETs JSON. Calls for the same path share one request while it is in
 * flight and for a moment after, so the shell, a Home tile and the page
 * around it reading the same endpoint cost one round trip. Any change sent
 * through this module (and refreshAll) forgets the shared answers.
 */
export function getJSON<T>(path: string): Promise<T> {
  const r = recent.get(path)
  if (r && Date.now() - r.at < SHARE_MS) return Promise.resolve(r.body as T)
  let p = inflight.get(path) as Promise<T> | undefined
  if (!p) {
    p = request(path).then((r) => r.json() as Promise<T>)
    inflight.set(path, p)
    void p.then(
      (body) => {
        inflight.delete(path)
        recent.set(path, { at: Date.now(), body })
      },
      () => inflight.delete(path),
    )
  }
  return p
}

/** Sends a JSON body with the confirm header (any change on this machine). */
export async function sendJSON<T>(path: string, method: "POST" | "PUT" | "DELETE", body?: unknown): Promise<T> {
  const res = await request(path, {
    method,
    headers: { "Content-Type": "application/json", "X-Lucid-Confirm": "yes" },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  const text = await res.text()
  return (text ? JSON.parse(text) : null) as T
}

/**
 * POSTs to an action endpoint. Actions that change things on the machine
 * (containers, CI) require the X-Lucid-Confirm header, which only this UI
 * sends; see docs/CONFIG.md.
 */
export async function postAction<T>(path: string): Promise<T> {
  return (await request(path, { method: "POST", headers: { "X-Lucid-Confirm": "yes" } })).json() as Promise<T>
}

/** The message of any thrown value. */
export const errorMessage = (e: unknown) => (e instanceof Error ? e.message : String(e))

/** What the docker CLI says, in one form or another, when no engine answers. */
const DOCKER_DOWN = /cannot connect to the docker daemon|is the docker daemon running|docker is running\?|error during connect|docker_engine|docker\.sock|docker daemon is not running|docker engine unreachable|docker unavailable|docker (ps|stats|version|info)[^:]*: exit status \d+|command "docker [^"]*" failed with error: exit status \d+/i

/** Whether an error message means the docker engine is not running. */
export const isDockerDown = (message: string | undefined | null) => !!message && DOCKER_DOWN.test(message)

/** The sentence shown instead of a raw "docker ps … exit status 1". */
export const DOCKER_DOWN_TEXT = "Docker is not running. Start Docker Desktop (or the docker service) and this fills in by itself."

/**
 * An error message as a person should read it: the docker CLI's many ways
 * of saying "no engine" become one plain sentence; anything else is kept.
 */
export function plainError(message: string): string
export function plainError(message: string | undefined): string | undefined
export function plainError(message: string | undefined): string | undefined {
  return isDockerDown(message) ? DOCKER_DOWN_TEXT : message
}

const REFRESH_EVENT = "lucidbench:refresh"

/** The last good answer per path, for polls that opt into the cache. */
const lastGood = new Map<string, { data: unknown; at: number }>()

/** Asks every mounted poll to reload now (the header refresh button, ⌘K). */
export function refreshAll() {
  recent.clear()
  window.dispatchEvent(new Event(REFRESH_EVENT))
}

export interface Polled<T> {
  data: T | null
  error: ApiError | null
  loading: boolean
  /** A load is in flight (first load or refresh). */
  refreshing: boolean
  /** When the last successful load finished (ms epoch). */
  updatedAt: number | null
  refresh: () => void
}

/**
 * False until ms after mount: for the shell's cosmetic reads (the mascot's
 * power state, celebrations, the update banner), so they wait until the
 * page has had its turn instead of competing with it.
 */
export function useAfterSettle(ms = 4000): boolean {
  const [on, setOn] = useState(false)
  useEffect(() => {
    const t = setTimeout(() => setOn(true), ms)
    return () => clearTimeout(t)
  }, [ms])
  return on
}

/**
 * Fetches JSON now and every intervalMs; keeps the last good data on errors.
 * Polling pauses while the tab is hidden and catches up when it returns.
 * A null path fetches nothing (a module that is not added stays quiet).
 *
 * With cache, the last good answer for the path (from any component) shows
 * at once, and a mount within intervalMs of it fetches nothing until the
 * next tick: for slow, shell-wide reads such as accounts and power, so a
 * page change neither waits on them nor asks again.
 */
export function usePoll<T>(path: string | null, intervalMs = 10000, opts?: { cache?: boolean }): Polled<T> {
  const cached = opts?.cache && path !== null ? (lastGood.get(path) as { data: T; at: number } | undefined) : undefined
  const [data, setData] = useState<T | null>(cached?.data ?? null)
  const [error, setError] = useState<ApiError | null>(null)
  const [loading, setLoading] = useState(path !== null && !cached)
  const [refreshing, setRefreshing] = useState(false)
  const [updatedAt, setUpdatedAt] = useState<number | null>(cached?.at ?? null)
  const alive = useRef(true)
  const inflight = useRef<string | null>(null)
  const fresh = useRef(!!cached && Date.now() - cached.at < intervalMs)
  const cache = !!opts?.cache

  const load = useCallback(async () => {
    if (path === null || inflight.current === path) return
    inflight.current = path
    setRefreshing(true)
    try {
      const body = await getJSON<T>(path)
      if (!alive.current) return
      if (cache) lastGood.set(path, { data: body, at: Date.now() })
      setData(body)
      setError(null)
      setUpdatedAt(Date.now())
    } catch (e) {
      if (!alive.current) return
      setError(e instanceof ApiError ? e : new ApiError(0, String(e)))
    } finally {
      if (inflight.current === path) inflight.current = null
      if (alive.current) {
        setLoading(false)
        setRefreshing(false)
      }
    }
  }, [path, cache])

  useEffect(() => {
    alive.current = true
    if (fresh.current) fresh.current = false
    else void load()
    const id = setInterval(() => {
      if (document.visibilityState !== "hidden") void load()
    }, intervalMs)
    const onVisible = () => document.visibilityState === "visible" && void load()
    const onRefresh = () => void load()
    document.addEventListener("visibilitychange", onVisible)
    window.addEventListener(REFRESH_EVENT, onRefresh)
    return () => {
      alive.current = false
      clearInterval(id)
      document.removeEventListener("visibilitychange", onVisible)
      window.removeEventListener(REFRESH_EVENT, onRefresh)
    }
  }, [load, intervalMs])

  return { data, error, loading, refreshing, updatedAt, refresh: () => void load() }
}
