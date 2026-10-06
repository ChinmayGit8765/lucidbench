import { useCallback, useEffect, useState, type ReactNode } from "react"
import {
  Activity,
  Boxes,
  ChevronRight,
  Copy,
  Cpu,
  FileKey2,
  ListChecks,
  Play,
  RefreshCw,
  Server,
  type LucideIcon,
} from "lucide-react"

import { copyText, CopyCommand } from "@/components/CopyCommand"
import { PowerCard } from "@/components/Power"
import { PageHeader } from "@/components/Shell"
import { Badge, StatusPill, type Tone } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card } from "@/components/ui/card"
import { Sheet } from "@/components/ui/dialog"
import { EmptyState, ErrorState, Skeleton } from "@/components/ui/states"
import { ApiError, request, usePoll, type Polled } from "@/lib/api"
import { useApp } from "@/lib/app"
import type { Health } from "@/lib/health"
import { runHelloJob, type ClusterInfo, type Job } from "@/lib/jobs"
import { isAsleep, usePower } from "@/lib/power"
import { absoluteTime, relativeTime, useNow } from "@/lib/time"
import { cn } from "@/lib/utils"
import type { ModulePageProps } from "@/modules/types"

const JOB_TONE: Record<string, Tone> = {
  Completed: "success",
  Running: "info",
  Failed: "danger",
  Pending: "warning",
}

const CLUSTER_UP = "lucid cluster up"

function JobStatus({ status }: { status: string }) {
  const tone = JOB_TONE[status] ?? "neutral"
  return (
    <StatusPill tone={tone} pulse={status === "Running"}>
      {status || "Unknown"}
    </StatusPill>
  )
}

function Tile({
  icon: Icon,
  label,
  loading,
  value,
  sub,
  status,
}: {
  icon: LucideIcon
  label: string
  loading?: boolean
  value: ReactNode
  sub?: ReactNode
  status?: ReactNode
}) {
  return (
    <Card className="p-4">
      <div className="flex min-h-5 items-center justify-between gap-2">
        <span className="flex items-center gap-2 text-xs font-medium text-muted-foreground">
          <Icon className="size-3.5 text-subtle-foreground" />
          {label}
        </span>
        {!loading && status}
      </div>
      {loading ? (
        <div className="mt-3 space-y-2">
          <Skeleton className="h-6 w-24" />
          <Skeleton className="h-3.5 w-32" />
        </div>
      ) : (
        <>
          <div className="mt-2.5 truncate text-xl font-semibold tabular-nums tracking-tight">{value}</div>
          {sub && <div className="mt-0.5 truncate text-xs text-subtle-foreground">{sub}</div>}
        </>
      )}
    </Card>
  )
}

function Tiles({
  health,
  cluster,
  jobs,
  asleep,
}: {
  health: Health | null | undefined
  cluster: Polled<ClusterInfo>
  jobs: Polled<Job[]>
  asleep: boolean
}) {
  const c = cluster.data
  const list = jobs.data ?? []
  const done = list.filter((j) => j.status === "Completed").length
  const failed = list.filter((j) => j.status === "Failed").length
  const clusterLoading = cluster.loading && !c
  return (
    <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
      <Tile
        icon={Cpu}
        label="Daemon"
        loading={health === undefined}
        value={health ? "Online" : "Unreachable"}
        sub={health ? <span className="font-mono">lucidd {health.version}</span> : "Start lucidd to continue"}
        status={<StatusPill tone={health ? "success" : "danger"}>{health ? "ok" : "down"}</StatusPill>}
      />
      <Tile
        icon={Server}
        label="Cluster"
        loading={clusterLoading}
        value={c ? (asleep ? "Asleep" : c.running ? "Running" : "Stopped") : "Unavailable"}
        sub={c ? <span className="font-mono">kind · {c.name}</span> : cluster.error?.message}
        status={
          c ? (
            <StatusPill tone={c.running && !asleep ? "success" : "neutral"}>{asleep ? "stopped" : c.running ? "up" : "down"}</StatusPill>
          ) : (
            <StatusPill tone="danger">error</StatusPill>
          )
        }
      />
      <Tile
        icon={FileKey2}
        label="Kubeconfig"
        loading={clusterLoading}
        value={c?.kubeconfig_present ? "Present" : "Missing"}
        sub={c?.kubeconfig_present ? "Private to Lucidbench" : "Written by lucid cluster up"}
        status={
          <StatusPill tone={c?.kubeconfig_present ? "success" : "warning"}>
            {c?.kubeconfig_present ? "ok" : "missing"}
          </StatusPill>
        }
      />
      <Tile
        icon={ListChecks}
        label="Jobs"
        loading={jobs.loading && !jobs.data && !jobs.error}
        value={jobs.data ? list.length : "-"}
        sub={
          jobs.data ? (
            <span className="tabular-nums">
              {done} completed{failed > 0 && ` · ${failed} failed`}
            </span>
          ) : (
            "Needs a running cluster"
          )
        }
      />
    </div>
  )
}

