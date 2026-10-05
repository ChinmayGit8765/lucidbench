import { useState } from "react"
import { Boxes, Ellipsis, ListChecks, Play, ScrollText, Server, Ship, Trash2, TriangleAlert } from "lucide-react"
import { toast } from "sonner"

import { CopyCommand } from "@/components/CopyCommand"
import { LogDrawer } from "@/components/LogDrawer"
import { PageHeader, RefreshButton } from "@/components/Shell"
import { Badge, StatusPill } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card } from "@/components/ui/card"
import { ConfirmDialog, type ConfirmRequest } from "@/components/ui/confirm"
import { Menu, type MenuItem } from "@/components/ui/menu"
import { EmptyState, ErrorState, Skeleton } from "@/components/ui/states"
import { Tabs } from "@/components/ui/tabs"
import { errorMessage, refreshAll, sendJSON, usePoll } from "@/lib/api"
import { useApp } from "@/lib/app"
import { runHelloJob } from "@/lib/jobs"
import {
  age,
  friendlyMemory,
  isTroubled,
  JOB_TONE,
  K8S_POLL_MS,
  PHASE_TONE,
  type K8sEvent,
  type K8sJob,
  type K8sNode,
  type K8sPod,
} from "@/lib/k8s"
import { absoluteTime, relativeTime, useNow } from "@/lib/time"
import { cn } from "@/lib/utils"
import type { ModulePageProps } from "@/modules/types"

const ALL = ""

function NodeCard({ n, now }: { n: K8sNode; now: number }) {
  return (
    <Card className="relative overflow-hidden p-4">
      <div aria-hidden className="tile-glow pointer-events-none absolute inset-0" />
      <div className="relative flex items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="flex items-center gap-2 text-xs font-medium text-muted-foreground">
            <Server className="size-3.5 text-subtle-foreground" /> Node
          </div>
          <div className="mt-1.5 truncate font-mono text-sm font-medium" title={n.name}>
            {n.name}
          </div>
          <div className="mt-1 flex flex-wrap items-center gap-1.5">
            {n.roles.map((r) => (
              <Badge key={r}>{r}</Badge>
            ))}
            <Badge className="font-mono">{n.version}</Badge>
          </div>
        </div>
        <StatusPill tone={n.ready ? "success" : "danger"} pulse={n.ready}>
          {n.ready ? "Ready" : "Not ready"}
        </StatusPill>
      </div>
      <dl className="relative mt-4 grid grid-cols-4 gap-2 text-xs">
        {[
          ["CPU", `${n.cpu} cores`],
          ["Memory", friendlyMemory(n.memory)],
          ["Pods", String(n.pods)],
          ["Age", age(n.created_at, now)],
        ].map(([k, v]) => (
          <div key={k}>
            <dt className="text-2xs uppercase tracking-[0.08em] text-subtle-foreground">{k}</dt>
            <dd className="mt-0.5 font-medium tabular-nums">{v}</dd>
          </div>
        ))}
      </dl>
      {n.pressure.length > 0 && (
        <div className="relative mt-3 flex items-center gap-1.5 text-xs text-warning-fg">
          <TriangleAlert className="size-3.5" /> {n.pressure.join(", ")}
        </div>
      )}
    </Card>
  )
}

function Stat({ label, value, tone }: { label: string; value: number | string; tone?: string }) {
  return (
    <div className="px-4 py-3">
      <div className="text-2xs font-medium uppercase tracking-[0.08em] text-subtle-foreground">{label}</div>
      <div className={cn("mt-1 text-xl font-semibold tabular-nums tracking-tight", tone)}>{value}</div>
    </div>
  )
}

const TH = "py-2 font-medium"

