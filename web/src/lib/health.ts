import { useEffect, useState } from "react"

export interface Health {
  status: string
  version: string
  /** Whether the docker engine answers; only present when asked for (withDocker). */
  docker?: boolean
  /** When this engine process started; it changes across a restart. */
  started?: string
}

/** The mounted health polls, so a Retry can ask now instead of waiting a tick. */
const checkers = new Set<() => Promise<void>>()

/** Asks every health poll again now; resolves once they have answered. */
export function recheckHealth(): Promise<void> {
  return Promise.all([...checkers].map((c) => c())).then(() => undefined)
}

/**
 * Polls the daemon's /api/health; null means unreachable, undefined not
 * checked yet. withDocker also asks whether the docker engine is up (the
 * daemon caches that), so the shell learns it without calling a docker
 * endpoint that would fail.
 */
export function useHealth(intervalMs = 10000, withDocker = false, enabled = true): Health | null | undefined {
  const [health, setHealth] = useState<Health | null | undefined>(undefined)
  useEffect(() => {
    if (!enabled) return
    let cancelled = false
    const poll = async () => {
      try {
        const res = await fetch(withDocker ? "/api/health?docker=1" : "/api/health")
        if (!res.ok) throw new Error(res.statusText)
        const body = (await res.json()) as Health
        if (!cancelled) setHealth(body)
      } catch {
        if (!cancelled) setHealth(null)
      }
    }
    void poll()
    const id = setInterval(poll, intervalMs)
    checkers.add(poll)
    return () => {
      cancelled = true
      clearInterval(id)
      checkers.delete(poll)
    }
  }, [intervalMs, withDocker, enabled])
  return health
}
