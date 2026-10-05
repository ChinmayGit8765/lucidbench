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

/**
 * GETs JSON. Concurrent calls for the same path share one request, so an
 * Overview tile and the page around it polling the same endpoint cost one
 * round trip.
 */
export function getJSON<T>(path: string): Promise<T> {
  let p = inflight.get(path) as Promise<T> | undefined
  if (!p) {
    p = request(path).then((r) => r.json() as Promise<T>)
    inflight.set(path, p)
    void p.then(
      () => inflight.delete(path),
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

const REFRESH_EVENT = "lucidbench:refresh"

/** Asks every mounted poll to reload now (the header refresh button, ⌘K). */
export function refreshAll() {
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
 * Fetches JSON now and every intervalMs; keeps the last good data on errors.
 * Polling pauses while the tab is hidden and catches up when it returns.
 */
export function usePoll<T>(path: string, intervalMs = 10000): Polled<T> {
  const [data, setData] = useState<T | null>(null)
  const [error, setError] = useState<ApiError | null>(null)
  const [loading, setLoading] = useState(true)
  const [refreshing, setRefreshing] = useState(false)
  const [updatedAt, setUpdatedAt] = useState<number | null>(null)
  const alive = useRef(true)
  const inflight = useRef<string | null>(null)

  const load = useCallback(async () => {
    if (inflight.current === path) return
    inflight.current = path
    setRefreshing(true)
    try {
      const body = await getJSON<T>(path)
      if (!alive.current) return
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
  }, [path])

  useEffect(() => {
    alive.current = true
    void load()
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
