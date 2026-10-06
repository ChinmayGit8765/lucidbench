import { lazy } from "react"
import { LayoutList } from "lucide-react"

import type { ModuleDef } from "@/modules/types"

/** Trello boards as lists of cards: add, move, and link from native cards. Needs an API key and token. */
export const trello: ModuleDef = {
  id: "trello",
  title: "Trello",
  icon: LayoutList,
  route: "/trello",
  section: "infrastructure",
  kind: "extension",
  order: 16,
  defaultEnabled: false,
  category: "productivity",
  description: "Trello boards and cards beside your projects.",
  requires: { env: ["TRELLO_API_KEY", "TRELLO_TOKEN"] },
  keywords: "boards cards kanban lists",
  component: lazy(() => import("@/pages/Trello")),
}
