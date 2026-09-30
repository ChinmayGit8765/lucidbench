import { useCallback, useEffect, useState } from "react"

export type ThemePref = "dark" | "light" | "system"
export type ResolvedTheme = "dark" | "light"

const PREFS: ThemePref[] = ["dark", "light", "system"]
const USER_KEY = "lucidbench.theme"
const CONFIG_KEY = "lucidbench.theme.config"
const DARK_QUERY = "(prefers-color-scheme: dark)"

const isPref = (v: unknown): v is ThemePref => PREFS.includes(v as ThemePref)

function read(key: string): ThemePref | null {
  try {
    const v = localStorage.getItem(key)
    return isPref(v) ? v : null
  } catch {
    return null
  }
}

function write(key: string, v: ThemePref) {
  try {
    localStorage.setItem(key, v)
  } catch {
    // Storage can be disabled; the choice then lasts for this tab only.
  }
}

/** A ?theme= URL parameter forces a theme for this page view without saving it. */
function forced(): ThemePref | null {
  const v = new URLSearchParams(location.search).get("theme")
  return isPref(v) ? v : null
}

function resolve(pref: ThemePref): ResolvedTheme {
  if (pref !== "system") return pref
  return matchMedia(DARK_QUERY).matches ? "dark" : "light"
}

/**
 * Theme preference. Order: ?theme= > the user's saved toggle > ui.theme from
 * /api/config > dark. index.html applies the same order before first paint.
 */
export function useTheme() {
  const [pref, setPrefState] = useState<ThemePref>(
    () => forced() ?? read(USER_KEY) ?? read(CONFIG_KEY) ?? "dark",
  )
  const [resolved, setResolved] = useState<ResolvedTheme>(() => resolve(pref))

  // Adopt ui.theme from the daemon unless the user or the URL chose already.
  useEffect(() => {
    if (forced() || read(USER_KEY)) return
    let cancelled = false
    fetch("/api/config")
      .then((r) => (r.ok ? r.json() : null))
      .then((body: { ui?: { theme?: string } } | null) => {
        const t = body?.ui?.theme
        if (cancelled || !isPref(t)) return
        write(CONFIG_KEY, t)
        setPrefState(t)
      })
      .catch(() => {})
    return () => {
      cancelled = true
    }
  }, [])

  useEffect(() => {
    const apply = () => {
      const r = resolve(pref)
      setResolved(r)
      document.documentElement.classList.toggle("dark", r === "dark")
    }
    apply()
    if (pref !== "system") return
    const mq = matchMedia(DARK_QUERY)
    mq.addEventListener("change", apply)
    return () => mq.removeEventListener("change", apply)
  }, [pref])

  const setPref = useCallback((p: ThemePref) => {
    write(USER_KEY, p)
    setPrefState(p)
  }, [])

  return { pref, resolved, setPref }
}
