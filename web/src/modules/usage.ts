import { Gauge } from "lucide-react"

import type { ModuleDef } from "@/modules/types"

/** On the roadmap: shown as "soon" in the sidebar, not openable yet. */
export const usage: ModuleDef = {
  id: "usage",
  title: "Usage",
  icon: Gauge,
  route: "/usage",
  section: "ai",
  kind: "core",
  order: 3,
  defaultEnabled: true,
  status: "soon",
  milestone: "M2",
  description: "How much of each subscription you have used.",
  keywords: "limits quota tokens cost",
}