function NoCluster() {
  return (
    <div className="space-y-3 rounded-lg border border-dashed p-4">
      <p className="text-sm text-muted-foreground">
        The local cluster is not running. Create it from a terminal, then jobs appear here.
      </p>
      <CopyCommand command={CLUSTER_UP} />
    </div>
  )
}

function LogViewer({ job, onClose }: { job: Job | null; onClose: () => void }) {
  const name = job?.name ?? ""
  const [text, setText] = useState<string | null>(null)
  const [err, setErr] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const load = useCallback(async () => {
    if (!name) return
    setErr(null)
    setBusy(true)
    try {
      setText(await (await request(`/api/jobs/${encodeURIComponent(name)}/logs`)).text())
    } catch (e) {
      setText(null)
      setErr(e instanceof ApiError ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }, [name])
  useEffect(() => {
    setText(null)
    void load()
  }, [load])

  const lines = text === null ? [] : text.replace(/\n$/, "").split("\n")
  return (
    <Sheet
      open={job !== null}
      onClose={onClose}
      title={<span className="font-mono">{name}</span>}
      description={
        job && (
          <span className="flex flex-wrap items-center gap-2">
            <JobStatus status={job.status} />
            <span className="font-mono">{job.image}</span>
            <span title={absoluteTime(job.createdAt)}>{absoluteTime(job.createdAt)}</span>
          </span>
        )
      }
      actions={
        <>
          <Button variant="ghost" size="sm" onClick={() => void load()} disabled={busy}>
            <RefreshCw className={cn(busy && "animate-spin")} /> Reload
          </Button>
          <Button
            variant="ghost"
            size="sm"
            disabled={!text}
            onClick={() => text && void copyText(text, "Logs copied")}
          >
            <Copy /> Copy
          </Button>
        </>
      }
    >
      <div className="p-4">
        {err ? (
          <ErrorState title="Could not load logs" message={err} onRetry={() => void load()} />
        ) : text === null ? (
          <div className="space-y-2 p-2">
            <Skeleton className="h-3.5 w-3/4" />
            <Skeleton className="h-3.5 w-1/2" />
            <Skeleton className="h-3.5 w-2/3" />
          </div>
        ) : text.trim() === "" ? (
          <p className="p-6 text-center text-sm text-muted-foreground">This job wrote no output.</p>
        ) : (
          <pre className="overflow-x-auto rounded-lg border bg-background py-3 font-mono text-xs leading-5">
            {lines.map((l, i) => (
              <div key={i} className="flex hover:bg-accent/40">
                <span className="w-10 shrink-0 select-none pr-3 text-right tabular-nums text-subtle-foreground/70">
                  {i + 1}
                </span>
                <span className="whitespace-pre-wrap break-all pr-4">{l || " "}</span>
              </div>
            ))}
          </pre>
        )}
      </div>
    </Sheet>
  )
}

function JobsTable({ jobs, onOpen }: { jobs: Job[]; onOpen: (j: Job) => void }) {
  const now = useNow()
  const sorted = [...jobs].sort((a, b) => Date.parse(b.createdAt) - Date.parse(a.createdAt))
  return (
    <div className="overflow-x-auto">
      <table className="w-full text-sm">
        <thead>
          <tr className="border-y bg-muted/40 text-left text-2xs font-medium uppercase tracking-[0.06em] text-subtle-foreground">
            <th className="px-5 py-2 font-medium">Name</th>
            <th className="px-3 py-2 font-medium">Status</th>
            <th className="px-3 py-2 font-medium max-md:hidden">Image</th>
            <th className="px-3 py-2 text-right font-medium">Created</th>
            <th className="w-10 px-3 py-2" />
          </tr>
        </thead>
        <tbody>
          {sorted.map((j) => (
            <tr
              key={j.name}
              tabIndex={0}
              onClick={() => onOpen(j)}
              onKeyDown={(e) => (e.key === "Enter" || e.key === " ") && (e.preventDefault(), onOpen(j))}
              aria-label={`Open logs for ${j.name}`}
              className="group cursor-pointer border-b transition-colors last:border-b-0 hover:bg-accent/50 focus-visible:bg-accent/60 focus-visible:outline-none"
            >
              <td className="px-5 py-2.5 font-mono text-xs">{j.name}</td>
              <td className="px-3 py-2.5">
                <JobStatus status={j.status} />
              </td>
              <td className="px-3 py-2.5 max-md:hidden">
                <Badge className="font-mono">{j.image}</Badge>
              </td>
              <td className="px-3 py-2.5 text-right text-xs tabular-nums whitespace-nowrap text-muted-foreground">
                <time dateTime={j.createdAt} title={absoluteTime(j.createdAt)}>
                  {relativeTime(j.createdAt, now)}
                </time>
              </td>
              <td className="px-3 py-2.5">
                <ChevronRight className="size-4 text-subtle-foreground transition-transform group-hover:translate-x-0.5 group-hover:text-foreground" />
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

function Sleeping({ manual }: { manual?: boolean }) {
  return (
    <div className="rounded-lg border border-dashed p-4 text-sm text-muted-foreground">
      {manual
        ? "The cluster is stopped, and power.cluster is off, so a job does not start it. Start it from the Power card above."
        : "The cluster is asleep. Running a job starts it first, which usually takes under a minute."}
    </div>
  )
}

function JobsCard({
  jobs,
  clusterRunning,
  asleep,
  stoppedManual,
}: {
  jobs: Polled<Job[]>
  clusterRunning?: boolean
  /** Stopped, and a job wakes it. */
  asleep?: boolean
  /** Stopped in off mode: only Start wakes it. */
  stoppedManual?: boolean
}) {
  const { data, error, loading, refresh } = jobs
  const [selected, setSelected] = useState<Job | null>(null)
  const [busy, setBusy] = useState(false)

  const runHello = async () => {
    setBusy(true)
    await runHelloJob(asleep)
    setBusy(false)
  }

  const unavailable = !asleep && !stoppedManual && (error?.status === 503 || clusterRunning === false)
  // Keep the open job's status fresh as polls come in.
  const open = selected ? (data?.find((j) => j.name === selected.name) ?? selected) : null

  return (
    <Card className="overflow-hidden">
      <div className="flex flex-wrap items-center justify-between gap-3 p-5">
        <div>
          <h2 className="flex items-center gap-2 text-sm font-semibold">
            Jobs
            {data && data.length > 0 && (
              <span className="rounded-full bg-muted px-1.5 text-2xs tabular-nums text-muted-foreground">
                {data.length}
              </span>
            )}
          </h2>
          <p className="mt-0.5 text-sm text-muted-foreground">Batch jobs on the local cluster. Select one to read its logs.</p>
        </div>
        <div className="flex items-center gap-2">
          <Button variant="ghost" size="icon" onClick={refresh} aria-label="Refresh jobs" title="Refresh jobs">
            <RefreshCw />
          </Button>
          <Button onClick={runHello} disabled={busy || unavailable || stoppedManual}>
            <Play /> {busy ? (asleep ? "Waking the cluster" : "Submitting") : "Run hello job"}
          </Button>
        </div>
      </div>

      {loading && !data && !error && (
        <div className="space-y-2 border-t p-5">
          {[0, 1, 2].map((i) => (
            <Skeleton key={i} className="h-8" />
          ))}
        </div>
      )}
      {unavailable && (
        <div className="border-t p-5">
          <NoCluster />
        </div>
      )}
      {(asleep || stoppedManual) && (
        <div className="border-t p-5">
          <Sleeping manual={stoppedManual} />
        </div>
      )}
      {!unavailable && !asleep && !stoppedManual && error && !data && (
        <div className="border-t p-5">
          <ErrorState title="Could not load jobs" message={error.message} onRetry={refresh} />
        </div>
      )}
      {!unavailable && data && data.length === 0 && (
        <div className="border-t">
          <EmptyState
            icon={<Boxes />}
            title="No jobs yet"
            description="Run the hello job to check the cluster end to end. It prints a greeting and exits."
          />
        </div>
      )}
      {!unavailable && data && data.length > 0 && <JobsTable jobs={data} onOpen={setSelected} />}

      <LogViewer job={open} onClose={() => setSelected(null)} />
    </Card>
  )
}

export default function System({ subpath }: ModulePageProps) {
  const { health } = useApp()
  const cluster = usePoll<ClusterInfo>("/api/cluster")
  const jobs = usePoll<Job[]>("/api/jobs")
  const power = usePower()
  // A stopped node that a job may wake: on demand or always, not off.
  const nodeDown = !!power.data && isAsleep(power.data.cluster)
  const asleep = nodeDown && power.data?.modes.cluster !== "off"
  const focusPower = subpath[0] === "power"
  const powerLoaded = power.data !== null
  useEffect(() => {
    if (focusPower && powerLoaded) document.getElementById("power")?.scrollIntoView({ behavior: "smooth", block: "start" })
  }, [focusPower, powerLoaded])
  return (
    <div className="space-y-6">
      <PageHeader icon={<Activity />} title="System" description="The Lucidbench daemon, the local kind cluster and the jobs it runs." />
      <Tiles health={health} cluster={cluster} jobs={jobs} asleep={nodeDown} />
      {cluster.error && !cluster.data && (
        <ErrorState
          title={cluster.error.status === 503 ? "Docker is not reachable" : "Could not load cluster status"}
          message={cluster.error.message}
          onRetry={cluster.refresh}
        />
      )}
      <PowerCard focus={focusPower} />
      <JobsCard jobs={jobs} clusterRunning={cluster.data?.running} asleep={asleep} stoppedManual={nodeDown && !asleep} />
    </div>
  )
}
