import { lazy } from "react"
import { Blocks } from "lucide-react"

import type { ModuleDef } from "@/modules/types"

/**
 * The extension gallery. The extensions you add sit under its sidebar
 * entry; /settings/extensions (where the gallery used to be) redirects here.
 */
export const extensions: ModuleDef = {
  id: "extensions",
  title: "Extensions",
  icon: Blocks,
  route: "/extensions",
  group: "tools",
  kind: "core",
  order: 2,
  defaultEnabled: true,
  description: "Add or remove extensions: CI runners, containers, Kubernetes, cloud deploys, databases, payments and more.",
  keywords: "extension gallery add remove install enable sidebar plugins",
  component: lazy(() => import("@/pages/Extensions")),
}