function PodsTable({ pods, showNs, now, onLogs }: { pods: K8sPod[]; showNs: boolean; now: number; onLogs: (p: K8sPod, c: string) => void }) {
  if (pods.length === 0) return <EmptyState icon={<Boxes />} title="No pods" description="Pods in this namespace show up here." />
  return (
    <div className="overflow-x-auto">
      <table className="w-full text-sm">
        <thead>
          <tr className="border-b bg-muted/40 text-left text-2xs font-medium uppercase tracking-[0.06em] text-subtle-foreground">
            <th className={cn(TH, "px-5")}>Pod</th>
            {showNs && <th className={cn(TH, "px-3")}>Namespace</th>}
            <th className={cn(TH, "px-3")}>Phase</th>
            <th className={cn(TH, "px-3 text-right")}>Ready</th>
            <th className={cn(TH, "px-3 text-right")}>Restarts</th>
            <th className={cn(TH, "px-3 max-lg:hidden")}>Node</th>
            <th className={cn(TH, "px-3 text-right")}>Age</th>
            <th className="w-10 px-3 py-2" />
          </tr>
        </thead>
        <tbody>
          {pods.map((p) => {
            const items: MenuItem[] = p.containers.map((c) => ({
              label: p.containers.length > 1 ? `Logs: ${c}` : "View logs",
              icon: ScrollText,
              onSelect: () => onLogs(p, c),
            }))
            return (
              <tr key={`${p.namespace}/${p.name}`} className="border-b transition-colors last:border-b-0 hover:bg-accent/40">
                <td className="max-w-72 px-5 py-2">
                  <button
                    onClick={() => p.containers[0] && onLogs(p, p.containers[0])}
                    className="block max-w-full truncate text-left font-mono text-xs outline-none focus-visible:underline"
                    title={`${p.name}: view logs`}
                  >
                    {p.name}
                  </button>
                </td>
                {showNs && <td className="px-3 py-2 text-xs text-muted-foreground">{p.namespace}</td>}
                <td className="px-3 py-2">
                  <StatusPill tone={isTroubled(p) ? "danger" : (PHASE_TONE[p.phase] ?? "neutral")} pulse={p.phase === "Pending"}>
                    {p.reason && isTroubled(p) ? p.reason : p.phase}
                  </StatusPill>
                </td>
                <td className="px-3 py-2 text-right text-xs tabular-nums text-muted-foreground">{p.ready}</td>
                <td className={cn("px-3 py-2 text-right text-xs tabular-nums", p.restarts > 0 ? "text-warning-fg" : "text-muted-foreground")}>
                  {p.restarts}
                </td>
                <td className="max-w-48 truncate px-3 py-2 font-mono text-2xs text-muted-foreground max-lg:hidden">{p.node || "-"}</td>
                <td className="px-3 py-2 text-right text-xs tabular-nums text-muted-foreground" title={absoluteTime(p.created_at)}>
                  {age(p.created_at, now)}
                </td>
                <td className="px-3 py-2">{items.length > 0 && <Menu label={`Actions for ${p.name}`} trigger={<Ellipsis />} items={items} />}</td>
              </tr>
            )
          })}
        </tbody>
      </table>
    </div>
  )
}

