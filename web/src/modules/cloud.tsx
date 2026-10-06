import { lazy } from "react"
import { ArrowRight, Cloud, CloudOff, ExternalLink } from "lucide-react"

import { StatTile } from "@/components/StatTile"
import { StatusPill } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { useApp } from "@/lib/app"
import { failedResources, providerLabel, totals, useCloudAdded, useCloudAll } from "@/lib/cloud"
import { relativeTime, useNow } from "@/lib/time"
import type { AttentionItem, ModuleDef } from "@/modules/types"

function CloudTile() {
  const { open } = useApp()
  const poll = useCloudAll(true)
  const providers = poll.data?.providers
  const t = providers ? totals(providers) : null
  const total = t ? t.ready + t.failed + t.building : 0
  const failing = providers ? failedResources(providers) : []
  return (
    <StatTile
      icon={Cloud}
      label="Cloud"
      onOpen={() => open("cloud")}
      loading={poll.loading && !providers && !poll.error}
      value={
        t && t.connected > 0 ? (
          <span>
            {t.ready}
            <span className="text-base font-normal text-subtle-foreground">/{total}</span>
          </span>
        ) : (
          <span className="text-base font-medium text-muted-foreground">{t ? "Not signed in" : "Unavailable"}</span>
        )
      }
      aside={
        t && t.connected > 0 ? (
          t.failed > 0 ? (
            <StatusPill tone="danger">{t.failed} failing</StatusPill>
          ) : (
            <StatusPill tone="success">all healthy</StatusPill>
          )
        ) : undefined
      }
      sub={
        t
          ? t.connected > 0
            ? `healthy deploys · ${t.connected} ${t.connected === 1 ? "provider" : "providers"} signed in`
            : "sign in to gcloud, wrangler or vercel"
          : poll.error?.message
      }
      footer={
        failing.length > 0 ? (
          <div className="space-y-1">
            {failing.slice(0, 3).map(({ provider, resource }) => (
              <div key={`${provider.provider}-${resource.name}-${resource.region ?? ""}`} className="flex items-center gap-2 text-xs text-muted-foreground">
                <span className="size-1.5 shrink-0 rounded-full bg-danger" />
                <span className="truncate font-mono">{resource.name}</span>
                <span className="ml-auto shrink-0 text-2xs text-subtle-foreground">{provider.label}</span>
              </div>
            ))}
          </div>
        ) : undefined
      }
    />
  )
}

/** Needs attention: a service or project whose latest deploy failed. */
function useCloudAttention(): AttentionItem[] | null {
  const { open } = useApp()
  const now = useNow(30000)
  const added = useCloudAdded()
  const poll = useCloudAll(added)
  if (!added) return []
  if (!poll.data && !poll.error) return null
  return failedResources(poll.data?.providers ?? []).map(({ provider, resource }) => ({
    key: `cloud-${provider.provider}-${resource.kind}-${resource.name}-${resource.region ?? ""}-${resource.project ?? ""}`,
    severity: "danger" as const,
    icon: <CloudOff className="size-4" />,
    title: (
      <>
        <span className="font-mono text-[0.92em]">{resource.name}</span> failed to deploy
      </>
    ),
    meta: (
      <>
        {providerLabel(provider.provider)}
        {resource.region ? ` · ${resource.region}` : ""}
        {resource.updated ? ` · ${relativeTime(resource.updated, now)}` : ""}
        {resource.detail ? ` · ${resource.detail}` : ""}
      </>
    ),
    action: (
      <>
        {resource.console && (
          <Button variant="ghost" size="sm" asChild>
            <a href={resource.console} target="_blank" rel="noreferrer">
              Console <ExternalLink />
            </a>
          </Button>
        )}
        <Button variant="ghost" size="sm" onClick={() => open("cloud")}>
          View <ArrowRight />
        </Button>
      </>
    ),
  }))
}

export const cloud: ModuleDef = {
  id: "cloud",
  title: "Cloud",
  icon: Cloud,
  route: "/cloud",
  section: "infrastructure",
  kind: "extension",
  order: 10,
  defaultEnabled: false,
  category: "cloud",
  description: "Google Cloud, Cloudflare and Vercel deploys, and Azure and AWS sign-ins, read through your own CLIs.",
  requires: { clis: ["gcloud|wrangler|vercel|az|aws"] },
  keywords: "gcp gcloud cloud run cloudflare wrangler workers pages vercel aws azure deploy",
  component: lazy(() => import("@/pages/Cloud")),
  overviewTile: CloudTile,
  useAttention: useCloudAttention,
}
