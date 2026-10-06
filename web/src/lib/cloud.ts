import { Boxes, Cloud, Globe, Server, Triangle, type LucideIcon } from "lucide-react"

import type { Tone } from "@/components/ui/badge"
import { request, usePoll, type Polled } from "@/lib/api"
import { usePrefs } from "@/lib/prefs"

/* Mirrors internal/cloud: keep these in step with the Go structs. */

export type CloudState = "connected" | "not_installed" | "not_signed_in" | "error"
export type CloudStatus = "ready" | "failed" | "building" | "unknown" | ""

export interface CloudResource {
  kind: "service" | "project" | "pages" | "worker" | "deployment" | "gcp-project"
  name: string
  region?: string
  project?: string
  status?: CloudStatus
  detail?: string
  url?: string
  console?: string
  /** RFC 3339. */
  updated?: string
  /** The CLI's own wording when it gives only "6 days ago". */
  updated_text?: string
}

export interface CloudSection {
  id: string
  title: string
  resources: CloudResource[]
  note?: string
  error?: string
}

export interface CloudSummary {
  provider: string
  label: string
  cli: string
  state: CloudState
  message?: string
  account?: string
  scope?: string
  login: string
  sections: CloudSection[]
  counts: { ready: number; failed: number; building: number }
  fetched: string
}

export interface CloudDeploy {
  provider: string
  service: string
  region?: string
  project?: string
  matched: boolean
  status?: CloudStatus
  detail?: string
  url?: string
  console?: string
  updated?: string
  updated_text?: string
  note?: string
}

/** The five providers in display order, with a generic icon (no vendor art). */
export const CLOUD_PROVIDERS: { id: string; label: string; icon: LucideIcon; install: string }[] = [
  { id: "gcloud", label: "Google Cloud", icon: Cloud, install: "https://cloud.google.com/sdk/docs/install" },
  { id: "wrangler", label: "Cloudflare", icon: Globe, install: "npm install -g wrangler" },
  { id: "vercel", label: "Vercel", icon: Triangle, install: "npm install -g vercel" },
  { id: "az", label: "Azure", icon: Boxes, install: "https://learn.microsoft.com/cli/azure/install-azure-cli" },
  { id: "aws", label: "AWS", icon: Server, install: "https://docs.aws.amazon.com/cli/latest/userguide/getting-started-install.html" },
]

export const providerLabel = (id: string) => CLOUD_PROVIDERS.find((p) => p.id === id)?.label ?? id

/** The server caches for five minutes, so polling faster only reads the cache. */
export const CLOUD_POLL_MS = 60000

export const STATUS_PILL: Record<string, { tone: Tone; label: string; pulse?: boolean }> = {
  ready: { tone: "success", label: "Ready" },
  failed: { tone: "danger", label: "Failed" },
  building: { tone: "info", label: "Deploying", pulse: true },
  unknown: { tone: "neutral", label: "Unknown" },
}

/** Whether the Cloud extension is in the sidebar; its hooks stay quiet until it is. */
export function useCloudAdded(): boolean {
  const { prefs } = usePrefs()
  return prefs.extensions["cloud"]?.added ?? false
}

/** Every provider's summary, for the Overview tile and attention items. */
export function useCloudAll(enabled: boolean): Polled<{ providers: CloudSummary[] }> {
  return usePoll<{ providers: CloudSummary[] }>(enabled ? "/api/cloud" : null, CLOUD_POLL_MS)
}

/** projects.yaml deploy entries matched to the inventory, by project id. */
export function useCloudDeploys(enabled: boolean): Polled<{ projects: Record<string, CloudDeploy[]> }> {
  return usePoll<{ projects: Record<string, CloudDeploy[]> }>(enabled ? "/api/cloud/deploys" : null, CLOUD_POLL_MS)
}

/** Runs every provider's CLI again, then asks the polls to reload from the fresh cache. */
export async function refreshCloud(): Promise<void> {
  await Promise.allSettled(CLOUD_PROVIDERS.map((p) => request(`/api/cloud/${p.id}?refresh=1`)))
}

/** Resources that failed to deploy, across providers; deployment history rows are not health. */
export function failedResources(providers: CloudSummary[]): { provider: CloudSummary; resource: CloudResource }[] {
  return providers.flatMap((provider) =>
    provider.sections.flatMap((s) =>
      s.resources.filter((r) => r.kind !== "deployment" && r.status === "failed").map((resource) => ({ provider, resource })),
    ),
  )
}

export function totals(providers: CloudSummary[]) {
  const sum = { ready: 0, failed: 0, building: 0, connected: 0 }
  for (const p of providers) {
    sum.ready += p.counts.ready
    sum.failed += p.counts.failed
    sum.building += p.counts.building
    if (p.state === "connected") sum.connected++
  }
  return sum
}