function JobsTable({ jobs, showNs, now, onDelete }: { jobs: K8sJob[]; showNs: boolean; now: number; onDelete: (j: K8sJob) => void }) {
  if (jobs.length === 0)
    return <EmptyState icon={<ListChecks />} title="No jobs" description="Run the hello job to check the cluster end to end." />
  return (
    <div className="overflow-x-auto">
      <table className="w-full text-sm">
        <thead>
          <tr className="border-b bg-muted/40 text-left text-2xs font-medium uppercase tracking-[0.06em] text-subtle-foreground">
            <th className={cn(TH, "px-5")}>Job</th>
            {showNs && <th className={cn(TH, "px-3")}>Namespace</th>}
            <th className={cn(TH, "px-3")}>Status</th>
            <th className={cn(TH, "px-3 max-md:hidden")}>Image</th>
            <th className={cn(TH, "px-3 text-right")}>Created</th>
            <th className="w-10 px-3 py-2" />
          </tr>
        </thead>
        <tbody>
          {jobs.map((j) => (
            <tr key={`${j.namespace}/${j.name}`} className="border-b transition-colors last:border-b-0 hover:bg-accent/40">
              <td className="px-5 py-2 font-mono text-xs">{j.name}</td>
              {showNs && <td className="px-3 py-2 text-xs text-muted-foreground">{j.namespace}</td>}
              <td className="px-3 py-2">
                <StatusPill tone={JOB_TONE[j.status] ?? "neutral"} pulse={j.status === "Running"}>
                  {j.status}
                </StatusPill>
              </td>
              <td className="px-3 py-2 max-md:hidden">
                <Badge className="font-mono">{j.image}</Badge>
              </td>
              <td className="whitespace-nowrap px-3 py-2 text-right text-xs tabular-nums text-muted-foreground">
                <time dateTime={j.created_at} title={absoluteTime(j.created_at)}>
                  {relativeTime(j.created_at, now)}
                </time>
              </td>
              <td className="px-3 py-2">
                <Menu
                  label={`Actions for ${j.name}`}
                  trigger={<Ellipsis />}
                  items={[
                    j.finished
                      ? { label: "Delete job", icon: Trash2, danger: true, onSelect: () => onDelete(j) }
                      : { label: "Delete when finished", icon: Trash2, disabled: true, onSelect: () => {} },
                  ]}
                />
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

function Events({ events, now }: { events: K8sEvent[]; now: number }) {
  if (events.length === 0) return <EmptyState icon={<ScrollText />} title="No events" description="Kubernetes keeps events for about an hour." />
  return (
    <ol className="relative py-2">
      <span aria-hidden className="absolute bottom-4 left-[29px] top-4 w-px bg-border" />
      {events.map((e, i) => {
        const warn = e.type === "Warning"
        return (
          <li key={`${e.object}-${e.reason}-${e.at}-${i}`} className="relative flex items-start gap-3 px-4 py-2">
            <span
              className={cn(
                "relative z-[1] mt-1 flex size-6 shrink-0 items-center justify-center rounded-full border bg-card",
                warn ? "border-warning/40 text-warning" : "text-subtle-foreground",
              )}
            >
              {warn ? <TriangleAlert className="size-3" /> : <span className="size-1.5 rounded-full bg-current" />}
            </span>
            <div className="min-w-0 flex-1">
              <div className="flex flex-wrap items-center gap-x-2 text-sm">
                <span className={cn("font-medium", warn && "text-warning-fg")}>{e.reason}</span>
                <span className="font-mono text-xs text-muted-foreground">{e.object}</span>
                {e.count > 1 && <span className="text-2xs text-subtle-foreground">×{e.count}</span>}
              </div>
              <p className="mt-0.5 break-words text-xs text-muted-foreground">{e.message}</p>
            </div>
            <time dateTime={e.at} title={absoluteTime(e.at)} className="w-16 shrink-0 pt-0.5 text-right text-xs tabular-nums text-subtle-foreground">
              {relativeTime(e.at, now)}
            </time>
          </li>
        )
      })}
    </ol>
  )
}

export default function Kubernetes({ subpath }: ModulePageProps) {
  const { navigate } = useApp()
  const now = useNow(5000)
  const [ns, setNs] = useState(ALL)
  const q = ns ? `?namespace=${encodeURIComponent(ns)}` : ""
  const namespaces = usePoll<string[]>("/api/k8s/namespaces", 30000)
  const nodes = usePoll<K8sNode[]>("/api/k8s/nodes", K8S_POLL_MS * 2)
  const pods = usePoll<K8sPod[]>(`/api/k8s/pods${q}`, K8S_POLL_MS)
  const jobs = usePoll<K8sJob[]>(`/api/k8s/jobs${q}`, K8S_POLL_MS)
  const events = usePoll<K8sEvent[]>(`/api/k8s/events${q}`, K8S_POLL_MS)
  const [logs, setLogs] = useState<{ pod: K8sPod; container: string } | null>(null)
  const [confirm, setConfirm] = useState<ConfirmRequest | null>(null)
  const [helloBusy, setHelloBusy] = useState(false)
  const tab = subpath[0] === "jobs" || subpath[0] === "events" ? subpath[0] : "pods"

  const all = [namespaces, nodes, pods, jobs, events]
  const updated = all.map((p) => p.updatedAt).filter((t): t is number => t !== null)
  const down = nodes.error && !nodes.data
  const podList = pods.data ?? []
  const troubled = podList.filter(isTroubled).length
  const warnings = (events.data ?? []).filter((e) => e.type === "Warning").length

  const del = (j: K8sJob) =>
    setConfirm({
      title: `Delete job ${j.name}?`,
      description: "The job and its finished pods are removed from the cluster. Their logs go with them.",
      confirmLabel: "Delete job",
      danger: true,
      run: async () => {
        try {
          await sendJSON(`/api/k8s/jobs/${encodeURIComponent(j.namespace)}/${encodeURIComponent(j.name)}`, "DELETE")
          toast.success("Job deleted", { description: j.name })
          refreshAll()
        } catch (e) {
          toast.error("Could not delete the job", { description: errorMessage(e) })
        }
      },
    })

  return (
    <div className="space-y-6">
      <PageHeader
        icon={<Ship />}
        title="Kubernetes"
        description="The local kind cluster: its node, pods, jobs and recent events. Select a pod to read its logs."
        actions={
          <>
            <label className="flex items-center gap-2 text-xs text-muted-foreground">
              <span className="max-[1100px]:sr-only">Namespace</span>
              <select
                value={ns}
                onChange={(e) => setNs(e.target.value)}
                className="h-7 rounded-md border bg-secondary px-2 text-xs text-foreground outline-none focus-visible:ring-2 focus-visible:ring-ring"
              >
                <option value={ALL}>All namespaces</option>
                {(namespaces.data ?? []).map((n) => (
                  <option key={n} value={n}>
                    {n}
                  </option>
                ))}
              </select>
            </label>
            <Button
              variant="secondary"
              size="sm"
              disabled={!!down || helloBusy}
              onClick={async () => {
                setHelloBusy(true)
                await runHelloJob()
                setHelloBusy(false)
              }}
            >
              <Play /> Hello job
            </Button>
            <RefreshButton refreshing={all.some((p) => p.refreshing)} updatedAt={updated.length ? Math.min(...updated) : null} />
          </>
        }
      />

      {nodes.loading && !nodes.data && !nodes.error && (
        <div className="grid gap-3 @3xl:grid-cols-[minmax(0,1.3fr)_minmax(0,1fr)]">
          <Skeleton className="h-36 rounded-xl" />
          <Skeleton className="h-36 rounded-xl" />
        </div>
      )}

      {down && (
        <Card className="border-dashed">
          <EmptyState
            icon={<Ship />}
            title={nodes.error?.status === 503 ? "The local cluster is not running" : "Could not reach the cluster"}
            description="Create it from a terminal. Pods, jobs and events appear here as soon as it is up."
          >
            <CopyCommand command="lucid cluster up" />
          </EmptyState>
          {nodes.error && nodes.error.status !== 503 && (
            <div className="px-6 pb-6">
              <ErrorState title="Error" message={nodes.error.message} onRetry={nodes.refresh} />
            </div>
          )}
        </Card>
      )}

      {nodes.data && (
        <>
          <div className="grid gap-3 @3xl:grid-cols-[minmax(0,1.3fr)_minmax(0,1fr)]">
            <div className="space-y-3">
              {nodes.data.map((n) => (
                <NodeCard key={n.name} n={n} now={now} />
              ))}
            </div>
            <Card className="grid grid-cols-2 divide-x divide-y overflow-hidden">
              <Stat label="Pods running" value={`${podList.filter((p) => p.phase === "Running").length}/${podList.length}`} />
              <Stat label="Unhealthy" value={troubled} tone={troubled ? "text-danger-fg" : undefined} />
              <Stat label="Jobs" value={jobs.data?.length ?? "-"} />
              <Stat label="Warnings" value={warnings} tone={warnings ? "text-warning-fg" : undefined} />
            </Card>
          </div>

          <Card className="overflow-hidden">
            <Tabs
              label="Cluster objects"
              className="px-3"
              value={tab}
              onChange={(t) => navigate(t === "pods" ? "/kubernetes" : `/kubernetes/${t}`)}
              items={[
                { id: "pods", label: "Pods", icon: Boxes, count: pods.data?.length },
                { id: "jobs", label: "Jobs", icon: ListChecks, count: jobs.data?.length },
                { id: "events", label: "Events", icon: ScrollText, count: events.data?.length },
              ]}
            />
            {tab === "pods" &&
              (pods.error && !pods.data ? (
                <div className="p-5">
                  <ErrorState title="Could not list pods" message={pods.error.message} onRetry={pods.refresh} />
                </div>
              ) : pods.data ? (
                <PodsTable pods={pods.data} showNs={!ns} now={now} onLogs={(pod, container) => setLogs({ pod, container })} />
              ) : (
                <Skeleton className="m-5 h-32" />
              ))}
            {tab === "jobs" &&
              (jobs.error && !jobs.data ? (
                <div className="p-5">
                  <ErrorState title="Could not list jobs" message={jobs.error.message} onRetry={jobs.refresh} />
                </div>
              ) : jobs.data ? (
                <JobsTable jobs={jobs.data} showNs={!ns} now={now} onDelete={del} />
              ) : (
                <Skeleton className="m-5 h-32" />
              ))}
            {tab === "events" &&
              (events.error && !events.data ? (
                <div className="p-5">
                  <ErrorState title="Could not list events" message={events.error.message} onRetry={events.refresh} />
                </div>
              ) : events.data ? (
                <Events events={events.data} now={now} />
              ) : (
                <Skeleton className="m-5 h-32" />
              ))}
          </Card>
        </>
      )}

      <LogDrawer
        url={
          logs
            ? `/api/k8s/pods/${encodeURIComponent(logs.pod.namespace)}/${encodeURIComponent(logs.pod.name)}/logs?tail=200&container=${encodeURIComponent(logs.container)}`
            : null
        }
        title={<span className="font-mono">{logs?.pod.name}</span>}
        description={
          logs && (
            <span className="flex flex-wrap items-center gap-2">
              <StatusPill tone={PHASE_TONE[logs.pod.phase] ?? "neutral"}>{logs.pod.phase}</StatusPill>
              <span>
                {logs.pod.namespace} · container <span className="font-mono">{logs.container}</span>
              </span>
            </span>
          )
        }
        onClose={() => setLogs(null)}
      />
      <ConfirmDialog request={confirm} onClose={() => setConfirm(null)} />
    </div>
  )
}
