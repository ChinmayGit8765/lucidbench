import { useMemo, useState, type ReactNode } from "react"
import {
  Container as ContainerIcon,
  Ellipsis,
  ExternalLink,
  GitBranch,
  KeyRound,
  Play,
  RotateCw,
  Square,
  Workflow,
} from "lucide-react"

import { CopyCommand, copyText } from "@/components/CopyCommand"
import { RunBars, RunIcon, RunnerDot } from "@/components/ci"
import { PageHeader, RefreshButton } from "@/components/Shell"
import { Badge, StatusPill } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card } from "@/components/ui/card"
import { ConfirmDialog, type ConfirmRequest } from "@/components/ui/confirm"
import { Menu } from "@/components/ui/menu"
import { EmptyState, ErrorState, Skeleton } from "@/components/ui/states"
import { refreshAll, usePoll } from "@/lib/api"
import {
  CI_POLL_MS,
  containerAction,
  failingRuns,
  formatDuration,
  isFailure,
  rerunFailed,
  RUN_STATE,
  runDuration,
  runState,
  shortRepo,
  type CIContainer,
  type CIRun,
  type CIRunner,
  type CIRuns,
  type CIRunners,
  type CISummary,
  type SourceError,
} from "@/lib/ci"
import { absoluteTime, relativeTime, useNow } from "@/lib/time"
import { cn } from "@/lib/utils"

const CONFIG_SNIPPET = `ci:
  github:
    repos: ["you/your-repo"]
    token: "env:GITHUB_TOKEN"   # or sign in with: gh auth login`

/* ---------- summary ---------- */

function Cell({ label, value, sub, tone }: { label: string; value: ReactNode; sub?: ReactNode; tone?: string }) {
  return (
    <div className="min-w-0 px-4 py-3.5">
      <div className="text-2xs font-medium uppercase tracking-[0.08em] text-subtle-foreground">{label}</div>
      <div className={cn("mt-1 truncate text-xl font-semibold tabular-nums tracking-tight", tone)}>{value}</div>
      {sub && <div className="mt-0.5 truncate text-xs text-muted-foreground">{sub}</div>}
    </div>
  )
}

function SummaryStrip({ s, runs, now }: { s: CISummary | null; runs: CIRun[]; now: number }) {
  if (!s) {
    return <Skeleton className="h-[88px] rounded-xl" />
  }
  const rate = s.runs_24h.pass_rate
  // Same measure as the Overview: workflow + branch pairs whose newest run failed.
  const failingList = failingRuns(runs)
  const failingRepoCount = new Set(failingList.map((r) => r.repo)).size
  // With no runs in 24h, show the recent window instead of a bare dash.
  const recent = runs.slice(0, 30)
  const pass = recent.filter((r) => runState(r) === "success").length
  const fail = recent.filter(isFailure).length
  const recentRate = pass + fail > 0 ? pass / (pass + fail) : null
  return (
    <Card className="grid grid-cols-2 divide-x divide-y overflow-hidden @3xl:grid-cols-[repeat(4,minmax(0,1fr))_minmax(0,1.6fr)] @3xl:divide-y-0">
      <Cell
        label="Runners"
        value={
          <span>
            {s.runners.online}
            <span className="text-base font-normal text-subtle-foreground">/{s.runners.total}</span>
          </span>
        }
        sub={
          s.runners.total === 0
            ? "none registered"
            : `${s.runners.busy} busy · ${s.runners.offline} offline`
        }
        tone={s.runners.offline > 0 ? "text-warning-fg" : undefined}
      />
      <Cell
        label="Containers"
        value={
          <span>
            {s.containers.up}
            <span className="text-base font-normal text-subtle-foreground">/{s.containers.total}</span>
          </span>
        }
        sub={s.containers.down > 0 ? `${s.containers.down} stopped` : s.containers.total ? "all running" : "none found"}
      />
      {rate !== null || recentRate === null ? (
        <Cell
          label="Pass · 24h"
          value={rate === null ? "-" : `${Math.round(rate * 100)}%`}
          sub={`${s.runs_24h.total} runs${s.runs_24h.in_progress ? ` · ${s.runs_24h.in_progress} active` : ""}`}
          tone={rate !== null && rate < 0.8 ? "text-danger-fg" : undefined}
        />
      ) : (
        <Cell
          label={`Pass · last ${recent.length}`}
          value={`${Math.round(recentRate * 100)}%`}
          sub="no runs in the last 24h"
          tone={recentRate < 0.8 ? "text-danger-fg" : undefined}
        />
      )}
      <Cell
        label="Failing"
        value={failingList.length}
        sub={
          failingList.length
            ? `${failingRepoCount === 1 ? shortRepo(failingList[0].repo) : `${failingRepoCount} repos`} · re-run below`
            : "all green"
        }
        tone={failingList.length ? "text-danger-fg" : undefined}
      />
      <div className="col-span-2 flex min-w-0 flex-col justify-center px-4 py-3 @3xl:col-span-1">
        <div className="mb-1.5 flex items-center justify-between text-2xs font-medium uppercase tracking-[0.08em] text-subtle-foreground">
          <span>Recent runs</span>
          <span className="normal-case tracking-normal tabular-nums">{Math.min(runs.length, 30)} across repos</span>
        </div>
        <RunBars runs={runs} slots={30} now={now} />
      </div>
    </Card>
  )
}

