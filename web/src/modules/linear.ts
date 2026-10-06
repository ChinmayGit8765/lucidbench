import { lazy } from "react"
import { Waypoints } from "lucide-react"

import type { ModuleDef } from "@/modules/types"

/**
 * Linear issues as a board, and promote or link from native cards. Needs a
 * personal API key; a Linear MCP server is detected and explained, but it is
 * the AI clients' access, not this connector's.
 */
export const linear: ModuleDef = {
  id: "linear",
  title: "Linear",
  icon: Waypoints,
  route: "/linear",
  section: "infrastructure",
  kind: "extension",
  order: 15,
  defaultEnabled: false,
  category: "productivity",
  description: "Linear issues by state, with promote and link from your cards.",
  requires: { env: ["LINEAR_API_KEY"], mcp: ["linear"], anyOf: true },
  keywords: "issues tickets cycles teams projects",
  component: lazy(() => import("@/pages/Linear")),
}
