import { lazy } from "react"
import { Boxes, Moon, ScrollText, Ship } from "lucide-react"

import type { Command } from "@/components/CommandPalette"
import { StatTile } from "@/components/StatTile"
import { StatusPill } from "@/components/ui/badge"
import { usePoll } from "@/lib/api"
import { useApp } from "@/lib/app"
import type { ClusterInfo } from "@/lib/jobs"
import { isTroubled, K8S_POLL_MS, type K8sJob, type K8sPod } from "@/lib/k8s"
import { isAsleep, MODE_LABEL, usePower } from "@/lib/power"
import type { ModuleDef } from "@/modules/types"

function KubernetesTile() {
  const { open } = useApp()
  const cluster = usePoll<ClusterInfo>("/api/cluster", 15000)
  const pods = usePoll<K8sPod[]>("/api/k8s/pods", K8S_POLL_MS * 2)
  const jobs = usePoll<K8sJob[]>("/api/k8s/jobs", K8S_POLL_MS * 2)
  // Pods answering means the cluster is up even if the status probe failed.
  const c = cluster.data ?? (pods.data ? { name: (pods.data[0]?.node ?? "").replace(/-control-plane$/, "") || "cluster", running: true, kubeconfig_present: true } : undefined)
  const list = pods.data ?? []
  const running = list.filter((p) => p.phase === "Running").length
  const troubled = list.filter(isTroubled).length
  const jobList = jobs.data ?? []
  // The node can be stopped while the cluster still exists: power knows.
  const power = usePower()
  const pc = power.data?.cluster
  // Until power answers, /api/cluster's "running" only means the cluster exists.
  const powerPending = power.loading && !power.data && !power.error
  if (pc && (isAsleep(pc) || pc.state === "starting")) {
    const starting = pc.state === "starting"
    return (
      <StatTile
        icon={Ship}
        label="Kubernetes"
        onOpen={() => open("kubernetes")}
        value={starting ? "Starting" : pc.state === "sleeping" ? "Sleeping" : "Stopped"}
        aside={
          <StatusPill tone={starting ? "info" : "neutral"} pulse={starting}>
            {starting ? "waking" : MODE_LABEL[pc.mode]}
          </StatusPill>
        }
        sub={<span className="font-mono">kind · {pc.name}</span>}
        footer={
          <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
            <Moon className="size-3.5 text-subtle-foreground" />
            {pc.mode === "off" ? "Start it from the Kubernetes page" : "Wakes when a job is submitted"}
          </div>
        }
      />
    )
  }
  return (
    <StatTile
      icon={Ship}
      label="Kubernetes"
      onOpen={() => open("kubernetes")}
      loading={(cluster.loading && !c && !cluster.error) || powerPending}
      value={c ? (c.running ? "Running" : "Stopped") : "Offline"}
      aside={
        troubled > 0 ? (
          <StatusPill tone="danger">{troubled} unhealthy</StatusPill>
        ) : (
          <StatusPill tone={c?.running ? "success" : c ? "neutral" : "danger"} pulse={!!c?.running}>
            {c?.running ? "up" : c ? "down" : "error"}
          </StatusPill>
        )
      }
      sub={c ? <span className="font-mono">kind · {c.name}</span> : "Docker is not reachable"}
      footer={
        <div className="flex items-center gap-3 text-xs tabular-nums text-muted-foreground">
          <span className="flex items-center gap-1.5">
            <Boxes className="size-3.5 text-subtle-foreground" />
            {pods.data ? `${running}/${list.length} pods running` : "no pods"}
          </span>
          {jobs.data && (
            <span>
              {jobList.length} {jobList.length === 1 ? "job" : "jobs"}
            </span>
          )}
        </div>
      }
    />
  )
}

function useKubernetesCommands(): Command[] {
  const { open } = useApp()
  return [
    { id: "k8s-events", label: "Show cluster events", group: "Actions", icon: ScrollText, keywords: "kubernetes events warnings", run: () => open("kubernetes", ["events"]) },
  ]
}
export const kubernetes: ModuleDef = {
  id: "kubernetes",
  title: "Kubernetes",
  icon: Ship,
  route: "/kubernetes",
  section: "infrastructure",
  kind: "extension",
  category: "devops",
  order: 2,
  defaultEnabled: true,
  description: "The local kind cluster: nodes, pods, jobs, events and pod logs.",
  requires: { docker: true, optional: { clis: ["kubectl"] } },
  keywords: "k8s kind cluster pods jobs events nodes",
  component: lazy(() => import("@/pages/Kubernetes")),
  useCommands: useKubernetesCommands,
  overviewTile: KubernetesTile,
}
