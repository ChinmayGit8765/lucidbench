import { toast } from "sonner"

import { errorMessage, refreshAll, request } from "@/lib/api"

export interface ClusterInfo {
  name: string
  running: boolean
  kubeconfig_present: boolean
}

export interface Job {
  name: string
  image: string
  status: string
  createdAt: string
}

/**
 * Submits the hello job with toasts. Resolves the job name, or null on
 * failure. waking says the cluster sleeps, so the daemon starts it first.
 */
export async function runHelloJob(waking = false): Promise<string | null> {
  const id = toast.loading(waking ? "Waking the cluster for the hello job" : "Submitting hello job", {
    description: waking ? "The node starts first; that usually takes under a minute." : undefined,
  })
  try {
    const res = await request("/api/jobs/hello", { method: "POST" })
    const body = (await res.json()) as { name: string }
    toast.success("Hello job submitted", { id, description: body.name })
    refreshAll()
    return body.name
  } catch (e) {
    toast.error("Could not submit the job", { id, description: errorMessage(e) })
    return null
  }
}
