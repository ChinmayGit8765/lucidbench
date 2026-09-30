import { useCallback, useEffect, useState } from "react"
import { Play, RefreshCw } from "lucide-react"

import { CopyCommand } from "@/components/CopyCommand"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { ErrorState, LoadingState } from "@/components/ui/states"
import { ApiError, request, usePoll } from "@/lib/api"
import type { Health } from "@/lib/health"
import { cn } from "@/lib/utils"

interface ClusterInfo {
  name: string
  running: boolean
  kubeconfig_present: boolean
}

interface Job {
  name: string
  image: string
  status: string
  createdAt: string
}

const JOB_STATUS: Record<string, string> = {
  Completed: "bg-emerald-500",
  Running: "bg-sky-500",
  Failed: "bg-destructive",
  Pending: "bg-amber-500",
}

const CLUSTER_UP = "lucid cluster up"

function Row({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <>
      <span className="text-muted-foreground">{label}</span>
      <span className="min-w-0 break-words">{children}</span>
    </>
  )
}

function Dot({ className }: { className: string }) {
  return <span className={cn("mr-2 inline-block size-2 rounded-full align-middle", className)} />
}

function NoCluster() {
  return (
    <div className="space-y-2">
      <p className="text-sm text-muted-foreground">
        The local cluster is not running. Create it from a terminal:
      </p>
      <CopyCommand command={CLUSTER_UP} />
    </div>
  )
}

function DaemonCard({ health }: { health: Health | null }) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>Daemon</CardTitle>
        <CardDescription>lucidd, the Lucidbench engine.</CardDescription>
      </CardHeader>
      <CardContent className="grid grid-cols-[8rem_1fr] gap-y-2 text-sm">
        <Row label="Status">
          <Dot className={health ? "bg-emerald-500" : "bg-destructive"} />
          {health ? "Running" : "Unreachable"}
        </Row>
        <Row label="Version">{health?.version ?? "-"}</Row>
      </CardContent>
    </Card>
  )
}

function ClusterCard() {
  const { data, error, loading } = usePoll<ClusterInfo>("/api/cluster")
  return (
    <Card>
      <CardHeader>
        <CardTitle>Cluster</CardTitle>
        <CardDescription>The local kind cluster that runs jobs.</CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        {loading && !data && <LoadingState />}
        {error && !data && (
          <ErrorState
            title={error.status === 503 ? "Docker is not reachable" : "Could not load cluster status"}
            message={error.message}
          />
        )}
        {data && (
          <>
            <div className="grid grid-cols-[8rem_1fr] gap-y-2 text-sm">
              <Row label="Name">{data.name}</Row>
              <Row label="Status">
                <Dot className={data.running ? "bg-emerald-500" : "bg-muted-foreground/50"} />
                {data.running ? "Running" : "Not running"}
              </Row>
              <Row label="Kubeconfig">{data.kubeconfig_present ? "Present" : "Missing"}</Row>
            </div>
            {!data.running && <NoCluster />}
          </>
        )}
      </CardContent>
    </Card>
  )
}

function formatTime(iso: string): string {
  const d = new Date(iso)
  return Number.isNaN(d.getTime()) ? "-" : d.toLocaleString()
}

function Logs({ name }: { name: string }) {
  const [text, setText] = useState<string | null>(null)
  const [err, setErr] = useState<string | null>(null)
  const load = useCallback(async () => {
    setErr(null)
    try {
      setText(await (await request(`/api/jobs/${encodeURIComponent(name)}/logs`)).text())
    } catch (e) {
      setText(null)
      setErr(e instanceof ApiError ? e.message : String(e))
    }
  }, [name])
  useEffect(() => {
    setText(null)
    void load()
  }, [load])

  return (
    <div className="space-y-2">
      <div className="flex items-center justify-between">
        <span className="text-sm font-medium">
          Logs <span className="font-mono text-muted-foreground">{name}</span>
        </span>
        <Button variant="ghost" size="sm" onClick={() => void load()}>
          <RefreshCw /> Reload
        </Button>
      </div>
      <pre className="max-h-64 overflow-auto rounded-md border bg-background p-3 font-mono text-xs leading-relaxed whitespace-pre-wrap">
        {err ? err : text === null ? "Loading..." : text.trim() === "" ? "(no output)" : text}
      </pre>
    </div>
  )
}

function JobsCard() {
  const { data, error, loading, refresh } = usePoll<Job[]>("/api/jobs")
  const [selected, setSelected] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [runError, setRunError] = useState<ApiError | null>(null)

  const runHello = async () => {
    setBusy(true)
    setRunError(null)
    try {
      const res = await request("/api/jobs/hello", { method: "POST" })
      const body = (await res.json()) as { name: string }
      setSelected(body.name)
      refresh()
    } catch (e) {
      setRunError(e instanceof ApiError ? e : new ApiError(0, String(e)))
    } finally {
      setBusy(false)
    }
  }

  const unavailable = error?.status === 503 || runError?.status === 503

  return (
    <Card>
      <CardHeader className="flex-row items-start justify-between gap-4 space-y-0">
        <div className="flex flex-col gap-1.5">
          <CardTitle>Jobs</CardTitle>
          <CardDescription>Batch jobs on the local cluster.</CardDescription>
        </div>
        <Button size="sm" onClick={runHello} disabled={busy}>
          <Play /> {busy ? "Submitting..." : "Run hello job"}
        </Button>
      </CardHeader>
      <CardContent className="space-y-4">
        {loading && !data && <LoadingState />}
        {unavailable && <NoCluster />}
        {!unavailable && error && !data && (
          <ErrorState title="Could not load jobs" message={error.message} />
        )}
        {!unavailable && runError && (
          <ErrorState title="Could not submit the job" message={runError.message} />
        )}
        {!unavailable && data && data.length === 0 && (
          <p className="py-4 text-center text-sm text-muted-foreground">
            No jobs yet. Run the hello job to try the cluster.
          </p>
        )}
        {!unavailable && data && data.length > 0 && (
          <div className="overflow-hidden rounded-md border">
            <table className="w-full text-sm">
              <thead className="bg-muted/50 text-left text-xs text-muted-foreground">
                <tr>
                  <th className="px-3 py-2 font-medium">Name</th>
                  <th className="px-3 py-2 font-medium">Status</th>
                  <th className="px-3 py-2 font-medium">Image</th>
                  <th className="px-3 py-2 font-medium">Created</th>
                </tr>
              </thead>
              <tbody>
                {data.map((j) => (
                  <tr
                    key={j.name}
                    onClick={() => setSelected(j.name)}
                    className={cn(
                      "cursor-pointer border-t transition-colors hover:bg-accent/50",
                      selected === j.name && "bg-accent",
                    )}
                  >
                    <td className="px-3 py-2 font-mono text-xs">{j.name}</td>
                    <td className="px-3 py-2">
                      <Dot className={JOB_STATUS[j.status] ?? "bg-muted-foreground/50"} />
                      {j.status}
                    </td>
                    <td className="px-3 py-2">
                      <Badge>{j.image}</Badge>
                    </td>
                    <td className="px-3 py-2 text-muted-foreground">{formatTime(j.createdAt)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
        {!unavailable && selected && <Logs name={selected} />}
      </CardContent>
    </Card>
  )
}

export default function System({ health }: { health: Health | null }) {
  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-semibold tracking-tight">System</h1>
      <div className="grid gap-6 md:grid-cols-2">
        <DaemonCard health={health} />
        <ClusterCard />
      </div>
      <JobsCard />
    </div>
  )
}
