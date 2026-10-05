import { useMemo, useState, type ReactNode } from "react"
import {
  Box,
  Container as ContainerIcon,
  Ellipsis,
  HardDrive,
  Layers,
  Lock,
  Play,
  RotateCw,
  ScrollText,
  Ship,
  Square,
} from "lucide-react"

import { CopyCommand } from "@/components/CopyCommand"
import { LogDrawer } from "@/components/LogDrawer"
import { PageHeader, RefreshButton } from "@/components/Shell"
import { Badge, StatusPill, type Tone } from "@/components/ui/badge"
import { Card } from "@/components/ui/card"
import { ConfirmDialog, type ConfirmRequest } from "@/components/ui/confirm"
import { Menu, type MenuItem } from "@/components/ui/menu"
import { EmptyState, ErrorState, Skeleton } from "@/components/ui/states"
import { Tabs } from "@/components/ui/tabs"
import { refreshAll, usePoll } from "@/lib/api"
import { useApp } from "@/lib/app"
import {
  DOCKER_POLL_MS,
  dockerAction,
  percent,
  shortStatus,
  statFor,
  type DockerAction,
  type DockerContainer,
  type DockerContainers,
  type DockerGroup,
  type DockerImage,
  type DockerStat,
  type DockerVolume,
} from "@/lib/docker"
import { usePrefs } from "@/lib/prefs"
import { absoluteTime, relativeTime, useNow } from "@/lib/time"
import { cn } from "@/lib/utils"
import type { ModulePageProps } from "@/modules/types"

const STATE_TONE: Record<string, Tone> = {
  running: "success",
  restarting: "info",
  paused: "warning",
  exited: "neutral",
  created: "neutral",
  dead: "danger",
}

const GROUP_ICON = { compose: Layers, kind: Ship, standalone: Box }

/* ---------- summary ---------- */

function Cell({ label, value, sub }: { label: string; value: ReactNode; sub?: ReactNode }) {
  return (
    <div className="min-w-0 px-4 py-3.5">
      <div className="text-2xs font-medium uppercase tracking-[0.08em] text-subtle-foreground">{label}</div>
      <div className="mt-1 truncate text-xl font-semibold tabular-nums tracking-tight">{value}</div>
      {sub && <div className="mt-0.5 truncate text-xs text-muted-foreground">{sub}</div>}
    </div>
  )
}

function Summary({ d, stats, images, volumes }: { d: DockerContainers; stats: DockerStat[] | null; images?: number; volumes?: number }) {
  const cpu = (stats ?? []).reduce((n, s) => n + percent(s.cpu), 0)
  const busiest = [...(stats ?? [])].sort((a, b) => percent(b.cpu) - percent(a.cpu))[0]
  return (
    <Card className="grid grid-cols-2 divide-x divide-y overflow-hidden @3xl:grid-cols-4 @3xl:divide-y-0">
      <Cell
        label="Running"
        value={
          <span>
            {d.running}
            <span className="text-base font-normal text-subtle-foreground">/{d.total}</span>
          </span>
        }
        sub={d.total - d.running > 0 ? `${d.total - d.running} stopped` : "all running"}
      />
      <Cell
        label="Projects"
        value={d.groups.filter((g) => g.kind === "compose").length}
        sub={`${d.groups.filter((g) => g.kind === "kind").length} kind cluster · ${d.groups.find((g) => g.kind === "standalone")?.containers.length ?? 0} standalone`}
      />
      <Cell label="CPU" value={stats ? `${cpu.toFixed(1)}%` : "-"} sub={busiest ? `busiest: ${busiest.name}` : "sampling…"} />
      <Cell label="Images · volumes" value={`${images ?? "-"} · ${volumes ?? "-"}`} sub="read-only" />
    </Card>
  )
}

/* ---------- containers ---------- */

function Meter({ value, label, tone }: { value: number; label: string; tone: string }) {
  return (
    <div className="flex items-center gap-1.5" title={label}>
      <span className="h-1 w-14 overflow-hidden rounded-full bg-muted">
        <span className={cn("block h-full rounded-full transition-[width] duration-500", tone)} style={{ width: `${Math.min(100, Math.max(value > 0 ? 3 : 0, value))}%` }} />
      </span>
      <span className="w-11 text-right text-2xs tabular-nums text-muted-foreground">{value.toFixed(value < 10 ? 1 : 0)}%</span>
    </div>
  )
}

