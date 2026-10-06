import { lazy } from "react"
import { ArrowRight, Gauge } from "lucide-react"

import { StatTile } from "@/components/StatTile"
import { Button } from "@/components/ui/button"
import { useApp } from "@/lib/app"
import { usePrefs } from "@/lib/prefs"
import { useNow } from "@/lib/time"
import { fmtTokens, livePercent, resetsIn, usageAlerts, useUsageSummary, USAGE_WARN_PERCENT } from "@/lib/usage"
import { cn } from "@/lib/utils"
import type { AttentionItem, ModuleDef } from "@/modules/types"

function UsageTile() {
  const { open } = useApp()
  const { label } = usePrefs()
  const poll = useUsageSummary()
  const now = useNow(30000)
  const s = poll.data
  const wins = (s?.providers ?? []).flatMap((p) => p.windows.map((w) => ({ p, w, pct: livePercent(w) })))
  const known = wins.filter((x) => x.pct !== null)
  const top = known.reduce<(typeof known)[number] | null>((a, x) => (a === null || (x.pct as number) > (a.pct as number) ? x : a), null)
  const week = (s?.providers ?? []).reduce((n, p) => n + p.totals.total - p.totals.cache_read, 0)
  return (
    <StatTile
      icon={Gauge}
      label={label("usage_meter", "Usage")}
      onOpen={() => open("usage")}
      loading={poll.loading && !s}
      value={top ? `${Math.round(top.pct as number)}%` : "-"}
      sub={
        top
          ? `${top.p.label} ${top.w.label} window${resetsIn(top.w.resets_at, now) ? ` · resets in ${resetsIn(top.w.resets_at, now)}` : ""}`
          : `${fmtTokens(week)} tokens in 7 days, no limit window reported`
      }
      footer={
        <div className="space-y-1.5">
          {wins.length === 0 && <div className="h-1.5 rounded-full bg-muted" />}
          {wins.slice(0, 3).map(({ p, w, pct }) => (
            <div key={`${p.id}-${w.name}`} className="flex items-center gap-2" title={`${p.label} ${w.label}: ${pct === null ? "unknown" : `${Math.round(pct)}% used`}`}>
              <span className="w-24 shrink-0 truncate text-2xs text-subtle-foreground">
                {p.label} {w.label}
              </span>
              <span className="h-1.5 min-w-0 flex-1 overflow-hidden rounded-full bg-muted">
                <span
                  className={cn("block h-full rounded-full", (pct ?? 0) >= USAGE_WARN_PERCENT ? "bg-danger" : (pct ?? 0) >= 60 ? "bg-warning" : "bg-brand")}
                  style={{ width: `${Math.min(100, pct ?? 0)}%` }}
                />
              </span>
            </div>
          ))}
        </div>
      }
    />
  )
}

/** Needs attention: limit windows at 80 % or more; 95 % and up reads as urgent. */
function useUsageAttention(): AttentionItem[] | null {
  const { open } = useApp()
  const { label } = usePrefs()
  const poll = useUsageSummary()
  const now = useNow(30000)
  if (!poll.data) return poll.error ? [] : null
  return usageAlerts(poll.data).map((a): AttentionItem => ({
    key: `usage-${a.provider}-${a.window.name}`,
    severity: a.percent >= 95 ? "danger" : "warning",
    icon: <Gauge className={cn("size-3.5", a.percent >= 95 ? "text-danger" : "text-warning")} />,
    title: (
      <>
        {a.providerLabel} {a.window.label} {label("usage_meter", "usage")} at {Math.round(a.percent)}%
      </>
    ),
    meta: resetsIn(a.window.resets_at, now) ? `resets in ${resetsIn(a.window.resets_at, now)}` : "reset time unknown",
    action: (
      <Button variant="ghost" size="sm" onClick={() => open("usage")}>
        View <ArrowRight />
      </Button>
    ),
  }))
}

export const usage: ModuleDef = {
  id: "usage",
  title: "Usage",
  icon: Gauge,
  route: "/usage",
  section: "ai",
  kind: "core",
  order: 3,
  defaultEnabled: true,
  description: "Tokens and limit windows for each subscription, read from the logs on this machine.",
  keywords: "limits quota tokens cost meter windows spend",
  component: lazy(() => import("@/pages/Usage")),
  overviewTile: UsageTile,
  useAttention: useUsageAttention,
}
