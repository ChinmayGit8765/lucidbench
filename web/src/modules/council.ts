import { Vote } from "lucide-react"

import type { ModuleDef } from "@/modules/types"

/** On the roadmap: shown as "soon" in the sidebar, not openable yet. */
export const council: ModuleDef = {
  id: "council",
  title: "Council",
  icon: Vote,
  route: "/council",
  section: "ai",
  kind: "core",
  order: 2,
  defaultEnabled: true,
  status: "soon",
  milestone: "M1",
  description: "Ask several models at once and compare their answers.",
  keywords: "models compare review",
}
