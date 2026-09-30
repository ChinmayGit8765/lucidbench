import { useEffect, useState } from "react"

export interface Health {
  status: string
  version: string
}

/** Polls the daemon's /api/health; null means unreachable. */
export function useHealth(intervalMs = 10000): Health | null {
  const [health, setHealth] = useState<Health | null>(null)
  useEffect(() => {
    let cancelled = false
    const poll = async () => {
      try {
        const res = await fetch("/api/health")
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
  }, [intervalMs])
  return health
}
