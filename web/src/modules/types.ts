import type { ComponentType, LazyExoticComponent } from "react"
import type { LucideIcon } from "lucide-react"

import type { Command } from "@/components/CommandPalette"

/** Where a module sits in the sidebar. Extensions always sit under "Extensions". */
export type Section = "workspace" | "ai" | "infrastructure" | "settings"

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
  /** URL path, e.g. "/containers". Overview is "/". */
  route: string
  section: Section
  kind: ModuleKind
  /** Default position within its sidebar group; the user can reorder. */
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
  component?: LazyExoticComponent<ComponentType<ModulePageProps>>
  /**
   * Palette commands. A hook, so it may fetch while the palette is open; it
   * is called for every module on every render, so keep it cheap when closed.
   */
  useCommands?: (paletteOpen: boolean) => Command[]
  /** A stat tile for the Overview grid; it should link back to the module. */
  overviewTile?: ComponentType
}
