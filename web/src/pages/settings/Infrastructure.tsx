import { useEffect, useState } from "react"
import { Copy, RotateCcw } from "lucide-react"

import { copyText, CopyCommand } from "@/components/CopyCommand"
import { PowerCard } from "@/components/Power"
import { StatusPill } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { ErrorState, Skeleton } from "@/components/ui/states"
import { usePoll } from "@/lib/api"
import type { PowerMode } from "@/lib/power"
import { Row, Section, Segmented } from "@/pages/settings/controls"

/** The power section of GET /api/config. */
interface PowerConfig {
  cluster: PowerMode
  cluster_idle_minutes: number
  runners: PowerMode
  runner_idle_minutes: number
  stacks: { project: string; mode: PowerMode }[]
  poll_seconds: number
}

interface ConfigView {
  power: PowerConfig
  sources: Record<string, string>
}

const MODES: { id: PowerMode; label: string; title: string }[] = [
  { id: "always", label: "Always on", title: "Lucidbench never stops it" },
  { id: "on-demand", label: "On demand", title: "Started when needed, stopped when idle" },
  { id: "off", label: "Manual", title: "Lucidbench never starts or stops it on its own" },
]

/** The power: block for config.yaml, in the same shape as config.example.yaml. */
function snippet(p: PowerConfig): string {
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

const same = (a: PowerConfig, b: PowerConfig) => snippet(a) === snippet(b)

/**
 * Settings › Infrastructure: when the cluster, runners and stacks run. The
 * daemon reads config at start and has no write route, so the controls
 * build the exact power: block to paste into config.yaml, then a restart
 * applies it (as the vault picker does).
 */
export function Infrastructure() {
  const cfg = usePoll<ConfigView>("/api/config", 60000)
  const current = cfg.data?.power
  const [draft, setDraft] = useState<PowerConfig | null>(null)
  useEffect(() => {
    if (current && !draft) setDraft(current)
  }, [current, draft])
  if (cfg.error && !current) return <ErrorState title="Could not read the configuration" message={cfg.error.message} onRetry={cfg.refresh} />
  if (!current || !draft) return <Skeleton className="h-64 rounded-xl" />
  const set = (patch: Partial<PowerConfig>) => setDraft({ ...draft, ...patch })
  const changed = !same(draft, current)
  const src = (k: string) => cfg.data?.sources[`power.${k}`] ?? "default"
  const fromEnv = ["cluster", "runners"].filter((k) => src(k).startsWith("env:"))
  return (
    <div className="space-y-5">
      <Section
        title="On-demand infrastructure"
        description="Run the local cluster, runner containers and stacks only when something needs them. Idle things are stopped to free memory; Start and Stop work in every mode."
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
          hint={
            draft.stacks.length
              ? draft.stacks.map((s) => `${s.project} (${s.mode})`).join(", ")
              : "None listed. Add compose projects to power.stacks to start and stop them as a group."
          }
        >
          <StatusPill tone="neutral">{draft.stacks.length} listed</StatusPill>
        </Row>
        <div className="space-y-3 border-t px-5 py-4">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <div className="text-sm font-medium">{changed ? "Apply these settings" : "Your current settings"}</div>
            <div className="flex items-center gap-2">
              {changed && (
                <Button variant="ghost" size="sm" onClick={() => setDraft(current)}>
                  <RotateCcw /> Reset
                </Button>
              )}
              <Button variant="secondary" size="sm" onClick={() => void copyText(snippet(draft), "Config copied")}>
                <Copy /> Copy
              </Button>
            </div>
          </div>
          <ol className="list-decimal space-y-2 pl-5 text-sm text-muted-foreground">
            <li>
              Find your config file:
              <CopyCommand command="lucid config path" className="mt-1.5" />
            </li>
            <li>
              Replace its <code className="font-mono text-xs">power:</code> block with this (add it if there is none):
              <pre className="mt-1.5 overflow-x-auto rounded-lg border bg-background px-3 py-2 font-mono text-xs text-foreground">{snippet(draft)}</pre>
            </li>
            <li>Restart Lucidbench. The daemon reads its config at start; it never writes the file.</li>
          </ol>
          {fromEnv.length > 0 && (
            <p className="text-xs text-warning-fg">
              {fromEnv.map((k) => `power.${k}`).join(" and ")} {fromEnv.length === 1 ? "is" : "are"} set by {fromEnv.map((k) => src(k).slice(4)).join(", ")}, which
              overrides the file.
            </p>
          )}
        </div>
      </Section>
      <PowerCard inSettings />
    </div>
  )
}
