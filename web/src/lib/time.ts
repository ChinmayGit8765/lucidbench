import { useEffect, useState } from "react"

/** The current time, refreshed every intervalMs so relative times stay fresh. */
export function useNow(intervalMs = 15000): number {
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), intervalMs)
    return () => clearInterval(id)
  }, [intervalMs])
  return now
}

/** "just now", "2m ago", "3h ago", "5d ago"; "-" for an unparsable time. */
export function relativeTime(iso: string, now: number): string {
  const t = new Date(iso).getTime()
  if (Number.isNaN(t)) return "-"
  const s = Math.max(0, Math.round((now - t) / 1000))
  if (s < 45) return "just now"
  const m = Math.round(s / 60)
  if (m < 60) return `${m}m ago`
  const h = Math.round(m / 60)
  if (h < 24) return `${h}h ago`
  const d = Math.round(h / 24)
  if (d < 30) return `${d}d ago`
  return new Date(t).toLocaleDateString(undefined, { day: "numeric", month: "short", year: "numeric" })
}

/** Full local date and time, for tooltips. */
export function absoluteTime(iso: string): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return ""
  return d.toLocaleString(undefined, { dateStyle: "medium", timeStyle: "medium" })
}
