import { NotebookPen } from "lucide-react"

import type { ModuleDef } from "@/modules/types"

/** On the roadmap: shown as "soon" in the sidebar, not openable yet. */
export const memory: ModuleDef = {
  id: "memory",
  title: "Memory",
  icon: NotebookPen,
  route: "/memory",
  section: "workspace",
  kind: "core",
  order: 3,
  defaultEnabled: true,
  status: "soon",
  description: "A notes workspace over a Markdown vault that other note apps can open too.",
  keywords: "notes vault markdown wiki docs",
}
