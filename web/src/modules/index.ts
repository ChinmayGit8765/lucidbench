import { accounts } from "@/modules/accounts"
import { boards } from "@/modules/boards"
import { browser } from "@/modules/browser"
import { cloud } from "@/modules/cloud"
import { containers } from "@/modules/containers"
import { council } from "@/modules/council"
import { databases } from "@/modules/databases"
import { ideas } from "@/modules/ideas"
import { kubernetes } from "@/modules/kubernetes"
import { linear } from "@/modules/linear"
import { mcp } from "@/modules/mcp"
import { nextup } from "@/modules/nextup"
import { memory } from "@/modules/memory"
import { overview } from "@/modules/overview"
import { payments } from "@/modules/payments"
import { picture } from "@/modules/picture"
import { projects } from "@/modules/projects"
import { runners } from "@/modules/runners"
import { settings } from "@/modules/settings"
import { studio } from "@/modules/studio"
import { system } from "@/modules/system"
import { trello } from "@/modules/trello"
import type { ModuleDef } from "@/modules/types"
import { usage } from "@/modules/usage"
import { work } from "@/modules/work"

/**
 * Every module. The sidebar, router, command palette, Overview tiles and
 * the Settings › Extensions gallery are all built from this list plus the
 * user's prefs. To add a module, write one file and list it here; see
 * docs/EXTENDING.md.
 */
export const MODULES: ModuleDef[] = [
  // Core: always present, reorderable.
  overview,
  nextup,
  work,
  ideas,
  projects,
  memory,
  boards,
  accounts,
  mcp,
  council,
  studio,
  usage,
  system,
  settings,
  // Extensions: added from Settings › Extensions.
  runners,
  containers,
  kubernetes,
  cloud,
  databases,
  payments,
  picture,
  browser,
  linear,
  trello,
]
