/*
 * The theme engine. A theme is a set of CSS variable values (the design
 * tokens in index.css) on a dark or light base, plus optional fonts, art and
 * label renames. The active theme and the user's overrides are written as
 * inline variables on <html>, so every component picks them up with no
 * re-render. index.html replays the last applied set before first paint.
 */

export type Base = "dark" | "light"

export interface Sprite {
  file: string
  caption?: string
}

export interface ThemeArt {
  headerImage?: string
  sidebarMascot?: string
  emptyState?: string
  spriteBoard?: Sprite[]
  /** State sprites by slot: loading, working, thinking, success, failure, sleeping, empty, celebrate. */
  sprites?: Partial<Record<string, string>>
}

export interface Theme {
  id: string
  name: string
  version: number
  base: Base
  description?: string
  tokens: Record<string, string>
  fonts?: { sans?: string; mono?: string }
  art?: ThemeArt
  labels?: Record<string, string>
  builtin: boolean
}

/** A theme with its assets inline: the save, export and preview format. */
export interface ThemeBundle {
  theme: Theme
  assets?: Record<string, string>
}

export type Density = "compact" | "comfortable"
export type FontChoice = "geist" | "inter" | "system" | "mono"
export type SidebarMode = "expanded" | "rail"

export interface Overrides {
  accent?: string
  density?: Density
  radius?: number
  font?: FontChoice
  sidebar?: SidebarMode
}

export const FONTS: { id: FontChoice; label: string; stack?: string; hint?: string }[] = [
  { id: "geist", label: "Geist" },
  { id: "inter", label: "Inter", stack: '"Inter Variable", "Inter", ui-sans-serif, system-ui, sans-serif', hint: "Uses Inter when it is installed on this machine" },
  { id: "system", label: "System", stack: 'ui-sans-serif, system-ui, -apple-system, "Segoe UI", sans-serif' },
  { id: "mono", label: "Mono", stack: 'var(--font-mono)' },
]

/** "system" follows the OS between these two presets. */
export const SYSTEM_THEME = "system"
export const DARK_DEFAULT = "midnight"
export const LIGHT_DEFAULT = "daylight"

const CACHE_KEY = "lucidbench.appearance"
const DARK_QUERY = "(prefers-color-scheme: dark)"

export const systemBase = (): Base => (matchMedia(DARK_QUERY).matches ? "dark" : "light")

/** The theme id that "system" (or a legacy dark/light value) stands for right now. */
export function resolveThemeId(id: string): string {
  if (id === "dark") return DARK_DEFAULT
  if (id === "light") return LIGHT_DEFAULT
  if (id === SYSTEM_THEME) return systemBase() === "dark" ? DARK_DEFAULT : LIGHT_DEFAULT
  return id
}

/** sRGB relative luminance of any CSS colour, or null if it does not parse. */
function luminance(color: string): number | null {
  const c = document.createElement("canvas")
  c.width = c.height = 1
  const ctx = c.getContext("2d", { willReadFrequently: true })
  if (!ctx) return null
  ctx.fillStyle = "#000"
  ctx.fillStyle = color
  ctx.fillRect(0, 0, 1, 1)
  const [r, g, b] = ctx.getImageData(0, 0, 1, 1).data
  const lin = (v: number) => {
    const s = v / 255
    return s <= 0.04045 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4
  }
  return 0.2126 * lin(r) + 0.7152 * lin(g) + 0.0722 * lin(b)
}

/** The variables an accent colour sets: brand, primary, ring and their tints. */
export function accentVars(accent: string): Record<string, string> {
  const l = luminance(accent)
  const onAccent = l !== null && l > 0.4 ? "oklch(0.18 0.02 260)" : "oklch(0.99 0 0)"
  return {
    "--brand": accent,
    "--primary": accent,
    "--ring": accent,
    "--primary-foreground": onAccent,
    "--brand-soft": `color-mix(in oklch, ${accent} 13%, transparent)`,
    "--brand-fg": `color-mix(in oklch, ${accent} 78%, var(--foreground))`,
    "--glow-1": `color-mix(in oklch, ${accent} 9%, transparent)`,
  }
}

/** Every variable a theme plus overrides puts on <html>. */
export function themeVars(theme: Theme | undefined, o: Overrides): Record<string, string> {
  const vars: Record<string, string> = { ...(theme?.tokens ?? {}) }
  if (theme?.fonts?.sans) vars["--font-sans"] = theme.fonts.sans
  if (theme?.fonts?.mono) vars["--font-mono"] = theme.fonts.mono
  const font = FONTS.find((f) => f.id === o.font)
  if (font?.stack) vars["--font-sans"] = font.stack
  if (o.accent) Object.assign(vars, accentVars(o.accent))
  if (o.radius !== undefined) vars["--radius"] = `${o.radius}px`
  return vars
}

/** Applies a theme and overrides to the document and caches them for the next first paint. */
export function applyAppearance(theme: Theme | undefined, base: Base, o: Overrides) {
  const root = document.documentElement
  root.classList.toggle("dark", base === "dark")
  // Clear every inline variable, including those index.html replayed.
  for (const k of [...root.style].filter((p) => p.startsWith("--"))) root.style.removeProperty(k)
  const vars = themeVars(theme, o)
  for (const [k, v] of Object.entries(vars)) root.style.setProperty(k, v)
  const density = o.density === "comfortable" ? "comfortable" : "compact"
  root.dataset.density = density
  try {
    localStorage.setItem(CACHE_KEY, JSON.stringify({ dark: base === "dark", vars, density }))
  } catch {
    // Not cached; the next load paints the default theme first.
  }
}

/** Inline style that renders a subtree in a given theme (thumbnails, previews). */
export function scopedStyle(theme: Theme, o: Overrides = {}): Record<string, string> {
  return themeVars(theme, o)
}

/** URL of a theme's art file. Unsaved previews carry their assets inline. */
export function assetURL(theme: Theme, file: string, inline?: Record<string, string>): string {
  const data = inline?.[file]
  if (data !== undefined) {
    if (file.endsWith(".svg")) return `data:image/svg+xml;charset=utf-8,${encodeURIComponent(data)}`
    const type = file.endsWith(".png") ? "png" : file.endsWith(".webp") ? "webp" : "gif"
    return `data:image/${type};base64,${data}`
  }
  return `/api/themes/${encodeURIComponent(theme.id)}/assets/${encodeURIComponent(file)}`
}

export const hasArt = (t: Theme | undefined) =>
  !!t?.art && !!(t.art.headerImage || t.art.sidebarMascot || t.art.emptyState || t.art.spriteBoard?.length || Object.keys(t.art.sprites ?? {}).length)
