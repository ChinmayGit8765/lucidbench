import { Database } from "lucide-react"

import type { ModuleDef } from "@/modules/types"

/** Extension on the roadmap: listed in the gallery, not addable yet. */
export const databases: ModuleDef = {
  id: "databases",
  title: "Databases",
  icon: Database,
  route: "/databases",
  section: "infrastructure",
  kind: "extension",
  order: 11,
  defaultEnabled: false,
  status: "soon",
  category: "data",
  description: "Your local and remote Postgres and Redis databases.",
  requires: { clis: ["psql|redis-cli"] },
  keywords: "postgres redis sql cache",
}
