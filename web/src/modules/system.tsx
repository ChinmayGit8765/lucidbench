import { lazy, useEffect, useState } from "react"
import { Activity, ArrowRight, Layers, Moon, Play, RefreshCw, Ship, TriangleAlert } from "lucide-react"
import { toast } from "sonner"

import type { Command } from "@/components/CommandPalette"
import { PowerTile } from "@/components/Power"
import { Button } from "@/components/ui/button"
import { getJSON, refreshAll } from "@/lib/api"
import { useApp } from "@/lib/app"
import { runHelloJob } from "@/lib/jobs"
import { isAsleep, powerAction, sleepIdle, usePower, type PowerState } from "@/lib/power"
import { relativeTime, useNow } from "@/lib/time"
import type { AttentionItem, ModuleDef } from "@/modules/types"

function useSystemCommands(paletteOpen: boolean): Command[] {
  // Power state is read only while the palette is open.
  const [power, setPower] = useState<PowerState | null>(null)
  useEffect(() => {
    if (!paletteOpen) return
    let cancelled = false
    getJSON<PowerState>("/api/power")
      .then((p) => !cancelled && setPower(p))
      .catch(() => !cancelled && setPower(null))
    return () => {
      cancelled = true
    }
  }, [paletteOpen])
  const out: Command[] = [
    {
      id: "hello",
      label: "Run hello job",
      group: "Actions",
      icon: Play,
      hint: power && isAsleep(power.cluster) && power.modes.cluster !== "off" ? "wakes the cluster" : "local cluster",
      keywords: "job kubernetes test",
      run: () => void runHelloJob(!!power && isAsleep(power.cluster)),
    },
    {
      id: "refresh",
      label: "Refresh data",
      group: "Actions",
      icon: RefreshCw,
      keywords: "reload update",
      run: () => {
        refreshAll()
        toast.success("Refreshing")
      },
    },
  ]
  if (power) {
    const c = power.cluster
    out.push({
      id: "power-start-cluster",
      label: "Start cluster",
      group: "Actions",
      icon: Ship,
      disabled: !isAsleep(c),
      hint: isAsleep(c) ? `kind · ${c.name}` : c.state,
      keywords: "power wake kubernetes kind on demand",
      run: () => void powerAction("cluster", c.name, "start"),
    })
    out.push({
      id: "power-sleep",
      label: "Sleep everything idle…",
      group: "Actions",
      icon: Moon,
      hint: `${power.running} running`,
      keywords: "power stop idle on demand save memory",
      children: [
        {
          id: "power-sleep-confirm",
          label: "Yes, stop idle on-demand things now",
          group: "Sleep everything idle",
          icon: Moon,
          hint: "busy things stay up",
          run: () => void sleepIdle(),
        },
      ],
    })
    for (const s of power.stacks.filter(isAsleep)) {
      out.push({
        id: `power-start-stack-${s.name}`,
        label: `Start ${s.name}`,
        group: "Actions",
        icon: Layers,
        hint: `stack · ${s.containers.length} containers`,
        keywords: "power stack compose start wake",
        run: () => void powerAction("stack", s.name, "start"),
      })
    }
  }
  return out
}

/** Needs attention: something the power supervisor failed to start or stop. */
function usePowerAttention(): AttentionItem[] | null {
  const { open } = useApp()
  const now = useNow(5000)
  const power = usePower()
  if (!power.data && !power.error) return null
  const p = power.data
  if (!p) return []
  return [p.cluster, ...p.runners, ...p.stacks]
    .filter((it) => it.error)
    .map((it): AttentionItem => ({
      key: `power-${it.kind}-${it.name}`,
      severity: it.kind === "cluster" ? "danger" : "warning",
      icon: <TriangleAlert className="size-3.5 text-danger" />,
      title: (
        <>
          {it.kind === "cluster" ? "Cluster" : it.kind === "runner" ? "Runner" : "Stack"}{" "}
          <span className="font-mono text-[0.92em]">{it.name}</span> {isAsleep(it) ? "failed to start" : "had a power error"}
        </>
      ),
      meta: (
        <>
          {it.error}
          {it.error_at && ` · ${relativeTime(it.error_at, now)}`}
        </>
      ),
      action: (
        <>
          {isAsleep(it) && (
            <Button variant="secondary" size="sm" onClick={() => void powerAction(it.kind, it.name, "start")}>
              <Play /> Retry
            </Button>
          )}
          <Button variant="ghost" size="sm" onClick={() => open("system", ["power"])}>
            View <ArrowRight />
          </Button>
        </>
      ),
    }))
}

/**
 * Daemon and cluster health with a quick view of the hello jobs, and the
 * Power card: what runs only when needed. The full cluster (pods, all jobs,
 * events, logs) lives in the Kubernetes extension; this stays core so health
 * and power are visible even when that extension is removed.
 */
export const system: ModuleDef = {
  id: "system",
  title: "System",
  icon: Activity,
  route: "/system",
  section: "infrastructure",
  kind: "core",
  order: 0,
  defaultEnabled: true,
  description: "Daemon, cluster and jobs health, and what runs on demand.",
  keywords: "health daemon cluster jobs kubeconfig power sleep wake on demand",
  component: lazy(() => import("@/pages/System")),
  useCommands: useSystemCommands,
  overviewTile: PowerTile,
  useAttention: usePowerAttention,
}
