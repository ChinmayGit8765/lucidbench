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

/** Submits the hello job with toasts. Resolves the job name, or null on failure. */
export async function runHelloJob(): Promise<string | null> {
  const id = toast.loading("Submitting hello job")
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
