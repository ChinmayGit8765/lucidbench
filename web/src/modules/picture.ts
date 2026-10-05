import { PenTool } from "lucide-react"

import type { ModuleDef } from "@/modules/types"

/** Extension on the roadmap: listed in the gallery, not addable yet. */
export const picture: ModuleDef = {
  id: "picture",
  title: "Picture",
  icon: PenTool,
  route: "/picture",
  section: "infrastructure",
  kind: "extension",
  order: 13,
  defaultEnabled: false,
  status: "soon",
  category: "design",
  description: "Design boards and schematics next to your code.",
  requires: {},
  keywords: "design diagram schematic canvas",
}
