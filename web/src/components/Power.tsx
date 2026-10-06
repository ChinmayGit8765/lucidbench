import { useState } from "react"
import { Container, Layers, Moon, Play, Power as PowerIcon, ServerCog, Ship, Square, TriangleAlert } from "lucide-react"

import { StatTile } from "@/components/StatTile"
import { Badge, StatusPill } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card } from "@/components/ui/card"
import { ConfirmDialog, type ConfirmRequest } from "@/components/ui/confirm"
import { EmptyState, ErrorState, Skeleton } from "@/components/ui/states"
import { useApp } from "@/lib/app"
import {
  allItems,
  formatBytes,
  formatSeconds,
  isAsleep,
  MODE_LABEL,
  powerAction,
  sleepIdle,
  STATE_TONE,
  usePower,
  type PowerItem,
  type PowerKind,
  type PowerState,
} from "@/lib/power"
import { absoluteTime, relativeTime, useNow } from "@/lib/time"
import { cn } from "@/lib/utils"

const KIND_ICON: Record<PowerKind, typeof Ship> = { cluster: Ship, runner: ServerCog, stack: Layers }

/** The confirm request for "Sleep everything idle". */
export function sleepRequest(p: PowerState | null | undefined): ConfirmRequest {
  const onDemand = allItems(p).filter((it) => it.mode === "on-demand" && (it.state === "running" || it.state === "partial"))
  return {
    title: "Sleep everything idle?",
    description:
      onDemand.length > 0
        ? `Stops what is idle now among ${onDemand.map((it) => it.name).join(", ")}, without waiting for the idle timeout. Anything with running pods, a busy runner or a queued run stays up. Always-on and manual things are left alone.`
        : "Nothing on demand is running right now. Always-on and manual things are never stopped by this.",
    confirmLabel: "Sleep idle things",
    run: () => sleepIdle(),
  }
}

/** Overview tile: what is running, what sleeps, the memory in use. */
export function PowerTile() {
  const { open } = useApp()
  const power = usePower()
  const [confirm, setConfirm] = useState<ConfirmRequest | null>(null)
  const p = power.data
  const items = allItems(p)
  const failing = items.filter((it) => it.error)
  const onDemandUp = items.some((it) => it.mode === "on-demand" && (it.state === "running" || it.state === "partial"))
  return (
    <>
      <StatTile
        icon={PowerIcon}
        label="Power"
        onOpen={() => open("system", ["power"])}
        loading={power.loading && !p && !power.error}
        value={
          p ? (
            <span>
              {p.running}
              <span className="text-base font-normal text-subtle-foreground"> on · {p.sleeping} asleep</span>
            </span>
          ) : (
            <span className="text-base font-medium text-muted-foreground">{power.unavailable ? "Not managed" : "Unavailable"}</span>
          )
        }
        aside={
          failing.length > 0 ? (
            <StatusPill tone="danger">{failing.length} failed</StatusPill>
          ) : items.some((it) => it.state === "starting") ? (
            <StatusPill tone="info" pulse>
              starting
            </StatusPill>
          ) : items.some((it) => it.mode === "on-demand") ? (
            <StatusPill tone="neutral">{items.filter((it) => it.mode === "on-demand").length} on demand</StatusPill>
          ) : undefined
        }
        sub={
          p
            ? p.memory.known
              ? `${formatBytes(p.memory.managed_bytes)} in use by managed containers`
              : "memory unknown"
            : (power.error?.message ?? "")
        }
        footer={
          p && items.length > 0 ? (
            <div className="flex items-end justify-between gap-2">
              <div className="flex min-w-0 flex-wrap gap-1">
                {items.slice(0, 6).map((it) => {
                  const Icon = KIND_ICON[it.kind]
                  return (
                    <span
                      key={`${it.kind}-${it.name}`}
                      title={`${it.name}: ${it.state} · ${MODE_LABEL[it.mode]}`}
                      className={cn(
                        "inline-flex h-5 max-w-28 items-center gap-1 rounded-md border px-1.5 text-2xs",
                        it.state === "running" || it.state === "partial" ? "text-foreground" : "text-subtle-foreground",
                      )}
                    >
                      {isAsleep(it) ? <Moon className="size-3 shrink-0" /> : <Icon className="size-3 shrink-0" />}
                      <span className="truncate font-mono">{it.name}</span>
                    </span>
                  )
                })}
              </div>
              <Button
                variant="secondary"
                size="sm"
                className="relative z-20 shrink-0"
                disabled={!onDemandUp}
                title={onDemandUp ? "Stop idle on-demand things now" : "Nothing on demand is running"}
                onClick={() => setConfirm(sleepRequest(p))}
              >
                <Moon /> Sleep idle
              </Button>
            </div>
          ) : undefined
        }
      />
      <ConfirmDialog request={confirm} onClose={() => setConfirm(null)} />
    </>
  )
}

