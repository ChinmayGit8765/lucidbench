import { lazy } from "react"
import { CreditCard } from "lucide-react"

import { StatTile } from "@/components/StatTile"
import { StatusPill } from "@/components/ui/badge"
import { useApp } from "@/lib/app"
import { money, usePaymentsAdded, usePaymentsStatus, useWeekVolume } from "@/lib/payments"
import type { ModuleDef } from "@/modules/types"

function PaymentsTile() {
  const { open } = useApp()
  const added = usePaymentsAdded()
  const status = usePaymentsStatus(added)
  const configured = status.data?.configured ?? false
  const week = useWeekVolume(added && configured)
  const mode = status.data?.mode
  const volume = week.data?.volume ?? []
  const paid = week.data?.items.filter((p) => p.status === "succeeded").length ?? 0
  return (
    <StatTile
      icon={CreditCard}
      label="Payments"
      onOpen={() => open("payments")}
      loading={status.loading && !status.data && !status.error}
      value={
        !status.data ? (
          <span className="text-base font-medium text-muted-foreground">Unavailable</span>
        ) : !configured ? (
          <span className="text-base font-medium text-muted-foreground">Not set up</span>
        ) : week.data ? (
          volume.length > 0 ? (
            <span>{money(volume[0].amount, volume[0].currency)}</span>
          ) : (
            <span className="text-base font-medium text-muted-foreground">No payments yet</span>
          )
        ) : (
          <span className="text-base font-medium text-muted-foreground">{week.error ? "Unavailable" : "Reading"}</span>
        )
      }
      aside={
        configured && mode && mode !== "unknown" ? (
          <StatusPill tone={mode === "live" ? "warning" : "info"}>{mode === "live" ? "LIVE" : "TEST"}</StatusPill>
        ) : undefined
      }
      sub={
        !configured && status.data
          ? "Add a Stripe key to see balances and payments"
          : week.data
            ? `${volume.length > 1 ? `+${volume.length - 1} more ${volume.length === 2 ? "currency" : "currencies"} · ` : ""}last 7 days · ${paid} succeeded${week.data.has_more ? "+" : ""}`
            : (week.error?.message ?? status.error?.message)
      }
    />
  )
}

/** Stripe: balance, payments, products and webhooks; sets up a project's payments in test mode. */
export const payments: ModuleDef = {
  id: "payments",
  title: "Payments",
  icon: CreditCard,
  route: "/payments",
  section: "infrastructure",
  kind: "extension",
  order: 12,
  defaultEnabled: false,
  category: "business",
  description: "Stripe balances, payments and webhooks, and a test-mode setup for a project.",
  requires: { env: ["STRIPE_API_KEY"], mcp: ["stripe"], anyOf: true },
  keywords: "stripe billing invoices checkout subscriptions webhooks",
  component: lazy(() => import("@/pages/Payments")),
  overviewTile: PaymentsTile,
}
