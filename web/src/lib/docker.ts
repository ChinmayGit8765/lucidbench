import { toast } from "sonner"

import { errorMessage, postAction } from "@/lib/api"

/* Mirrors internal/docker: keep these in step with the Go structs. */

export type DockerAction = "start" | "stop" | "restart"

export interface DockerContainer {
  id: string
  name: string
  image: string
  state: string
  status: string
  ports?: string
  created_at?: string
  started_at?: string
  compose_project?: string
  compose_service?: string
  kind_cluster?: string
  actions: DockerAction[]
}

export interface DockerGroup {
  project: string
  kind: "compose" | "kind" | "standalone"
  containers: DockerContainer[]
}

export interface DockerContainers {
  groups: DockerGroup[]
  total: number
  running: number
}

export interface DockerStat {
  id: string
  name: string
  cpu: string
  mem_usage: string
  mem: string
  net_io: string
  block_io: string
  pids: string
}

export interface DockerImage {
  id: string
  repository: string
  tag: string
  size: string
  created_at: string
  created_since: string
}

export interface DockerVolume {
  name: string
  driver: string
  scope: string
  compose_project?: string
  anonymous: boolean
}

export const DOCKER_POLL_MS = 10000

/** "63.06%" as 63.06; NaN-safe. */
export const percent = (s: string | undefined) => {
  const n = Number.parseFloat(s ?? "")
  return Number.isFinite(n) ? n : 0
}

/** The stat for a container, matched by name (stats carry short ids). */
export const statFor = (stats: DockerStat[] | null, c: DockerContainer) =>
  stats?.find((s) => s.name === c.name || c.id.startsWith(s.id))

/** "Up 2 hours" or "Exited (0) 3 hours ago" without what the state pill already says. */
export const shortStatus = (s: string) =>
  s.replace(/^Up /, "").replace(/^Exited \(\d+\) /, "").replace(/\s*\(healthy\)/, "")

const VERB: Record<DockerAction, [string, string]> = {
  start: ["Starting", "started"],
  stop: ["Stopping", "stopped"],
  restart: ["Restarting", "restarted"],
}

/** Starts or stops every container of a compose project, with toasts. */
export async function projectAction(project: string, action: "start" | "stop"): Promise<boolean> {
  const [verb, done] = VERB[action]
  const id = toast.loading(`${verb} ${project}`)
  try {
    const res = await postAction<{ containers: string[] }>(`/api/docker/projects/${encodeURIComponent(project)}/${action}`)
    const n = res.containers.length
    toast.success(n ? `${n} container${n === 1 ? "" : "s"} ${done}` : `Nothing to ${action}`, { id, description: project })
    return true
  } catch (e) {
    toast.error(`Could not ${action} ${project}`, { id, description: errorMessage(e) })
    return false
  }
}

/** Starts, stops or restarts a container, with toasts. */
export async function dockerAction(name: string, action: DockerAction): Promise<boolean> {
  const [verb, done] = VERB[action]
  const id = toast.loading(`${verb} ${name}`)
  try {
    await postAction(`/api/docker/containers/${encodeURIComponent(name)}/${action}`)
    toast.success(`Container ${done}`, { id, description: name })
    return true
  } catch (e) {
    toast.error(`Could not ${action} the container`, { id, description: errorMessage(e) })
    return false
  }
}