function ItemRow({ it, now, onAct }: { it: PowerItem; now: number; onAct: (it: PowerItem, action: "start" | "stop") => void }) {
  const Icon = KIND_ICON[it.kind]
  const up = it.state === "running" || it.state === "partial"
  const moving = it.state === "starting" || it.state === "stopping"
  return (
    <li className="flex flex-wrap items-center gap-x-3 gap-y-1 border-t px-5 py-3 text-sm">
      <Icon className="size-4 shrink-0 text-subtle-foreground" />
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-center gap-2">
          <span className="truncate font-mono text-xs font-medium">{it.name}</span>
          <StatusPill tone={it.error ? "danger" : STATE_TONE[it.state]} pulse={moving}>
            {it.error && !moving ? "failed" : it.state}
          </StatusPill>
          <Badge>{MODE_LABEL[it.mode]}</Badge>
          {it.busy && (
            <StatusPill tone="info" pulse>
              busy
            </StatusPill>
          )}
        </div>
        <div className="mt-0.5 truncate text-xs text-muted-foreground">
          {[
            it.kind === "runner" && it.repo,
            it.detail,
            it.stops_in_seconds !== undefined && it.mode === "on-demand" && `sleeps in ${formatSeconds(it.stops_in_seconds)}`,
            it.stops_in_seconds === undefined && it.idle_seconds !== undefined && `idle ${formatSeconds(it.idle_seconds)}`,
            it.memory_bytes > 0 && formatBytes(it.memory_bytes),
          ]
            .filter(Boolean)
            .join(" · ") || (isAsleep(it) ? "Not using memory" : "")}
        </div>
        {it.error && (
          <div className="mt-1 flex items-start gap-1.5 text-xs text-danger-fg">
            <TriangleAlert className="mt-0.5 size-3.5 shrink-0" />
            <span className="break-words">
              {it.error}
              {it.error_at && <span className="text-subtle-foreground"> · {relativeTime(it.error_at, now)}</span>}
            </span>
          </div>
        )}
      </div>
      {up ? (
        <Button variant="secondary" size="sm" disabled={moving || it.busy} onClick={() => onAct(it, "stop")}>
          <Square /> Stop
        </Button>
      ) : (
        <Button variant="secondary" size="sm" disabled={moving || it.state === "missing" || it.state === "unknown"} onClick={() => onAct(it, "start")}>
          <Play /> {moving ? "Starting" : "Start"}
        </Button>
      )}
    </li>
  )
}

