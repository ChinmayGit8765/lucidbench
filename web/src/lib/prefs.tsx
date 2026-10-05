import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from "react"
import { toast } from "sonner"

import { errorMessage, getJSON, request } from "@/lib/api"
import {
  applyAppearance,
  resolveThemeId,
  systemBase,
  SYSTEM_THEME,
  type Base,
  type Overrides,
  type Theme,
  type ThemeBundle,
} from "@/lib/theme"

/** The content of <data dir>/ui.json (see internal/prefs). */
export interface Prefs {
  theme: string
  overrides: Overrides
  modules: Record<string, { order: number }>
  extensions: Record<string, { added: boolean; order: number }>
  sprite_board: boolean
}

const DEFAULT_PREFS: Prefs = { theme: "midnight", overrides: {}, modules: {}, extensions: {}, sprite_board: false }
const CACHE_KEY = "lucidbench.prefs"

function cachedPrefs(): Prefs {
  try {
    const raw = localStorage.getItem(CACHE_KEY)
    if (raw) return { ...DEFAULT_PREFS, ...(JSON.parse(raw) as Partial<Prefs>) }
  } catch {
    // fall through
  }
  return DEFAULT_PREFS
}

/** ?theme= forces a theme (a preset or user theme id, or dark/light/system) for this page view only. */
function forcedTheme(): string | null {
  const v = new URLSearchParams(location.search).get("theme")
  return v && /^[a-z0-9][a-z0-9-]{0,47}$/.test(v) ? v : null
}

interface PrefsValue {
  prefs: Prefs
  /** True once ui.json has been read from the daemon. */
  loaded: boolean
  update: (fn: (p: Prefs) => Prefs) => void
  themes: Theme[]
  themesLoaded: boolean
  reloadThemes: () => Promise<void>
  /** The theme on screen: a preview, a ?theme= override, or the saved choice. */
  active: Theme | undefined
  base: Base
  /** An unsaved generated or imported theme being tried on. */
  preview: ThemeBundle | null
  setPreview: (b: ThemeBundle | null) => void
  /** Inline assets for a theme that is not saved yet. */
  inlineAssets: (t: Theme) => Record<string, string> | undefined
  /** A theme's playful rename of a UI string, or the fallback. */
  label: (key: string, fallback: string) => string
}

const PrefsContext = createContext<PrefsValue | null>(null)

export function usePrefs(): PrefsValue {
  const v = useContext(PrefsContext)
  if (!v) throw new Error("usePrefs outside PrefsProvider")
  return v
}

export function PrefsProvider({ children }: { children: ReactNode }) {
  const [prefs, setPrefs] = useState<Prefs>(cachedPrefs)
  const [loaded, setLoaded] = useState(false)
  const [themes, setThemes] = useState<Theme[]>([])
  const [themesLoaded, setThemesLoaded] = useState(false)
  const [preview, setPreview] = useState<ThemeBundle | null>(null)
  const [forced, setForced] = useState<string | null>(forcedTheme)
  const [osBase, setOsBase] = useState<Base>(systemBase)
  const saveTimer = useRef<ReturnType<typeof setTimeout> | null>(null)
  const latest = useRef(prefs)

  useEffect(() => {
    getJSON<Prefs>("/api/prefs")
      .then((p) => {
        const next = { ...DEFAULT_PREFS, ...p, overrides: p.overrides ?? {} }
        latest.current = next
        setPrefs(next)
      })
      .catch(() => undefined)
      .finally(() => setLoaded(true))
  }, [])

  const reloadThemes = useCallback(async () => {
    try {
      const l = await getJSON<{ themes: Theme[] }>("/api/themes")
      setThemes(l.themes)
    } catch {
      // Presets still apply from the cache; the gallery shows the error.
    } finally {
      setThemesLoaded(true)
    }
  }, [])
  useEffect(() => void reloadThemes(), [reloadThemes])

  useEffect(() => {
    const mq = matchMedia("(prefers-color-scheme: dark)")
    const on = () => setOsBase(mq.matches ? "dark" : "light")
    mq.addEventListener("change", on)
    return () => mq.removeEventListener("change", on)
  }, [])

  const update = useCallback((fn: (p: Prefs) => Prefs) => {
    const next = fn(latest.current)
    latest.current = next
    setPrefs(next)
    setForced(null)
    try {
      localStorage.setItem(CACHE_KEY, JSON.stringify(next))
    } catch {
      // not cached
    }
    if (saveTimer.current) clearTimeout(saveTimer.current)
    saveTimer.current = setTimeout(() => {
      request("/api/prefs", {
        method: "PUT",
        headers: { "Content-Type": "application/json", "X-Lucid-Confirm": "yes" },
        body: JSON.stringify(latest.current),
      }).catch((e) => toast.error("Could not save your settings", { description: errorMessage(e) }))
    }, 350)
  }, [])

  // "system" re-resolves on every render, and osBase re-renders when the OS flips.
  const chosen = preview?.theme.id ?? resolveThemeId(forced ?? prefs.theme)
  const active = preview?.theme ?? themes.find((t) => t.id === chosen)
  const base: Base = active?.base ?? ((forced ?? prefs.theme) === SYSTEM_THEME ? osBase : chosen === "daylight" ? "light" : "dark")

  useEffect(() => {
    // Until the theme list arrives, keep whatever index.html painted.
    if (!active && !themesLoaded) return
    applyAppearance(active, base, prefs.overrides)
  }, [active, base, prefs.overrides, themesLoaded])

  const inlineAssets = useCallback(
    (t: Theme) => (preview && preview.theme.id === t.id ? (preview.assets ?? {}) : undefined),
    [preview],
  )
  const label = useCallback((key: string, fallback: string) => active?.labels?.[key] || fallback, [active])

  const value = useMemo<PrefsValue>(
    () => ({ prefs, loaded, update, themes, themesLoaded, reloadThemes, active, base, preview, setPreview, inlineAssets, label }),
    [prefs, loaded, update, themes, themesLoaded, reloadThemes, active, base, preview, inlineAssets, label],
  )
  return <PrefsContext.Provider value={value}>{children}</PrefsContext.Provider>
}
