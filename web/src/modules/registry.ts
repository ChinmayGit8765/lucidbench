import type { Prefs } from "@/lib/prefs"
import { MODULES } from "@/modules"
import type { ModuleDef } from "@/modules/types"

export const moduleById = (id: string): ModuleDef | undefined => MODULES.find((m) => m.id === id)

/** Whether a module is in the sidebar: core always, extensions when added. */
export function isAdded(m: ModuleDef, prefs: Prefs): boolean {
  if (m.kind === "core") return true
  if (m.status === "soon") return false
  return prefs.extensions[m.id]?.added ?? m.defaultEnabled
}

/** Whether a module can be opened: in the sidebar and not on the roadmap. */
export const isOpenable = (m: ModuleDef, prefs: Prefs) => m.status !== "soon" && isAdded(m, prefs)

function orderOf(m: ModuleDef, prefs: Prefs): number {
  const p = m.kind === "core" ? prefs.modules[m.id] : prefs.extensions[m.id]
  return p?.order ?? m.order
}

/** Modules sorted by the user's order, falling back to each module's default. */
export function sorted(ms: ModuleDef[], prefs: Prefs): ModuleDef[] {
  return [...ms].sort((a, b) => orderOf(a, prefs) - orderOf(b, prefs) || a.order - b.order)
}

export interface SidebarGroup {
  id: string
  label: string
  items: ModuleDef[]
}

export const SECTION_LABEL: Record<string, string> = {
  workspace: "Workspace",
  ai: "AI",
  extensions: "Extensions",
  settings: "System",
}

/** The sidebar: Workspace and AI core modules, added extensions, then the bottom group. */
export function sidebarGroups(prefs: Prefs): { main: SidebarGroup[]; bottom: ModuleDef[] } {
  const core = (section: string) => sorted(MODULES.filter((m) => m.kind === "core" && m.section === section), prefs)
  const extensions = sorted(MODULES.filter((m) => m.kind === "extension" && isAdded(m, prefs)), prefs)
  const main: SidebarGroup[] = [
    { id: "workspace", label: SECTION_LABEL.workspace, items: core("workspace") },
    { id: "ai", label: SECTION_LABEL.ai, items: core("ai") },
    { id: "extensions", label: SECTION_LABEL.extensions, items: extensions },
  ]
  const bottom = sorted(MODULES.filter((m) => m.kind === "core" && (m.section === "infrastructure" || m.section === "settings")), prefs)
  return { main: main.filter((g) => g.items.length > 0), bottom }
}

/** Every module in sidebar order, openable or not. */
export function navOrder(prefs: Prefs): ModuleDef[] {
  const { main, bottom } = sidebarGroups(prefs)
  return [...main.flatMap((g) => g.items), ...bottom]
}

/** The module a URL path belongs to, and the rest of the path. */
export function matchPath(pathname: string): { module: ModuleDef | undefined; subpath: string[] } {
  const parts = pathname.replace(/\/+$/, "").split("/").filter(Boolean)
  if (parts.length === 0) return { module: moduleById("overview"), subpath: [] }
  const m = MODULES.find((x) => x.route === `/${parts[0]}`)
  return { module: m, subpath: parts.slice(1).map(decodeURIComponent) }
}

export const pathFor = (m: ModuleDef, sub: string[] = []) =>
  (m.route === "/" ? "/" : m.route) + (sub.length ? `/${sub.map(encodeURIComponent).join("/")}` : "")
