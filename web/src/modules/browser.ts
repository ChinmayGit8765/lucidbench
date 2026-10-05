import { AppWindow } from "lucide-react"

import type { ModuleDef } from "@/modules/types"

/** Extension on the roadmap: listed in the gallery, not addable yet. */
export const browser: ModuleDef = {
  id: "browser",
  title: "Live browser",
  icon: AppWindow,
  route: "/browser",
  section: "infrastructure",
  kind: "extension",
  order: 14,
  defaultEnabled: false,
  status: "soon",
  category: "productivity",
  description: "Watch and drive a browser your agents use.",
  requires: {},
  keywords: "browser preview automation",
}