/* ---------- setup ---------- */

function SetupCard({ configured, token }: { configured: boolean; token: string }) {
  return (
    <Card className="overflow-hidden">
      <div className="grid @3xl:grid-cols-[1fr_1.1fr]">
        <EmptyState
          className="py-8"
          icon={<Workflow />}
          title={configured ? "Add a GitHub token" : "Connect your repositories"}
          description={
            configured
              ? "Lucidbench found no token. Export GITHUB_TOKEN, or sign in with the GitHub CLI, and runners and runs appear within seconds."
              : "List the repositories whose self-hosted runners and workflow runs you want here. Local runner containers already show below."
          }
        />
        <div className="space-y-3 border-t bg-muted/30 p-5 @3xl:border-l @3xl:border-t-0">
          <div className="text-xs font-medium text-muted-foreground">1 · Find your config file</div>
          <CopyCommand command="lucid config path" />
          <div className="pt-1 text-xs font-medium text-muted-foreground">2 · Add the ci section</div>
          <div className="relative">
            <pre className="overflow-x-auto rounded-lg border bg-background p-3 font-mono text-xs leading-5">{CONFIG_SNIPPET}</pre>
            <Button
              variant="ghost"
              size="sm"
              className="absolute right-1.5 top-1.5"
              onClick={() => void copyText(CONFIG_SNIPPET, "Snippet copied")}
            >
              Copy
            </Button>
          </div>
          <div className="flex items-center gap-2 pt-1 text-xs text-muted-foreground">
            <KeyRound className="size-3.5 text-subtle-foreground" />
            Token: {token === "none" ? "not found" : token === "gh" ? "from gh auth token" : "from the environment"}
            <span className="text-subtle-foreground">· in docker compose, export GITHUB_TOKEN</span>
          </div>
        </div>
      </div>
    </Card>
  )
}

function SourceErrors({ errors }: { errors: SourceError[] }) {
  if (errors.length === 0) return null
  return (
    <div className="space-y-2">
      {errors.map((e) => (
        <ErrorState
          key={`${e.source}:${e.message}`}
          title={e.source === "docker" ? "Docker is not reachable" : e.source === "github" ? "GitHub" : e.source}
          message={e.message}
        />
      ))}
    </div>
  )
}

/* ---------- fleet ---------- */

interface FleetEntry {
  key: string
  runner?: CIRunner
  container?: CIContainer
}

