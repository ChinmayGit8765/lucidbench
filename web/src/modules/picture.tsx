import { lazy } from "react"
import { PenTool } from "lucide-react"

import type { ModuleDef } from "@/modules/types"

export const picture: ModuleDef = {
  id: "picture",
  title: "Picture",
  icon: PenTool,
  route: "/picture",
  section: "infrastructure",
  kind: "extension",
  order: 13,
  defaultEnabled: false,
  category: "design",
  description: "Back-end schematics in Mermaid and front-end design canvases in Excalidraw, saved next to your code.",
  requires: {},
  keywords: "design diagram schematic canvas mermaid excalidraw architecture wireframe",
  component: lazy(() => import("@/pages/Picture")),
}
