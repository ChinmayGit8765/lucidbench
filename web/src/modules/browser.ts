import { lazy } from "react"
import { AppWindow } from "lucide-react"

import type { ModuleDef } from "@/modules/types"

export const browser: ModuleDef = {
  id: "browser",
  title: "Live browser",
  icon: AppWindow,
  route: "/browser",
  section: "infrastructure",
  kind: "extension",
  order: 14,
  defaultEnabled: false,
  category: "devops",
  description: "Watch and drive the browser your agents use: a Chromium container with its own profile, a live view, take-over and a dev server preview.",
  requires: { docker: true },
  keywords: "browser preview automation chromium cdp screenshot headless agent",
  component: lazy(() => import("@/pages/Browser")),
}