function fleet(runners: CIRunner[], containers: CIContainer[]): FleetEntry[] {
  const used = new Set<string>()
  const out: FleetEntry[] = runners.map((r) => {
    const c = r.container ? containers.find((x) => x.name === r.container) : undefined
    if (c) used.add(c.name)
    return { key: `r:${r.repo}/${r.id}`, runner: r, container: c }
  })
  for (const c of containers) if (!used.has(c.name)) out.push({ key: `c:${c.name}`, container: c })
  return out
}

/** Labels GitHub assigns itself; custom labels are highlighted. */
const DEFAULT_LABELS = new Set(["self-hosted", "Linux", "Windows", "macOS", "X64", "ARM64", "ARM"])

/** owner/name from a REPO_URL such as https://github.com/owner/name. */
function repoFromURL(url?: string): string | undefined {
  const m = url?.match(/github\.com\/([^/]+\/[^/]+?)(?:\.git)?\/?$/)
  return m?.[1]
}

function RunnerCard({ e, onAct }: { e: FleetEntry; onAct: (c: CIContainer, a: "start" | "stop" | "restart") => void }) {
  const { runner: r, container: c } = e
  const busy = !!r?.busy && r.status === "online"
  const repo = r?.repo ?? repoFromURL(c?.repo_url)
  const name = r?.name ?? c?.runner_name ?? c?.name ?? "runner"
  const status = r
    ? busy
      ? { tone: "info" as const, label: "Busy" }
      : r.status === "online"
        ? { tone: "success" as const, label: "Idle" }
        : { tone: "neutral" as const, label: "Offline" }
    : { tone: "neutral" as const, label: "Not watched" }
  const up = c?.state === "running"
  const labels = (r?.labels ?? []).filter((l) => l !== "self-hosted")
  return (
    <Card
      className={cn(
        "group relative flex flex-col overflow-hidden p-4 transition-[border-color,box-shadow] duration-200 hover:border-border-strong",
        busy && "border-info/40",
      )}
    >
      {busy && <div aria-hidden className="busy-shimmer absolute inset-x-0 top-0 h-0.5" />}
      <div className="flex items-start gap-3">
        <span className="mt-0.5 flex size-8 shrink-0 items-center justify-center rounded-lg border bg-muted/60">
          <RunnerDot status={r?.status ?? (up ? "unknown" : "offline")} busy={busy} />
        </span>
        <div className="min-w-0 flex-1">
          <div className="truncate font-mono text-sm font-medium" title={name}>
            {name}
          </div>
          <div className="mt-1 flex min-w-0 items-center gap-2">
            <StatusPill tone={status.tone} pulse={busy}>
              {status.label}
            </StatusPill>
            <span className="truncate text-xs text-muted-foreground" title={repo}>
              {repo ?? "no repository"}
            </span>
          </div>
        </div>
        {c && (
          <Menu
            label={`Actions for ${c.name}`}
            trigger={<Ellipsis />}
            items={[
              { label: "Start", icon: Play, disabled: up, onSelect: () => onAct(c, "start") },
              { label: "Restart", icon: RotateCw, disabled: !up, onSelect: () => onAct(c, "restart") },
              { label: "Stop", icon: Square, disabled: !up, danger: true, onSelect: () => onAct(c, "stop") },
            ]}
          />
        )}
      </div>

      {!r && (
        <p className="mt-3 text-xs text-subtle-foreground">
          {repo ? (
            <>
              Add <code className="font-mono text-muted-foreground">{repo}</code> to ci.github.repos to see its status and runs.
            </>
          ) : (
            "This container has no REPO_URL, so it cannot be matched to a repository."
          )}
        </p>
      )}

      {labels.length > 0 && (
        <div className="mt-3 flex flex-wrap gap-1">
          {labels.slice(0, 6).map((l) => (
            <Badge key={l} className={cn("font-mono", !DEFAULT_LABELS.has(l) && "border-brand/30 bg-brand-soft text-brand-fg")}>
              {l}
            </Badge>
          ))}
          {labels.length > 6 && <Badge>+{labels.length - 6}</Badge>}
        </div>
      )}

      <div className="mt-auto flex items-center gap-2 border-t pt-3 text-xs [&:not(:first-child)]:mt-3.5">
        <ContainerIcon className="size-3.5 shrink-0 text-subtle-foreground" />
        {c ? (
          <>
            <span className="min-w-0 truncate font-mono" title={c.image}>
              {c.name}
            </span>
            <span className="flex-1" />
            <span
              className={cn("shrink-0 tabular-nums", up ? "text-muted-foreground" : "text-warning-fg")}
              title={c.started_at ? `Started ${absoluteTime(c.started_at)}` : undefined}
            >
              {c.status}
            </span>
          </>
        ) : (
          <span className="text-subtle-foreground">Not running on this machine</span>
        )}
      </div>
    </Card>
  )
}

