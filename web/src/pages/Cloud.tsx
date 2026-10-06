import { useState, type ReactNode } from "react"
import { Cloud as CloudIcon, ExternalLink, Link2, LogIn, RefreshCw, ShieldCheck } from "lucide-react"

import { CopyCommand } from "@/components/CopyCommand"
import { PageHeader } from "@/components/Shell"
import { Badge, StatusPill } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card } from "@/components/ui/card"
import { EmptyState, ErrorState, Skeleton } from "@/components/ui/states"
import { refreshAll, usePoll } from "@/lib/api"
import {
  CLOUD_POLL_MS,
  CLOUD_PROVIDERS,
  refreshCloud,
  STATUS_PILL,
  totals,
  type CloudResource,
  type CloudSection,
  type CloudSummary,
} from "@/lib/cloud"
import { absoluteTime, relativeTime, useNow } from "@/lib/time"
import { cn } from "@/lib/utils"

function Cell({ label, value, sub }: { label: string; value: ReactNode; sub?: ReactNode }) {
  return (
    <div className="min-w-0 px-4 py-3.5">
      <div className="text-2xs font-medium uppercase tracking-[0.08em] text-subtle-foreground">{label}</div>
      <div className="mt-1 truncate text-xl font-semibold tabular-nums tracking-tight">{value}</div>
      {sub && <div className="mt-0.5 truncate text-xs text-muted-foreground">{sub}</div>}
    </div>
  )
}

function Summary({ providers }: { providers: CloudSummary[] }) {
  const t = totals(providers)
  const total = t.ready + t.failed + t.building
  return (
    <Card className="grid grid-cols-2 divide-x divide-y overflow-hidden @3xl:grid-cols-4 @3xl:divide-y-0">
      <Cell label="Signed in" value={`${t.connected}/${providers.length}`} sub="CLIs on this machine" />
      <Cell
        label="Healthy"
        value={
          <span>
            {t.ready}
            <span className="text-base font-normal text-subtle-foreground">/{total}</span>
          </span>
        }
        sub="services and projects"
      />
      <Cell label="Failing" value={<span className={cn(t.failed > 0 && "text-danger-fg")}>{t.failed}</span>} sub={t.failed > 0 ? "last deploy failed" : "none"} />
      <Cell label="Deploying" value={t.building} sub="rolling out now" />
    </Card>
  )
}

function Updated({ r, now }: { r: CloudResource; now: number }) {
  if (r.updated) return <span title={absoluteTime(r.updated)}>{relativeTime(r.updated, now)}</span>
  if (r.updated_text) return <span>{r.updated_text}</span>
  return <span className="text-subtle-foreground">-</span>
}

function Links({ r }: { r: CloudResource }) {
  const cls = "inline-flex size-6 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-accent hover:text-foreground [&_svg]:size-3.5"
  return (
    <span className="inline-flex items-center gap-0.5">
      {r.url && (
        <a href={r.url} target="_blank" rel="noreferrer" className={cls} title={`Open ${r.url}`} aria-label={`Open ${r.name}`}>
          <Link2 />
        </a>
      )}
      {r.console && (
        <a href={r.console} target="_blank" rel="noreferrer" className={cls} title="Open in the provider's console" aria-label={`Open ${r.name} in the console`}>
          <ExternalLink />
        </a>
      )}
    </span>
  )
}

