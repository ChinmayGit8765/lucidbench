import type { ComponentType, LazyExoticComponent, ReactNode } from "react"
import type { LucideIcon } from "lucide-react"

import type { Command } from "@/components/CommandPalette"

/**
 * Where a core module sits in the sidebar: the workspace group at the top,
 * or the tools group under the divider. A core module with no group has no
 * sidebar entry of its own: it opens as a tab of its parent, from a link, or
 * from the command palette. Extensions always sit under "Extensions".
 */
export type NavGroup = "workspace" | "tools"

/** Core modules are always present; extensions are added from Settings › Extensions. */
export type ModuleKind = "core" | "extension"

export type ExtensionCategory = "devops" | "cloud" | "data" | "design" | "business" | "productivity"

/**
 * What an extension needs on this machine. Each listed item is checked live:
 * a CLI on PATH (any one of a list joined by "|" counts), an MCP server in the
 * access matrix, an environment variable being set, or a reachable docker
 * engine. Optional requirements show as hints and never block adding.
 */
export interface Requirements {
  clis?: string[]
  mcp?: string[]
  env?: string[]
  docker?: boolean
  /** Any one listed requirement is enough (for example a CLI or an MCP server). */
  anyOf?: boolean
  optional?: { clis?: string[]; mcp?: string[] }
}

/** Props every module page receives: the rest of the route after its own. */
export interface ModulePageProps {
  /** Path segments after the module's route, e.g. ["extensions", "payments"] for /settings/extensions/payments. */
  subpath: string[]
}

export interface ModuleDef {
  id: string
  title: string
  icon: LucideIcon
  /** URL path, e.g. "/containers". Home is "/". */
  route: string
  /** Older top-level paths that still open this module, e.g. "/nextup" for Home. */
  aliases?: string[]
  /** Core modules: the sidebar group; leave it out for a module that lives under its parent. */
  group?: NavGroup
  /** The module this one belongs to: its sidebar entry lights up, and the breadcrumb reads "Parent › Title". */
  parent?: string
  /** The label of this module in its family's tab strip (the parent's own tab included); without one it gets no tab. */
  tab?: string
  kind: ModuleKind
  /** Default position within its sidebar group (or its parent's tabs); the user can reorder the sidebar. */
  order: number
  /** Core: always true. Extensions: whether a new user has it in the sidebar. */
  defaultEnabled: boolean
  /** Roadmap modules show "soon" and cannot be opened or added. */
  status?: "soon"
  /** Shown on "soon" items, e.g. "M2". */
  milestone?: string
  /** One line for the extension gallery, command palette and tooltips. */
  description?: string
  /** Extra words the command palette matches on. */
  keywords?: string
  /** Extensions only. */
  category?: ExtensionCategory
  requires?: Requirements
  /**
   * A core page that shows docker state of its own (System): the shell asks
   * whether the engine is up while it is open, as it does for an added
   * docker extension.
   */
  checksDocker?: boolean
  component?: LazyExoticComponent<ComponentType<ModulePageProps>>
  /**
   * Palette commands. A hook, so it may fetch while the palette is open; it
   * is called for every module on every render, so keep it cheap when closed.
   */
  useCommands?: (paletteOpen: boolean) => Command[]
  /** A stat tile for Home; it should link back to the module. */
  overviewTile?: ComponentType
  /**
   * The tile's place on Home, lower first. Home shows at most four tiles,
   * and only of modules that are live (added, and for a docker extension,
   * with the engine up). A module without a rank shows no tile.
   */
  tileRank?: number
  /**
   * Home's "Needs your move" entries. A hook, called for every module on
   * every Home render in a fixed order. live is false when the module is not
   * in the sidebar or what it needs is down: the hook must then fetch nothing
   * (pass a null path to usePoll) and return []. Return null until the first
   * load is in.
   */
  useAttention?: (live: boolean) => AttentionItem[] | null
}

/** How urgent an attention entry is: broken, about to block you, or your move. */
export type AttentionSeverity = "danger" | "warning" | "info"

export const SEVERITY_RANK: Record<AttentionSeverity, number> = { danger: 0, warning: 1, info: 2 }

/**
 * Whose move an entry is. The user's own moves (a brief to approve, a diff
 * to review, a reply an agent waits for) come before the machine's (Docker
 * is not reachable, a runner is offline), whatever their severity.
 */
export type AttentionKind = "move" | "infra"

/** One "Needs attention" row on Overview. */
export interface AttentionItem {
  /** Unique across modules; prefix it with the module id. */
  key: string
  severity: AttentionSeverity
  /** Default "move"; infrastructure trouble ranks below every move. */
  kind?: AttentionKind
  icon: ReactNode
  title: ReactNode
  meta: ReactNode
  /** Buttons on the right; at least one should take the user to the fix. */
  action?: ReactNode
}