function FleetSkeleton() {
  return (
    <div className="grid gap-3 @2xl:grid-cols-2 @5xl:grid-cols-3">
      {[0, 1, 2].map((i) => (
        <Card key={i} className="space-y-3 p-4">
          <div className="flex items-center gap-3">
            <Skeleton className="size-8 rounded-lg" />
            <div className="flex-1 space-y-1.5">
              <Skeleton className="h-3.5 w-28" />
              <Skeleton className="h-3 w-36" />
            </div>
          </div>
          <Skeleton className="h-4 w-40" />
          <Skeleton className="h-3.5 w-full" />
        </Card>
      ))}
    </div>
  )
}

/* ---------- runs ---------- */

function RepoFilter({
  repos,
  value,
  onChange,
  runs,
}: {
  repos: string[]
  value: string
  onChange: (r: string) => void
  runs: CIRun[]
}) {
  const opts = ["", ...repos]
  return (
    <div role="radiogroup" aria-label="Repository" className="flex flex-wrap items-center gap-1 rounded-lg border bg-muted/40 p-0.5">
      {opts.map((r) => {
        const on = value === r
        const mine = r ? runs.filter((x) => x.repo === r) : runs
        const failing = mine.some(isFailure)
        return (
          <button
            key={r || "all"}
            role="radio"
            aria-checked={on}
            onClick={() => onChange(r)}
            className={cn(
              "flex h-6 items-center gap-1.5 rounded-md px-2 text-xs transition-colors",
              on ? "bg-elevated font-medium text-foreground shadow-card" : "text-muted-foreground hover:text-foreground",
            )}
          >
            {r ? shortRepo(r) : "All"}
            <span className="tabular-nums text-subtle-foreground">{mine.length}</span>
            {r && failing && <span aria-label="has failures" className="size-1.5 rounded-full bg-danger" />}
          </button>
        )
      })}
    </div>
  )
}