function GroupCard({
  g,
  stats,
  onLogs,
  onAction,
}: {
  g: DockerGroup
  stats: DockerStat[] | null
  onLogs: (c: DockerContainer) => void
  onAction: (c: DockerContainer, a: DockerAction) => void
}) {
  const now = useNow()
  const { label } = usePrefs()
  const Icon = GROUP_ICON[g.kind]
  const up = g.containers.filter((c) => c.state === "running").length
  const controllable = g.containers.some((c) => c.actions.length > 0)
  const title = g.kind === "standalone" ? "Standalone containers" : g.project
  return (
    <Card className="overflow-hidden">
      <div className="flex flex-wrap items-center justify-between gap-3 px-5 pb-3 pt-4">
        <div className="flex min-w-0 items-center gap-2.5">
          <span className="flex size-7 items-center justify-center rounded-md border bg-background/60 text-subtle-foreground">
            <Icon className="size-3.5" />
          </span>
          <h2 className="truncate text-sm font-semibold">{title}</h2>
          <Badge>{g.kind === "kind" ? "kind cluster" : g.kind === "compose" ? "compose" : "no project"}</Badge>
          {!controllable && (
            <span className="flex items-center gap-1 text-2xs text-subtle-foreground" title="Lucidbench only acts on its own project, runner containers, kind nodes and docker.allowed_projects">
              <Lock className="size-3" /> read-only
            </span>
          )}
        </div>
        <span className="text-xs tabular-nums text-subtle-foreground">
          {up}/{g.containers.length} running
        </span>
      </div>
      <div className="overflow-x-auto">
        <table className="w-full table-fixed text-sm">
          {/* Fixed widths keep the columns aligned from one project card to the next. */}
          <colgroup>
            <col className="w-[28%]" />
            <col className="w-28" />
            <col />
            <col className="w-[17%]" />
            <col className="w-32" />
            <col className="w-36" />
            <col className="w-12" />
          </colgroup>
          <thead>
            <tr className="border-y bg-muted/40 text-left text-2xs font-medium uppercase tracking-[0.06em] text-subtle-foreground">
              <th className="px-5 py-2 font-medium">Name</th>
              <th className="px-3 py-2 font-medium">State</th>
              <th className="px-3 py-2 font-medium">Image</th>
              <th className="px-3 py-2 font-medium">Ports</th>
              <th className="px-3 py-2 font-medium">{label("usage_meter", "Usage")}</th>
              <th className="px-3 py-2 text-right font-medium">Started</th>
              <th className="w-10 px-3 py-2" />
            </tr>
          </thead>
          <tbody>
            {g.containers.map((c) => {
              const s = statFor(stats, c)
              const running = c.state === "running"
              const items: MenuItem[] = [
                { label: "View logs", icon: ScrollText, onSelect: () => onLogs(c) },
                ...c.actions.map((a) => ({
                  label: a === "start" ? "Start" : a === "stop" ? "Stop" : "Restart",
                  icon: a === "start" ? Play : a === "stop" ? Square : RotateCw,
                  danger: a === "stop",
                  onSelect: () => onAction(c, a),
                })),
              ]
              if (c.actions.length === 0) items.push({ label: "Read-only container", icon: Lock, disabled: true, onSelect: () => {} })
              return (
                <tr key={c.id} className="group border-b transition-colors last:border-b-0 hover:bg-accent/40">
                  <td className="px-5 py-2">
                    <button onClick={() => onLogs(c)} className="block max-w-full text-left outline-none focus-visible:underline" title={`${c.name}: view logs`}>
                      <span className="block truncate font-mono text-xs">{c.name}</span>
                      {c.compose_service && <span className="block truncate text-2xs text-subtle-foreground">{c.compose_service}</span>}
                    </button>
                  </td>
                  <td className="px-3 py-2">
                    <StatusPill tone={STATE_TONE[c.state] ?? "neutral"} pulse={c.state === "restarting"}>
                      {c.state}
                    </StatusPill>
                  </td>
                  <td className="px-3 py-2">
                    <Badge className="max-w-full font-mono" title={c.image}>
                      <span className="truncate">{c.image.replace(/@sha256:.*/, "")}</span>
                    </Badge>
                  </td>
                  <td className="truncate px-3 py-2 font-mono text-2xs text-muted-foreground" title={c.ports}>
                    {c.ports ? c.ports.replace(/0\.0\.0\.0:|\[::\]:/g, "") : <span className="text-subtle-foreground">-</span>}
                  </td>
                  <td className="px-3 py-2">
                    {running && s ? (
                      <div className="space-y-0.5">
                        <Meter value={percent(s.cpu)} label={`CPU ${s.cpu}`} tone="bg-brand" />
                        <Meter value={percent(s.mem)} label={`Memory ${s.mem_usage}`} tone="bg-[var(--brand-2)]" />
                      </div>
                    ) : running ? (
                      <Skeleton className="h-4 w-24" />
                    ) : (
                      <span className="text-2xs text-subtle-foreground">-</span>
                    )}
                  </td>
                  <td className="truncate whitespace-nowrap px-3 py-2 text-right text-xs tabular-nums text-muted-foreground">
                    {running && c.started_at ? (
                      <time dateTime={c.started_at} title={absoluteTime(c.started_at)}>
                        {relativeTime(c.started_at, now)}
                      </time>
                    ) : (
                      <span title={c.status}>{shortStatus(c.status)}</span>
                    )}
                  </td>
                  <td className="px-3 py-2">
                    <Menu label={`Actions for ${c.name}`} trigger={<Ellipsis />} items={items} />
                  </td>
                </tr>
              )
            })}
          </tbody>
        </table>
      </div>
    </Card>
  )
}

