import { lazy } from "react"
import { Gauge } from "lucide-react"

import { StatTile } from "@/components/StatTile"
import { useApp } from "@/lib/app"
import { usePrefs } from "@/lib/prefs"
import { useNow } from "@/lib/time"
import { fmtTokens, livePercent, resetsIn, useUsageSummary, USAGE_WARN_PERCENT } from "@/lib/usage"
import { cn } from "@/lib/utils"
import type { ModuleDef } from "@/modules/types"

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
              <span className="w-14 shrink-0 truncate text-2xs text-subtle-foreground">
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
}
