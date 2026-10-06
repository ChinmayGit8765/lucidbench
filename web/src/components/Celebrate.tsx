import { useEffect, useRef, useState } from "react"

import { StateSprite } from "@/components/StateSprite"
import { usePoll } from "@/lib/api"
import { useWorkBoard } from "@/lib/boards"
import { sessionsPath, WORK_POLL_MS, type WorkSession } from "@/lib/work"

/*
 * Celebrations: a card reaching Done, a PR merging. Each thing is celebrated
 * once, ever (the ids are remembered in localStorage), so a poll that sees
 * the same merged PR again stays quiet, and so does the first load of things
 * that finished while the app was closed.
 */

const EVENT = "lucidbench:celebrate"
const SEEN_KEY = "lucidbench.celebrated"
const MAX_SEEN = 400

export interface Celebration {
  /** Unique per thing: "card:<id>", "pr:<session id>". */
  key: string
  title: string
  detail?: string
}

function seen(): string[] {
  try {
    return JSON.parse(localStorage.getItem(SEEN_KEY) ?? "[]") as string[]
  } catch {
    return []
  }
}

function remember(keys: string[]) {
  try {
    const all = [...new Set([...seen(), ...keys])].slice(-MAX_SEEN)
    localStorage.setItem(SEEN_KEY, JSON.stringify(all))
  } catch {
    /* not remembered: at worst a second celebration */
  }
}

/** Everything this page has celebrated or found already finished. */
const known = new Set<string>()

/** Celebrates something now, unless it was celebrated before. */
export function celebrate(c: Celebration) {
  if (known.has(c.key) || seen().includes(c.key)) return
  known.add(c.key)
  remember([c.key])
  window.dispatchEvent(new CustomEvent<Celebration>(EVENT, { detail: c }))
}

/** Things that were already finished when first seen: never celebrated. */
export function markCelebrated(keys: string[]) {
  for (const k of keys) known.add(k)
}

const COLORS = ["var(--brand)", "var(--brand-2)", "var(--success)", "var(--warning)", "var(--info)", "var(--danger)"]

const reducedMotion = () => matchMedia("(prefers-reduced-motion: reduce)").matches

/**
 * The overlay: confetti and the theme's celebrate sprite (or Lumi) with a
 * line of text, for about three seconds. Clicks pass through it.
 */
export function Celebrations() {
  const [show, setShow] = useState<(Celebration & { n: number }) | null>(null)
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null)
  useEffect(() => {
    let n = 0
    const on = (e: Event) => {
      const c = (e as CustomEvent<Celebration>).detail
      setShow({ ...c, n: ++n })
      if (timer.current) clearTimeout(timer.current)
      timer.current = setTimeout(() => setShow(null), 3400)
    }
    window.addEventListener(EVENT, on)
    return () => {
      window.removeEventListener(EVENT, on)
      if (timer.current) clearTimeout(timer.current)
    }
  }, [])
  if (!show) return null
  const calm = reducedMotion()
  return (
    <div key={show.n} aria-live="polite" data-testid="celebration" className="pointer-events-none fixed inset-0 z-[60] overflow-hidden">
      {!calm &&
        Array.from({ length: 46 }, (_, i) => (
          <span
            key={i}
            className="lb-confetti"
            style={
              {
                left: `${(i * 37) % 100}%`,
                background: COLORS[i % COLORS.length],
                "--d": `${(i % 9) * 0.07}s`,
                "--t": `${2 + ((i * 13) % 10) / 10}s`,
                "--dx": `${((i * 29) % 21) - 10}vw`,
                "--spin": `${360 + ((i * 47) % 360)}deg`,
              } as React.CSSProperties
            }
          />
        ))}
      <div className="absolute inset-x-0 bottom-16 flex justify-center">
        <div className="relative flex items-center gap-3 rounded-2xl border border-border-strong bg-elevated/95 py-2.5 pl-2.5 pr-5 shadow-pop backdrop-blur animate-in fade-in-0 zoom-in-95 slide-in-from-bottom-4 duration-300">
          <span aria-hidden className="lb-glow absolute -inset-6 -z-10 rounded-full" />
          <StateSprite state="celebrate" className="size-12" />
          <div>
            <div className="text-sm font-semibold">{show.title}</div>
            {show.detail && <div className="max-w-xs truncate text-xs text-muted-foreground">{show.detail}</div>}
          </div>
        </div>
      </div>
    </div>
  )
}

/**
 * Watches the work board and Work's sessions and celebrates what finished
 * since the last look: a card that reached Done, a PR seen merged. What was
 * already finished at the first look is remembered, not celebrated.
 */
export function CelebrationWatcher() {
  const wb = useWorkBoard()
  const sessions = usePoll<WorkSession[]>(sessionsPath, WORK_POLL_MS * 2)
  const primed = useRef({ cards: false, prs: false })

  useEffect(() => {
    const cards = wb.board?.cards
    if (!cards) return
    const done = cards.filter((c) => c.column === "Done" || c.done)
    if (!primed.current.cards) {
      primed.current.cards = true
      markCelebrated(done.map((c) => `card:${c.id}`))
      return
    }
    for (const c of done) celebrate({ key: `card:${c.id}`, title: "Done!", detail: c.title })
  }, [wb.board])

  useEffect(() => {
    const list = sessions.data
    if (!list) return
    const merged = list.filter((s) => s.pr_state === "merged")
    if (!primed.current.prs) {
      primed.current.prs = true
      markCelebrated(merged.map((s) => `pr:${s.id}`))
      return
    }
    for (const s of merged) celebrate({ key: `pr:${s.id}`, title: "PR merged", detail: s.title || s.branch })
  }, [sessions.data])

  return null
}
