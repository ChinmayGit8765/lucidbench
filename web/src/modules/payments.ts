import { CreditCard } from "lucide-react"

import type { ModuleDef } from "@/modules/types"

/** Extension on the roadmap: listed in the gallery, not addable yet. */
export const payments: ModuleDef = {
  id: "payments",
  title: "Payments",
  icon: CreditCard,
  route: "/payments",
  section: "infrastructure",
  kind: "extension",
  order: 12,
  defaultEnabled: false,
  status: "soon",
  category: "business",
  description: "Stripe balances, payments and webhooks.",
  requires: { clis: ["stripe"], mcp: ["stripe"], anyOf: true },
  keywords: "stripe billing invoices",
}