function RunsTable({ runs, now, onRerun }: { runs: CIRun[]; now: number; onRerun: (r: CIRun) => void }) {
  return (
    <div className="overflow-x-auto">
      <table className="w-full text-sm">
        <thead>
          <tr className="border-y bg-muted/40 text-left text-2xs font-medium uppercase tracking-[0.06em] text-subtle-foreground">
            <th className="py-2 pl-5 pr-3 font-medium">Workflow</th>
            <th className="px-3 py-2 font-medium">Repository</th>
            <th className="px-3 py-2 font-medium">Branch</th>
            <th className="px-3 py-2 text-right font-medium">Duration</th>
            <th className="px-3 py-2 text-right font-medium">Started</th>
            <th className="w-32 py-2 pl-3 pr-5" />
          </tr>
        </thead>
        <tbody>
          {runs.map((r) => {
            const st = runState(r)
            return (
              <tr key={`${r.repo}-${r.id}`} className="group border-b transition-colors last:border-b-0 hover:bg-accent/40">
                <td className="max-w-0 py-2.5 pl-5 pr-3 @4xl:w-[38%]">
                  <div className="flex min-w-0 items-center gap-2.5">
                    <RunIcon run={r} />
                    <div className="min-w-0">
                      <div className="flex items-baseline gap-1.5">
                        <span className="truncate font-medium">{r.name}</span>
                        <span className="shrink-0 font-mono text-2xs text-subtle-foreground">#{r.run_number}</span>
                      </div>
                      <div className="truncate text-xs text-muted-foreground" title={r.title}>
                        {r.title || r.event}
                      </div>
                    </div>
                  </div>
                </td>
                <td className="whitespace-nowrap px-3 py-2.5 text-xs text-muted-foreground" title={r.repo}>
                  {shortRepo(r.repo)}
                </td>
                <td className="max-w-44 px-3 py-2.5">
                  <Badge className="max-w-full font-mono" title={`${r.branch} · ${r.event}`}>
                    <GitBranch />
                    <span className="truncate">{r.branch}</span>
                  </Badge>
                </td>
                <td className="whitespace-nowrap px-3 py-2.5 text-right font-mono text-xs tabular-nums text-muted-foreground">
                  {st === "queued" ? "-" : formatDuration(runDuration(r, now))}
                </td>
                <td className="whitespace-nowrap px-3 py-2.5 text-right text-xs tabular-nums text-muted-foreground">
                  <time dateTime={r.created_at} title={absoluteTime(r.created_at)}>
                    {relativeTime(r.created_at, now)}
                  </time>
                </td>
                <td className="py-2.5 pl-3 pr-5">
                  <div className="flex items-center justify-end gap-1">
                    {isFailure(r) && (
                      <Button variant="secondary" size="sm" onClick={() => onRerun(r)}>
                        <RotateCw /> Re-run failed
                      </Button>
                    )}
                    <a
                      href={r.html_url}
                      target="_blank"
                      rel="noreferrer"
                      aria-label={`Open ${r.name} #${r.run_number} on GitHub`}
                      title="Open on GitHub"
                      className="inline-flex size-7 items-center justify-center rounded-md text-subtle-foreground transition-colors hover:bg-accent hover:text-foreground"
                    >
                      <ExternalLink className="size-3.5" />
                    </a>
                  </div>
                </td>
              </tr>
            )
          })}
        </tbody>
      </table>
      <span className="sr-only">{runs.length} runs. Result shown by icon: {Object.values(RUN_STATE).map((s) => s.label).join(", ")}.</span>
    </div>
  )
}

/* ---------- page ---------- */

