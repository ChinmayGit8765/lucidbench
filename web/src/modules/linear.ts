import { Waypoints } from "lucide-react"

import type { ModuleDef } from "@/modules/types"

/** Extension on the roadmap: listed in the gallery, not addable yet. */
export const linear: ModuleDef = {
  id: "linear",
  title: "Linear",
  icon: Waypoints,
  route: "/linear",
  section: "infrastructure",
  kind: "extension",
  order: 15,
  defaultEnabled: false,
  status: "soon",
  category: "productivity",
  description: "Linear issues and cycles beside your projects.",
  requires: { mcp: ["linear"] },
  keywords: "issues tickets cycles",
}
