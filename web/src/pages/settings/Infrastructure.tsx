import { useEffect, useState } from "react"
import { Copy, RotateCcw } from "lucide-react"

import { PendingRestart, SaveButton, useConfigFile, useConfigSave, type ConfigFile } from "@/components/ConfigSave"
import { copyText } from "@/components/CopyCommand"
import { PowerCard } from "@/components/Power"
import { StatusPill } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { ErrorState, Skeleton } from "@/components/ui/states"
import { usePoll } from "@/lib/api"
import type { PowerMode } from "@/lib/power"
import { Row, Section, Segmented } from "@/pages/settings/controls"

/** The power section of GET /api/config. */
export interface PowerConfig {
  cluster: PowerMode
  cluster_idle_minutes: number
  runners: PowerMode
  runner_idle_minutes: number
  stacks: { project: string; mode: PowerMode }[]
  poll_seconds: number
}

export const MODES: { id: PowerMode; label: string; title: string }[] = [
  { id: "always", label: "Always on", title: "Lucidbench never stops it" },
  { id: "on-demand", label: "On demand", title: "Started when needed, stopped when idle" },
  { id: "off", label: "Manual", title: "Lucidbench never starts or stops it on its own" },
]

/** The power: block for config.yaml, in the same shape as config.example.yaml. */
export function snippet(p: PowerConfig): string {
  const stacks =
    p.stacks.length === 0
      ? "  stacks: []"
      : "  stacks:\n" + p.stacks.map((s) => `    - project: "${s.project}"\n      mode: "${s.mode}"`).join("\n")
  return [
    "power:",
    `  cluster: "${p.cluster}"`,
    `  cluster_idle_minutes: ${p.cluster_idle_minutes}`,
    `  runners: "${p.runners}"`,
    `  runner_idle_minutes: ${p.runner_idle_minutes}`,
    stacks,
    `  poll_seconds: ${p.poll_seconds}`,
  ].join("\n")
}

function Minutes({ value, onChange, label, min = 1, max = 1440 }: { value: number; onChange: (n: number) => void; label: string; min?: number; max?: number }) {
  return (
    <label className="flex items-center gap-2 text-xs text-muted-foreground">
      <input
        type="number"
        aria-label={label}
        min={min}
        max={max}
        value={value}
        onChange={(e) => {
          const n = Math.round(Number(e.target.value))
          if (Number.isFinite(n)) onChange(Math.min(max, Math.max(min, n)))
        }}
        className="h-7 w-16 rounded-md border bg-background px-2 text-right text-xs tabular-nums text-foreground outline-none focus-visible:ring-2 focus-visible:ring-ring"
      />
      {label.includes("seconds") ? "s" : "min"}
    </label>
  )
}

/** The power keys the page saves, as they are named in config.yaml. */
const KEYS = ["cluster", "cluster_idle_minutes", "runners", "runner_idle_minutes", "poll_seconds"] as const
type Key = (typeof KEYS)[number]
type Draft = Pick<PowerConfig, Key>

function fromFile(f: ConfigFile): Draft {
  const v = (k: Key) => f.values[`power.${k}`]
  return {
    cluster: v("cluster") as PowerMode,
    cluster_idle_minutes: Number(v("cluster_idle_minutes")),
    runners: v("runners") as PowerMode,
    runner_idle_minutes: Number(v("runner_idle_minutes")),
    poll_seconds: Number(v("poll_seconds")),
  }
}

/**
 * Settings › Infrastructure: when the cluster, runners and stacks run. Save
 * writes the changed power keys to config.yaml (after showing the diff);
 * the engine reads its config at start, so a saved change applies after a
 * restart, and the page says so until then.
 */
