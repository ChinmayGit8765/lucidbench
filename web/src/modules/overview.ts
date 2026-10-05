import { lazy } from "react"
import { LayoutDashboard } from "lucide-react"

import type { ModuleDef } from "@/modules/types"

export const overview: ModuleDef = {
  id: "overview",
  title: "Overview",
  icon: LayoutDashboard,
  route: "/",
  section: "workspace",
  kind: "core",
  order: 0,
  defaultEnabled: true,
  description: "What needs you, and a tile for every module.",
  keywords: "home dashboard cockpit",
  component: lazy(() => import("@/pages/Overview")),
}
