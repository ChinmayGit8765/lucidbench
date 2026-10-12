import { useEffect, useState } from "react"

export interface Health {
  status: string
  version: string
  /** Whether the docker engine answers; only present when asked for (withDocker). */
  docker?: boolean
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
    return () => {
      cancelled = true
      clearInterval(id)
    }
  }, [intervalMs, withDocker, enabled])
  return health
}
