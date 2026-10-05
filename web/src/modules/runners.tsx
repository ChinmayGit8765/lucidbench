import { lazy, useEffect, useState } from "react"
import { RotateCw, ServerCog } from "lucide-react"

import { RunBars } from "@/components/ci"
import type { Command } from "@/components/CommandPalette"
import { StatTile } from "@/components/StatTile"
import { StatusPill } from "@/components/ui/badge"
import { getJSON, refreshAll, usePoll } from "@/lib/api"
import { useApp } from "@/lib/app"
import {
  CI_POLL_MS,
  failingRuns,
  isFailure,
  rerunFailed,
  runState,
  shortRepo,
  type CIRun,
  type CIRuns,
  type CISummary,
} from "@/lib/ci"
import { useNow } from "@/lib/time"
import { cn } from "@/lib/utils"
import type { ModuleDef } from "@/modules/types"

function RunnersTile() {
  const { open } = useApp()
  const now = useNow(5000)
  const summary = usePoll<CISummary>("/api/ci/summary", CI_POLL_MS)
  const runsPoll = usePoll<CIRuns>("/api/ci/runs", CI_POLL_MS)
  const s = summary.data
  const runs = runsPoll.data?.runs ?? []
  const failing = failingRuns(runs)
  const recent = runs.slice(0, 30)
  const pass = recent.filter((r) => runState(r) === "success").length
  const fail = recent.filter(isFailure).length
  const rate24 = s?.runs_24h.pass_rate ?? null
  const rate = rate24 ?? (pass + fail > 0 ? pass / (pass + fail) : null)
  return (
    <StatTile
      icon={ServerCog}
      label="Runners & CI"
      onOpen={() => open("runners")}
      loading={(runsPoll.loading && !runsPoll.data) || (summary.loading && !s)}
      value={
        <span className={cn(rate !== null && rate < 0.8 && "text-danger-fg")}>
          {rate === null ? "-" : `${Math.round(rate * 100)}%`}
          <span className="text-base font-normal text-subtle-foreground"> pass</span>
        </span>
      }
      aside={
        failing.length > 0 ? (
          <StatusPill tone="danger">{failing.length} failing</StatusPill>
        ) : s && s.runners.busy > 0 ? (
          <StatusPill tone="info" pulse>
            {s.runners.busy} busy
          </StatusPill>
        ) : runs.length > 0 ? (
          <StatusPill tone="success">green</StatusPill>
        ) : undefined
      }
      sub={
        s
          ? `${s.runners.online}/${s.runners.total} runners online · ${rate24 !== null ? "last 24h" : `last ${recent.length} runs`}`
          : (summary.error?.message ?? "not configured")
      }
      footer={<RunBars runs={runs} slots={24} now={now} className="h-6" />}
    />
  )
}

/** "Re-run last failed run": fetches fresh runs while the palette is open. */
function useRunnerCommands(paletteOpen: boolean): Command[] {
  const [lastFailed, setLastFailed] = useState<CIRun | null | undefined>(undefined)
  useEffect(() => {
    if (!paletteOpen) return
    let cancelled = false
    setLastFailed(undefined)
    getJSON<CIRuns>("/api/ci/runs")
      .then((r) => !cancelled && setLastFailed(failingRuns(r.runs)[0] ?? null))
      .catch(() => !cancelled && setLastFailed(null))
    return () => {
      cancelled = true
    }
  }, [paletteOpen])
  return [
    {
      id: "rerun-last-failed",
      label: "Re-run last failed run",
      group: "Actions",
      icon: RotateCw,
      disabled: !lastFailed,
      hint:
        lastFailed === undefined
          ? "checking…"
          : lastFailed
            ? `${lastFailed.name} · ${shortRepo(lastFailed.repo)}`
            : "nothing failing",
      keywords: "ci github retry",
      run: () => {
        if (lastFailed) void rerunFailed(lastFailed).then((ok) => ok && setTimeout(refreshAll, 1500))
      },
    },
  ]
}

export const runners: ModuleDef = {
  id: "runners",
  title: "Runners & CI",
  icon: ServerCog,
  route: "/runners",
  section: "infrastructure",
  kind: "extension",
  category: "devops",
  order: 0,
  defaultEnabled: true,
  description: "Self-hosted GitHub Actions runners, their containers, and every recent workflow run.",
  requires: { docker: true, optional: { clis: ["gh"] } },
  keywords: "ci github actions workflow runs runners",
  component: lazy(() => import("@/pages/Runners")),
  useCommands: useRunnerCommands,
  overviewTile: RunnersTile,
}
