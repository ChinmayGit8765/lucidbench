import type { Prefs } from "@/lib/prefs"
import { MODULES } from "@/modules"
import { SEVERITY_RANK, type AttentionItem, type ModuleDef, type NavGroup } from "@/modules/types"

export const moduleById = (id: string): ModuleDef | undefined => MODULES.find((m) => m.id === id)

/** Whether a module is in the sidebar: core always, extensions when added. */
export function isAdded(m: ModuleDef, prefs: Prefs): boolean {
  if (m.kind === "core") return true
  if (m.status === "soon") return false
  return prefs.extensions[m.id]?.added ?? m.defaultEnabled
}

/** Whether a module can be opened: in the sidebar and not on the roadmap. */
export const isOpenable = (m: ModuleDef, prefs: Prefs) => m.status !== "soon" && isAdded(m, prefs)

/**
 * Whether a module is live: it can be opened, and what it needs is up. A
 * docker extension is live only while the engine answers (dockerUp is
 * undefined until the first check is in, which counts as not yet). Home's
 * tiles and attention, and the shell's own polls, ask only live modules.
 */
export const isLive = (m: ModuleDef, prefs: Prefs, dockerUp: boolean | undefined) =>
  isOpenable(m, prefs) && (!m.requires?.docker || dockerUp === true)

/** Whether any added extension needs the docker engine, so the shell should ask whether it is up. */
export const needsDocker = (prefs: Prefs) => MODULES.some((m) => m.kind === "extension" && m.requires?.docker && isOpenable(m, prefs))

function orderOf(m: ModuleDef, prefs: Prefs): number {
  const p = m.kind === "core" ? prefs.modules[m.id] : prefs.extensions[m.id]
  return p?.order ?? m.order
}

/** Modules sorted by the user's order, falling back to each module's default. */
export function sorted(ms: ModuleDef[], prefs: Prefs): ModuleDef[] {
  return [...ms].sort((a, b) => orderOf(a, prefs) - orderOf(b, prefs) || a.order - b.order)
}

export interface SidebarGroup {
  id: NavGroup
  items: ModuleDef[]
}

/**
 * The sidebar: the workspace group, a divider, then the tools group. The
 * added extensions sit under the Extensions entry, in their own order.
 */
export function sidebarGroups(prefs: Prefs): { groups: SidebarGroup[]; extensions: ModuleDef[] } {
  const core = (g: NavGroup) => sorted(MODULES.filter((m) => m.kind === "core" && m.group === g), prefs)
  return {
    groups: [
      { id: "workspace", items: core("workspace") },
      { id: "tools", items: core("tools") },
    ],
    extensions: sorted(MODULES.filter((m) => m.kind === "extension" && isAdded(m, prefs)), prefs),
  }
}

/** The modules that live under a parent, in tab order. */
export const childrenOf = (id: string) => MODULES.filter((m) => m.parent === id).sort((a, b) => a.order - b.order)

/**
 * Every module in navigation order, openable or not: each sidebar entry
 * followed by the modules under it, then the added extensions. The command
 * palette's "Go to" uses this, so a module without a sidebar entry is still
 * one search away.
 */
export function navOrder(prefs: Prefs): ModuleDef[] {
  const { groups, extensions } = sidebarGroups(prefs)
  const out: ModuleDef[] = []
  for (const m of groups.flatMap((g) => g.items)) {
    out.push(m)
    for (const c of childrenOf(m.id)) out.push(c)
    if (m.id === "extensions") out.push(...extensions)
  }
  return out
}

/** The sidebar entry a module lights up: its parent's when it has one, else its own. */
export function navOwner(m: ModuleDef | undefined): string | undefined {
  if (!m) return undefined
  return m.parent ?? m.id
}

/**
 * A module's tab family: its parent (or itself) and the children that have
 * a tab, in order. Empty when the module has no tabs to show.
 */
export function familyOf(m: ModuleDef | undefined): ModuleDef[] {
  if (!m) return []
  const root = m.parent ? moduleById(m.parent) : m
  if (!root || (m !== root && !m.tab)) return []
  const kids = childrenOf(root.id).filter((c) => c.tab)
  return kids.length > 0 && root.tab ? [root, ...kids] : []
}

/** The breadcrumb: the parent's title, or "Extensions" for an extension, then the module's own. */
export function breadcrumb(m: ModuleDef | undefined): string[] {
  if (!m) return ["Not found"]
  if (m.parent) return [moduleById(m.parent)?.title ?? m.parent, m.title]
  if (m.kind === "extension") return [moduleById("extensions")?.title ?? "Extensions", m.title]
  return [m.title]
}

/**
 * Old addresses that moved, so bookmarks and links keep working. Returns
 * where path now lives, or null when it has not moved. /boards with one
 * board is handled by the Work tabs, since it needs the board list.
 */
export function redirectPath(path: string): string | null {
  const p = path.replace(/\/+$/, "") || "/"
  if (p === "/nextup") return "/"
  if (p === "/council") return "/ideas"
  if (p === "/assistant/braindump") return "/ideas/new/split"
  const ext = /^\/settings\/extensions(\/.*)?$/.exec(p)
  if (ext) return `/extensions${ext[1] ?? ""}`
  return null
}

/** The module a URL path belongs to, and the rest of the path. */
export function matchPath(pathname: string): { module: ModuleDef | undefined; subpath: string[] } {
  const parts = pathname.replace(/\/+$/, "").split("/").filter(Boolean)
  if (parts.length === 0) return { module: moduleById("overview"), subpath: [] }
  const top = `/${parts[0]}`
  const m = MODULES.find((x) => x.route === top) ?? MODULES.find((x) => x.aliases?.includes(top))
  return { module: m, subpath: parts.slice(1).map(decodeURIComponent) }
}

export const pathFor = (m: ModuleDef, sub: string[] = []) =>
  (m.route === "/" ? "/" : m.route) + (sub.length ? `/${sub.map(encodeURIComponent).join("/")}` : "")

/**
 * Home's "Needs your move": every module's entries, the user's own moves
 * first and infrastructure trouble last, each part most urgent first. Each
 * module's hook runs on every render in a fixed order (the rules of hooks),
 * told whether its module is live; a module that is not live fetches
 * nothing. Entries show as each module answers: loading is true only until
 * the first one does, and pending while any is still out.
 */
export function useAttention(prefs: Prefs, dockerUp: boolean | undefined): { items: AttentionItem[]; loading: boolean; pending: boolean } {
  const per = MODULES.map((m) => {
    const live = isLive(m, prefs, dockerUp)
    return { m, live, items: m.useAttention ? m.useAttention(live) : [] }
  })
  const live = per.filter((x) => x.live && x.m.useAttention)
  const items = live
    .flatMap(({ items }) => items ?? [])
    .map((a, i) => ({ a, i }))
    .sort(
      (x, y) =>
        Number(x.a.kind === "infra") - Number(y.a.kind === "infra") ||
        SEVERITY_RANK[x.a.severity] - SEVERITY_RANK[y.a.severity] ||
        x.i - y.i,
    )
    .map(({ a }) => a)
  const waiting = live.filter(({ items }) => items === null).length
  return { items, loading: live.length > 0 && waiting === live.length, pending: waiting > 0 }
}
