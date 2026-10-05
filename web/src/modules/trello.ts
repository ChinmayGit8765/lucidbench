import { LayoutList } from "lucide-react"

import type { ModuleDef } from "@/modules/types"

/** Extension on the roadmap: listed in the gallery, not addable yet. */
export const trello: ModuleDef = {
  id: "trello",
  title: "Trello",
  icon: LayoutList,
  route: "/trello",
  section: "infrastructure",
  kind: "extension",
  order: 16,
  defaultEnabled: false,
  status: "soon",
  category: "productivity",
  description: "Trello boards and cards beside your projects.",
  requires: { env: ["TRELLO_API_KEY"] },
  keywords: "boards cards kanban",
}
