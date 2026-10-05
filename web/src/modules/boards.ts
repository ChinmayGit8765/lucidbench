import { SquareKanban } from "lucide-react"

import type { ModuleDef } from "@/modules/types"

/** On the roadmap: shown as "soon" in the sidebar, not openable yet. */
export const boards: ModuleDef = {
  id: "boards",
  title: "Boards",
  icon: SquareKanban,
  route: "/boards",
  section: "workspace",
  kind: "core",
  order: 4,
  defaultEnabled: true,
  status: "soon",
  milestone: "M3",
  description: "Lucidbench's own task boards.",
  keywords: "kanban tasks tickets",
}