/** System page card: every managed thing with Start and Stop, and the activity log. */
export function PowerCard({ focus, inSettings }: { focus?: boolean; inSettings?: boolean }) {
  const { open } = useApp()
  const power = usePower()
  const now = useNow(5000)
  const [confirm, setConfirm] = useState<ConfirmRequest | null>(null)
  const p = power.data
  const items = allItems(p)
  const act = (it: PowerItem, action: "start" | "stop") => {
    if (action === "start") {
      void powerAction(it.kind, it.name, "start")
      return
    }
    setConfirm({
      title: `Stop ${it.name}?`,
      description:
        it.kind === "cluster"
          ? "The kind node stops. Pods and jobs come back when it starts again. A cluster with running pods or active jobs is not stopped."
          : it.kind === "runner"
            ? "The runner container stops and its runner goes offline on GitHub. A runner that is running a job is not stopped."
            : `Every container of the ${it.name} compose project stops.`,
      confirmLabel: "Stop",
      danger: true,
      run: () => powerAction(it.kind, it.name, "stop"),
    })
  }
  return (
    <Card id="power" className={cn("scroll-mt-6 overflow-hidden", focus && "ring-2 ring-brand/40")}>
      <div className="flex flex-wrap items-center justify-between gap-3 p-5">
        <div>
          <h2 className="flex items-center gap-2 text-sm font-semibold">
            <PowerIcon className="size-4 text-subtle-foreground" /> Power
          </h2>
          <p className="mt-0.5 text-sm text-muted-foreground">
            {inSettings ? (
              "Everything Lucidbench starts and stops, with what it did lately."
            ) : (
              <>
                What runs only when needed. Modes and timeouts are in{" "}
                <button className="underline-offset-2 hover:underline" onClick={() => open("settings", ["infrastructure"])}>
                  Settings › Infrastructure
                </button>
                .
              </>
            )}
          </p>
        </div>
        <div className="flex items-center gap-3">
          {p?.memory.known && (
            <span className="text-xs tabular-nums text-muted-foreground" title={`${formatBytes(p.memory.docker_bytes)} used by all running containers`}>
              {formatBytes(p.memory.managed_bytes)} in use
            </span>
          )}
          <Button variant="secondary" size="sm" disabled={!p} onClick={() => setConfirm(sleepRequest(p))}>
            <Moon /> Sleep everything idle
          </Button>
        </div>
      </div>
      {power.loading && !p && !power.error && <Skeleton className="mx-5 mb-5 h-16" />}
      {power.unavailable && (
        <div className="border-t">
          <EmptyState icon={<PowerIcon />} title="Power management is not running" description="This daemon does not run the power supervisor." />
        </div>
      )}
      {power.error && !power.unavailable && !p && (
        <div className="border-t p-5">
          <ErrorState title="Could not read power state" message={power.error.message} onRetry={power.refresh} />
        </div>
      )}
      {p && (
        <>
          {p.errors.map((e) => (
            <div key={e} className="flex items-center gap-2 border-t px-5 py-2 text-xs text-danger-fg">
              <TriangleAlert className="size-3.5" /> {e}
            </div>
          ))}
          {items.length === 0 ? (
            <div className="border-t">
              <EmptyState icon={<Container />} title="Nothing to manage yet" description="Create the cluster, add runner containers or list compose stacks in power.stacks." />
            </div>
          ) : (
            <ul>
              {items.map((it) => (
                <ItemRow key={`${it.kind}-${it.name}`} it={it} now={now} onAct={act} />
              ))}
            </ul>
          )}
          {p.activity.length > 0 && (
            <div className="border-t px-5 py-4">
              <div className="mb-2 text-2xs font-medium uppercase tracking-[0.08em] text-subtle-foreground">Recent activity</div>
              <ol className="space-y-1.5">
                {p.activity.slice(0, 8).map((e, i) => (
                  <li key={`${e.at}-${i}`} className="flex items-baseline gap-2 text-xs">
                    <span className={cn("size-1.5 shrink-0 translate-y-[-1px] rounded-full", e.ok ? "bg-success" : "bg-danger")} />
                    <span className="min-w-0 flex-1 truncate text-muted-foreground">
                      <span className="text-foreground">
                        {e.action === "start" ? "Started" : "Stopped"} {e.kind} <span className="font-mono">{e.name}</span>
                      </span>
                      {e.reason && ` · ${e.reason}`}
                      {e.auto ? " · automatic" : ""}
                      {e.seconds ? ` · ${formatSeconds(e.seconds)}` : ""}
                      {e.restarted ? " · node restarted once" : ""}
                      {e.error && <span className="text-danger-fg"> · {e.error}</span>}
                    </span>
                    <time dateTime={e.at} title={absoluteTime(e.at)} className="shrink-0 tabular-nums text-subtle-foreground">
                      {relativeTime(e.at, now)}
                    </time>
                  </li>
                ))}
              </ol>
            </div>
          )}
        </>
      )}
      <ConfirmDialog request={confirm} onClose={() => setConfirm(null)} />
    </Card>
  )
}
