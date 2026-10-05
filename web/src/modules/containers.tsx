import { lazy } from "react"
import { Container, HardDrive, Layers } from "lucide-react"

import type { Command } from "@/components/CommandPalette"
import { StatTile } from "@/components/StatTile"
import { StatusPill } from "@/components/ui/badge"
import { usePoll } from "@/lib/api"
import { useApp } from "@/lib/app"
import { DOCKER_POLL_MS, percent, type DockerContainers, type DockerStat } from "@/lib/docker"
import { usePrefs } from "@/lib/prefs"
import type { ModuleDef } from "@/modules/types"

function ContainersTile() {
  const { open } = useApp()
  const { label } = usePrefs()
  const poll = usePoll<DockerContainers>("/api/docker/containers", DOCKER_POLL_MS)
  const stats = usePoll<DockerStat[]>("/api/docker/stats", DOCKER_POLL_MS * 2)
  const d = poll.data
  const stopped = d ? d.total - d.running : 0
  const top = [...(stats.data ?? [])].sort((a, b) => percent(b.cpu) - percent(a.cpu)).slice(0, 4)
  const max = Math.max(1, ...top.map((s) => percent(s.cpu)))
  return (
    <StatTile
      icon={Container}
      label="Containers"
      onOpen={() => open("containers")}
      loading={poll.loading && !d && !poll.error}
      value={
        d ? (
          <span>
            {d.running}
            <span className="text-base font-normal text-subtle-foreground">/{d.total}</span>
          </span>
        ) : (
          <span className="text-base font-medium text-muted-foreground">Docker offline</span>
        )
      }
      aside={stopped > 0 ? <StatusPill tone="neutral">{stopped} stopped</StatusPill> : d ? <StatusPill tone="success">all up</StatusPill> : undefined}
      sub={d ? `running · ${d.groups.filter((g) => g.kind === "compose").length} compose projects` : poll.error?.message}
      footer={
        top.length > 0 ? (
          <div className="space-y-1">
            <div className="text-2xs font-medium uppercase tracking-[0.08em] text-subtle-foreground">{label("usage_meter", "Usage")} · CPU</div>
            {top.map((s) => (
              <div key={s.id} className="flex items-center gap-2 text-2xs text-muted-foreground" title={`${s.name}: ${s.cpu} CPU, ${s.mem} memory`}>
                <span className="w-24 truncate font-mono">{s.name}</span>
                <span className="h-1 flex-1 overflow-hidden rounded-full bg-muted">
                  <span className="block h-full rounded-full bg-brand" style={{ width: `${Math.max(3, (percent(s.cpu) / max) * 100)}%` }} />
                </span>
                <span className="w-10 text-right tabular-nums">{s.cpu}</span>
              </div>
            ))}
          </div>
        ) : undefined
      }
    />
  )
}

function useContainerCommands(): Command[] {
  const { open } = useApp()
  return [
    { id: "docker-images", label: "Show Docker images", group: "Actions", icon: Layers, keywords: "docker images", run: () => open("containers", ["images"]) },
    { id: "docker-volumes", label: "Show Docker volumes", group: "Actions", icon: HardDrive, keywords: "docker volumes disks", run: () => open("containers", ["volumes"]) },
  ]
}

export const containers: ModuleDef = {
  id: "containers",
  title: "Containers",
  icon: Container,
  route: "/containers",
  section: "infrastructure",
  kind: "extension",
  category: "devops",
  order: 1,
  defaultEnabled: true,
  description: "Every Docker container by compose project, with live usage, logs, images and volumes.",
  requires: { docker: true },
  keywords: "docker compose containers logs images volumes stats",
  component: lazy(() => import("@/pages/Containers")),
  useCommands: useContainerCommands,
  overviewTile: ContainersTile,
}