export function Infrastructure() {
  const file = useConfigFile()
  const running = usePoll<{ power: PowerConfig }>("/api/config", 60000)
  const stacks = running.data?.power.stacks ?? []
  const saved = file.data ? fromFile(file.data) : null
  const [draft, setDraft] = useState<Draft | null>(null)
  useEffect(() => {
    if (saved && !draft) setDraft(saved)
  }, [saved, draft])
  // After a save the draft is what the file now says; the refresh catches up.
  const { save, busy, dialog } = useConfigSave(() => file.refresh())
  if (file.error && !file.data) return <ErrorState title="Could not read the configuration" message={file.error.message} onRetry={file.refresh} />
  if (!saved || !draft) return <Skeleton className="h-64 rounded-xl" />
  const set = (patch: Partial<Draft>) => setDraft({ ...draft, ...patch })
  const changed = KEYS.filter((k) => draft[k] !== saved[k])
  const src = (k: string) => file.data?.sources[`power.${k}`] ?? "default"
  const fromEnv = ["cluster", "runners"].filter((k) => src(k).startsWith("env:"))
  return (
    <div className="space-y-5">
      <Section
        title="On-demand infrastructure"
        description="Run the local cluster, runner containers and stacks only when something needs them. Idle things are stopped to free memory; Start and Stop work in every mode. Saved to config.yaml: the idle times and the check interval apply at once, the modes when the engine restarts."
        actions={
          <>
            {changed.length > 0 && (
              <Button variant="ghost" size="sm" onClick={() => setDraft(saved)}>
                <RotateCcw /> Reset
              </Button>
            )}
            <SaveButton
              dirty={changed.length > 0}
              busy={busy}
              label={changed.length > 1 ? `Save ${changed.length} changes` : "Save"}
              onClick={() => void save(Object.fromEntries(changed.map((k) => [`power.${k}`, draft[k]])), "the infrastructure settings")}
            />
          </>
        }
      >
        <Row label="Kubernetes cluster" hint="On demand: a submitted job starts the kind node; it stops after the idle time with no pod, job or submission.">
          <Segmented label="Cluster mode" value={draft.cluster} options={MODES} onChange={(cluster) => set({ cluster })} />
        </Row>
        <Row label="Cluster idle time" hint="Minutes before an idle on-demand cluster sleeps.">
          <Minutes label="Cluster idle minutes" value={draft.cluster_idle_minutes} onChange={(cluster_idle_minutes) => set({ cluster_idle_minutes })} />
        </Row>
        <Row label="Runner containers" hint="On demand: a queued run starts its repository's stopped runner; a runner with no job and no queued run sleeps. Busy runners are never stopped.">
          <Segmented label="Runner mode" value={draft.runners} options={MODES} onChange={(runners) => set({ runners })} />
        </Row>
        <Row label="Runner idle time" hint="Minutes before an idle on-demand runner sleeps.">
          <Minutes label="Runner idle minutes" value={draft.runner_idle_minutes} onChange={(runner_idle_minutes) => set({ runner_idle_minutes })} />
        </Row>
        <Row label="Check every" hint="How often the cluster and runners are checked. On-demand runners ask GitHub this often; unchanged answers do not count against the rate limit.">
          <Minutes label="Poll seconds" min={10} max={3600} value={draft.poll_seconds} onChange={(poll_seconds) => set({ poll_seconds })} />
        </Row>
        <Row
          label="Stacks"
          hint={`${stacks.length ? stacks.map((s) => `${s.project} (${s.mode})`).join(", ") : "None listed."} Edit power.stacks in config.yaml to change the list.`}
        >
          <StatusPill tone="neutral">{stacks.length} listed</StatusPill>
        </Row>
        {fromEnv.length > 0 && (
          <p className="border-t px-5 py-3 text-xs text-warning-fg">
            {fromEnv.map((k) => `power.${k}`).join(" and ")} {fromEnv.length === 1 ? "is" : "are"} set by {fromEnv.map((k) => src(k).slice(4)).join(", ")}, which
            overrides the file.
          </p>
        )}
        <PendingRestart file={file.data} keys={KEYS.map((k) => `power.${k}`)} />
        <div className="flex items-center gap-2 border-t px-5 py-2.5 text-2xs text-subtle-foreground">
          <span className="truncate font-mono" title={file.data?.path}>
            {file.data?.path}
          </span>
          <button onClick={() => void copyText(snippet({ ...draft, stacks: [] } as PowerConfig).replace(/\n {2}stacks: \[\]/, ""), "Config copied")} className="shrink-0 hover:text-foreground">
            <Copy className="mr-1 inline size-3" />
            Copy as YAML
          </button>
        </div>
      </Section>
      {dialog}
      <PowerCard inSettings />
    </div>
  )
}