export default function Runners() {
  const summary = usePoll<CISummary>("/api/ci/summary", CI_POLL_MS)
  const fleetData = usePoll<CIRunners>("/api/ci/runners", CI_POLL_MS)
  const runsData = usePoll<CIRuns>("/api/ci/runs", CI_POLL_MS)
  const now = useNow(5000)
  const [repo, setRepo] = useState("")
  const [confirm, setConfirm] = useState<ConfirmRequest | null>(null)

  const s = summary.data
  const runs = runsData.data?.runs ?? []
  const shown = repo ? runs.filter((r) => r.repo === repo) : runs
  const entries = useMemo(
    () => fleet(fleetData.data?.runners ?? [], fleetData.data?.containers ?? []),
    [fleetData.data],
  )
  const errors = s?.errors ?? []
  const needsSetup = s && (!s.configured || s.token_source === "none")
  const refreshing = summary.refreshing || fleetData.refreshing || runsData.refreshing
  const updatedAt = Math.min(...[summary.updatedAt, fleetData.updatedAt, runsData.updatedAt].map((t) => t ?? Infinity))

  const act = (c: CIContainer, a: "start" | "stop" | "restart") =>
    setConfirm({
      title: `${a[0].toUpperCase()}${a.slice(1)} ${c.name}?`,
      description:
        a === "stop"
          ? "The runner goes offline and picks up no jobs until it is started again. A job running on it now is interrupted."
          : a === "restart"
            ? "The container restarts. An ephemeral runner re-registers with GitHub; a job running on it now is interrupted."
            : "The container starts and the runner registers with GitHub again.",
      confirmLabel: a === "stop" ? "Stop runner" : a === "restart" ? "Restart runner" : "Start runner",
      danger: a !== "start",
      run: async () => {
        if (await containerAction(c.name, a)) refreshAll()
      },
    })
  const rerun = (r: CIRun) =>
    setConfirm({
      title: "Re-run failed jobs?",
      description: `${r.repo} · ${r.name} #${r.run_number} on ${r.branch}. GitHub queues only the jobs that failed.`,
      confirmLabel: "Re-run failed jobs",
      run: async () => {
        if (await rerunFailed(r)) setTimeout(refreshAll, 1500)
      },
    })

  return (
    <div className="space-y-6">
      <PageHeader
        icon={<Workflow />}
        title="Runners & CI"
        description="Your self-hosted GitHub Actions runners, the containers behind them, and every recent workflow run."
        actions={<RefreshButton refreshing={refreshing} updatedAt={Number.isFinite(updatedAt) ? updatedAt : null} />}
      />

      <SummaryStrip s={s} runs={runs} now={now} />
      {needsSetup && <SetupCard configured={s.configured} token={s.token_source} />}
      <SourceErrors errors={errors.filter((e) => !(needsSetup && e.source === "github"))} />
      {summary.error && !s && <ErrorState title="Could not load CI status" message={summary.error.message} onRetry={summary.refresh} />}

      <section aria-labelledby="fleet-h" className="space-y-3">
        <div className="flex items-baseline justify-between gap-3">
          <h2 id="fleet-h" className="text-sm font-semibold">
            Fleet
            {entries.length > 0 && <span className="ml-2 font-normal tabular-nums text-subtle-foreground">{entries.length}</span>}
          </h2>
          <span className="text-xs text-subtle-foreground">Container actions ask before they run</span>
        </div>
        {fleetData.loading && !fleetData.data ? (
          <FleetSkeleton />
        ) : entries.length === 0 ? (
          <Card className="border-dashed">
            <EmptyState
              icon={<ContainerIcon />}
              title="No runners yet"
              description="No self-hosted runner is registered on your repositories, and no runner container runs on this machine."
            />
          </Card>
        ) : (
          <div className="grid gap-3 @2xl:grid-cols-2 @5xl:grid-cols-3">
            {entries.map((e) => (
              <RunnerCard key={e.key} e={e} onAct={act} />
            ))}
          </div>
        )}
      </section>

      <Card className="overflow-hidden">
        <div className="flex flex-wrap items-center justify-between gap-3 px-5 py-4">
          <div>
            <h2 className="text-sm font-semibold">Workflow runs</h2>
            <p className="mt-0.5 text-sm text-muted-foreground">The last 10 runs of each repository, newest first.</p>
          </div>
          {runsData.data && runsData.data.repos.length > 1 && (
            <RepoFilter repos={runsData.data.repos} value={repo} onChange={setRepo} runs={runs} />
          )}
        </div>
        {runsData.loading && !runsData.data ? (
          <div className="space-y-2 border-t p-5">
            {[0, 1, 2, 3].map((i) => (
              <Skeleton key={i} className="h-9" />
            ))}
          </div>
        ) : shown.length === 0 ? (
          <div className="border-t">
            <EmptyState
              icon={<Workflow />}
              title={runsData.data?.repos.length ? "No workflow runs" : "No repositories configured"}
              description={
                runsData.data?.repos.length
                  ? "Runs appear here as soon as a workflow starts."
                  : "Add repositories to ci.github.repos to see their runs."
              }
            />
          </div>
        ) : (
          <RunsTable runs={shown} now={now} onRerun={rerun} />
        )}
      </Card>

      <ConfirmDialog request={confirm} onClose={() => setConfirm(null)} />
    </div>
  )
}