/* ---------- images and volumes ---------- */

function Table({ head, children }: { head: string[]; children: ReactNode }) {
  return (
    <Card className="overflow-hidden">
      <div className="overflow-x-auto">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b bg-muted/40 text-left text-2xs font-medium uppercase tracking-[0.06em] text-subtle-foreground">
              {head.map((h, i) => (
                <th key={h} className={cn("py-2 font-medium", i === 0 ? "px-5" : "px-3", i === head.length - 1 && "text-right")}>
                  {h}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>{children}</tbody>
        </table>
      </div>
    </Card>
  )
}

function Images({ images }: { images: DockerImage[] }) {
  if (images.length === 0) return <EmptyState icon={<Layers />} title="No images" description="Images you pull or build show up here." />
  return (
    <Table head={["Repository", "Tag", "Image ID", "Size", "Created"]}>
      {images.map((i) => (
        <tr key={`${i.id}-${i.repository}-${i.tag}`} className="border-b last:border-b-0 hover:bg-accent/40">
          <td className="max-w-80 truncate px-5 py-2 font-mono text-xs">{i.repository}</td>
          <td className="px-3 py-2">
            <Badge className="font-mono">{i.tag}</Badge>
          </td>
          <td className="px-3 py-2 font-mono text-2xs text-muted-foreground">{i.id.replace("sha256:", "").slice(0, 12)}</td>
          <td className="px-3 py-2 text-xs tabular-nums text-muted-foreground">{i.size}</td>
          <td className="px-3 py-2 text-right text-xs text-muted-foreground">{i.created_since}</td>
        </tr>
      ))}
    </Table>
  )
}

function Volumes({ volumes }: { volumes: DockerVolume[] }) {
  if (volumes.length === 0) return <EmptyState icon={<HardDrive />} title="No volumes" description="Named and anonymous volumes show up here." />
  const sortedVolumes = [...volumes].sort((a, b) => Number(a.anonymous) - Number(b.anonymous) || a.name.localeCompare(b.name))
  return (
    <Table head={["Name", "Project", "Driver", "Kind"]}>
      {sortedVolumes.map((v) => (
        <tr key={v.name} className="border-b last:border-b-0 hover:bg-accent/40">
          <td className="max-w-96 truncate px-5 py-2 font-mono text-xs" title={v.name}>
            {v.anonymous ? `${v.name.slice(0, 12)}…` : v.name}
          </td>
          <td className="px-3 py-2 text-xs text-muted-foreground">{v.compose_project ?? "-"}</td>
          <td className="px-3 py-2 text-xs text-muted-foreground">{v.driver}</td>
          <td className="px-3 py-2 text-right">
            <StatusPill tone={v.anonymous ? "neutral" : "info"}>{v.anonymous ? "anonymous" : "named"}</StatusPill>
          </td>
        </tr>
      ))}
    </Table>
  )
}

/* ---------- page ---------- */

const VERBS: Record<DockerAction, string> = { start: "Start", stop: "Stop", restart: "Restart" }

export default function Containers({ subpath }: ModulePageProps) {
  const { navigate } = useApp()
  const tab = subpath[0] === "images" || subpath[0] === "volumes" ? subpath[0] : "containers"
  const containers = usePoll<DockerContainers>("/api/docker/containers", DOCKER_POLL_MS)
  const stats = usePoll<DockerStat[]>("/api/docker/stats", DOCKER_POLL_MS)
  const images = usePoll<DockerImage[]>("/api/docker/images", 60000)
  const volumes = usePoll<DockerVolume[]>("/api/docker/volumes", 60000)
  const [logs, setLogs] = useState<DockerContainer | null>(null)
  const [confirm, setConfirm] = useState<ConfirmRequest | null>(null)

  const all = [containers, stats, images, volumes]
  const updated = all.map((p) => p.updatedAt).filter((t): t is number => t !== null)
  const d = containers.data
  const groups = useMemo(() => d?.groups ?? [], [d])

  const act = (c: DockerContainer, a: DockerAction) =>
    setConfirm({
      title: `${VERBS[a]} ${c.name}?`,
      description:
        c.kind_cluster !== undefined && c.kind_cluster !== ""
          ? `This restarts a node of the ${c.kind_cluster} cluster. Pods on it restart too.`
          : a === "stop"
            ? "The container stops. Anything it serves goes offline until you start it again."
            : a === "restart"
              ? "The container stops and starts again."
              : "The container starts with its existing configuration.",
      confirmLabel: VERBS[a],
      danger: a === "stop",
      run: async () => {
        if (await dockerAction(c.name, a)) setTimeout(refreshAll, 800)
      },
    })

  return (
    <div className="space-y-6">
      <PageHeader
        icon={<ContainerIcon />}
        title="Containers"
        description="Every container on this machine's Docker engine, grouped by compose project. Lucidbench controls only its own project, runner containers, kind nodes and the projects you allow."
        actions={<RefreshButton refreshing={all.some((p) => p.refreshing)} updatedAt={updated.length ? Math.min(...updated) : null} />}
      />

      {containers.loading && !d && !containers.error && (
        <div className="space-y-4">
          <Skeleton className="h-[88px] rounded-xl" />
          <Skeleton className="h-48 rounded-xl" />
        </div>
      )}

      {containers.error && !d && (
        <Card className="border-dashed">
          <EmptyState
            icon={<ContainerIcon />}
            title="Docker is not reachable"
            description="Start Docker Desktop (or the docker service) and this page fills in within seconds."
          >
            <CopyCommand command="docker info" />
          </EmptyState>
          <div className="px-6 pb-6">
            <ErrorState title="What the engine said" message={containers.error.message} onRetry={containers.refresh} />
          </div>
        </Card>
      )}

      {d && (
        <>
          <Summary d={d} stats={stats.data} images={images.data?.length} volumes={volumes.data?.length} />
          <Tabs
            label="Docker"
            value={tab}
            onChange={(t) => navigate(t === "containers" ? "/containers" : `/containers/${t}`)}
            items={[
              { id: "containers", label: "Containers", icon: ContainerIcon, count: d.total },
              { id: "images", label: "Images", icon: Layers, count: images.data?.length },
              { id: "volumes", label: "Volumes", icon: HardDrive, count: volumes.data?.length },
            ]}
          />
          {tab === "containers" &&
            (groups.length === 0 ? (
              <Card className="border-dashed">
                <EmptyState icon={<ContainerIcon />} title="No containers" description="Containers you run with docker or compose show up here." />
              </Card>
            ) : (
              <div className="space-y-4">
                {groups.map((g) => (
                  <GroupCard key={`${g.kind}/${g.project}`} g={g} stats={stats.data} onLogs={setLogs} onAction={act} />
                ))}
              </div>
            ))}
          {tab === "images" &&
            (images.error && !images.data ? (
              <ErrorState title="Could not list images" message={images.error.message} onRetry={images.refresh} />
            ) : images.data ? (
              <Images images={images.data} />
            ) : (
              <Skeleton className="h-48 rounded-xl" />
            ))}
          {tab === "volumes" &&
            (volumes.error && !volumes.data ? (
              <ErrorState title="Could not list volumes" message={volumes.error.message} onRetry={volumes.refresh} />
            ) : volumes.data ? (
              <Volumes volumes={volumes.data} />
            ) : (
              <Skeleton className="h-48 rounded-xl" />
            ))}
        </>
      )}

      <LogDrawer
        url={logs ? `/api/docker/containers/${encodeURIComponent(logs.name)}/logs?tail=200` : null}
        title={<span className="font-mono">{logs?.name}</span>}
        description={
          logs && (
            <span className="flex flex-wrap items-center gap-2">
              <StatusPill tone={STATE_TONE[logs.state] ?? "neutral"}>{logs.state}</StatusPill>
              <span className="font-mono">{logs.image.replace(/@sha256:.*/, "")}</span>
              <span>last 200 lines</span>
            </span>
          )
        }
        onClose={() => setLogs(null)}
      />
      <ConfirmDialog request={confirm} onClose={() => setConfirm(null)} />
    </div>
  )
}