function ResourceTable({ s, now }: { s: CloudSection; now: number }) {
  const place = (r: CloudResource) => [r.region, r.kind === "gcp-project" ? r.project : ""].filter(Boolean).join(" · ")
  const hasHealth = s.resources.some((r) => r.status)
  return (
    <div>
      <div className="flex items-center gap-2 border-y bg-muted/40 px-5 py-2">
        <h3 className="text-2xs font-medium uppercase tracking-[0.06em] text-subtle-foreground">{s.title}</h3>
        <span className="text-2xs tabular-nums text-subtle-foreground">{s.resources.length}</span>
      </div>
      {s.error && <div className="px-5 py-3 text-sm text-danger-fg">Could not load: {s.error}</div>}
      {s.note && <div className="px-5 py-2.5 text-xs text-muted-foreground">{s.note}</div>}
      {s.resources.length > 0 && (
        <div className="overflow-x-auto">
          <table className="w-full table-fixed text-sm">
            <colgroup>
              <col className="w-[26%]" />
              {hasHealth && <col className="w-28" />}
              <col />
              <col className="w-28" />
              <col className="w-20" />
            </colgroup>
            <tbody>
              {s.resources.map((r, i) => (
                <tr key={`${r.kind}-${r.name}-${r.region ?? ""}-${r.project ?? ""}-${i}`} className="border-b last:border-b-0 hover:bg-accent/40">
                  <td className="truncate px-5 py-2 font-medium" title={r.name}>
                    {r.name}
                  </td>
                  {hasHealth && (
                    <td className="px-3 py-2">
                      {r.status && STATUS_PILL[r.status] ? (
                        <StatusPill tone={STATUS_PILL[r.status].tone} pulse={STATUS_PILL[r.status].pulse}>
                          {STATUS_PILL[r.status].label}
                        </StatusPill>
                      ) : null}
                    </td>
                  )}
                  <td className="truncate px-3 py-2 text-xs text-muted-foreground" title={[place(r), r.detail].filter(Boolean).join(" · ")}>
                    {place(r) && <Badge className="mr-1.5 font-mono">{place(r)}</Badge>}
                    {r.detail}
                  </td>
                  <td className="px-3 py-2 text-right text-xs tabular-nums text-muted-foreground">
                    <Updated r={r} now={now} />
                  </td>
                  <td className="px-3 py-2 text-right">
                    <Links r={r} />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}

const STATE_PILL = {
  connected: { tone: "success", label: "Connected" },
  not_signed_in: { tone: "warning", label: "Not signed in" },
  not_installed: { tone: "neutral", label: "Not installed" },
  error: { tone: "danger", label: "Error" },
} as const

function ProviderCard({ id, poll, now }: { id: string; poll: ReturnType<typeof usePoll<CloudSummary>>; now: number }) {
  const meta = CLOUD_PROVIDERS.find((p) => p.id === id)!
  const Icon = meta.icon
  const s = poll.data
  const pill = s ? STATE_PILL[s.state] : undefined
  return (
    <Card className="overflow-hidden">
      <div className="flex items-center gap-3 px-5 py-3.5">
        <span className="flex size-8 shrink-0 items-center justify-center rounded-lg border border-border-strong bg-elevated text-brand [&_svg]:size-4">
          <Icon />
        </span>
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2">
            <h2 className="text-sm font-semibold">{meta.label}</h2>
            <span className="font-mono text-2xs text-subtle-foreground">{id}</span>
          </div>
          <div className="truncate text-xs text-muted-foreground">
            {s?.state === "connected" ? (
              <>
                Connected: <span className="font-mono">{s.account}</span>
                {s.scope && <> · {s.scope}</>}
              </>
            ) : (
              (s?.message ?? "Checking…")
            )}
          </div>
        </div>
        {pill && <StatusPill tone={pill.tone}>{pill.label}</StatusPill>}
      </div>

      {!s && poll.loading && !poll.error && (
        <div className="space-y-2 border-t p-5">
          <Skeleton className="h-4 w-1/3" />
          <Skeleton className="h-4 w-2/3" />
        </div>
      )}
      {!s && poll.error && <div className="border-t px-5 py-3 text-sm text-danger-fg">{poll.error.message}</div>}

      {s?.state === "not_signed_in" && (
        <div className="space-y-2 border-t px-5 py-4">
          <p className="flex items-center gap-2 text-sm text-muted-foreground">
            <LogIn className="size-4 text-subtle-foreground" /> Sign in with your own account; Lucidbench never sees the credentials.
          </p>
          <CopyCommand command={s.login} />
        </div>
      )}
      {s?.state === "not_installed" && (
        <div className="space-y-2 border-t px-5 py-4 text-sm text-muted-foreground">
          <p>
            The <span className="font-mono">{s.cli}</span> command was not found on PATH.
          </p>
          {meta.install.startsWith("http") ? (
            <a href={meta.install} target="_blank" rel="noreferrer" className="inline-flex items-center gap-1.5 text-brand hover:underline">
              Install guide <ExternalLink className="size-3.5" />
            </a>
          ) : (
            <CopyCommand command={meta.install} />
          )}
        </div>
      )}
      {s?.state === "error" && <div className="border-t px-5 py-3 text-sm text-danger-fg">{s.message}</div>}

      {s?.state === "connected" && s.sections.length === 0 && (
        <div className="border-t px-5 py-3 text-xs text-muted-foreground">Identity only: this CLI lists no resources here.</div>
      )}
      {s?.state === "connected" && s.sections.map((sec) => <ResourceTable key={sec.id} s={sec} now={now} />)}
    </Card>
  )
}

export default function Cloud() {
  const now = useNow(15000)
  const [forcing, setForcing] = useState(false)
  // One poll per provider, so each card fills in as its CLI answers.
  const polls = [
    usePoll<CloudSummary>("/api/cloud/gcloud", CLOUD_POLL_MS),
    usePoll<CloudSummary>("/api/cloud/wrangler", CLOUD_POLL_MS),
    usePoll<CloudSummary>("/api/cloud/vercel", CLOUD_POLL_MS),
    usePoll<CloudSummary>("/api/cloud/az", CLOUD_POLL_MS),
    usePoll<CloudSummary>("/api/cloud/aws", CLOUD_POLL_MS),
  ]
  const loaded = polls.flatMap((p) => (p.data ? [p.data] : []))
  const updated = polls.map((p) => p.updatedAt).filter((t): t is number => t !== null)
  const allDown = polls.every((p) => p.error && !p.data)
  const refresh = async () => {
    setForcing(true)
    await refreshCloud()
    refreshAll()
    setForcing(false)
  }
  const none = loaded.length === polls.length && loaded.every((s) => s.state === "not_installed")

  return (
    <div className="space-y-6">
      <PageHeader
        icon={<CloudIcon />}
        title="Cloud"
        description="What you have deployed on Google Cloud, Cloudflare and Vercel, read through the CLIs you are already signed in to."
        actions={
          <div className="flex items-center gap-3">
            <span className="flex items-center gap-1.5 text-xs text-subtle-foreground max-[1100px]:hidden">
              <ShieldCheck className="size-3.5" /> Read-only · no credentials stored
            </span>
            {updated.length > 0 && (
              <span className="text-xs tabular-nums text-subtle-foreground max-[1100px]:hidden">Checked {relativeTime(new Date(Math.min(...updated)).toISOString(), now)}</span>
            )}
            <Button variant="secondary" size="sm" onClick={refresh} disabled={forcing} title="Run the CLIs again (results are otherwise cached for five minutes)">
              <RefreshCw className={cn(forcing && "animate-spin")} />
              Refresh
            </Button>
          </div>
        }
      />

      {allDown && <ErrorState title="Cannot read the Cloud inventory" message={polls[0].error?.message} onRetry={refreshAll} />}
      {none && (
        <Card className="border-dashed">
          <EmptyState
            icon={<CloudIcon />}
            title="No cloud CLI found"
            description="Install gcloud, wrangler or vercel and sign in; this page lists what you have deployed. Lucidbench runs the CLI as you and keeps nothing."
          />
        </Card>
      )}
      {!allDown && !none && (
        <>
          {loaded.length === polls.length ? <Summary providers={loaded} /> : <Skeleton className="h-[88px] rounded-xl" />}
          <div className="space-y-4">
            {CLOUD_PROVIDERS.map((p, i) => (
              <ProviderCard key={p.id} id={p.id} poll={polls[i]} now={now} />
            ))}
          </div>
        </>
      )}
    </div>
  )
}
