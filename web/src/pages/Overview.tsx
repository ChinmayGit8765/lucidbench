import { useState, type ReactNode } from "react"
import {
  ArrowRight,
  Blocks,
  Boxes,
  CircleCheck,
  Container as ContainerIcon,
  ExternalLink,
  Gauge,
  KeyRound,
  OctagonAlert,
  Play,
  Plus,
  RotateCw,
  ServerCog,
  TriangleAlert,
  Workflow,
  type LucideIcon,
} from "lucide-react"

import { RunIcon, RunnerDot } from "@/components/ci"
import { PROVIDERS } from "@/components/ProviderMark"
import { PageHeader, RefreshButton } from "@/components/Shell"
import { SpriteBoard } from "@/components/ThemeArt"
import { Button } from "@/components/ui/button"
import { Card } from "@/components/ui/card"
import { ConfirmDialog, type ConfirmRequest } from "@/components/ui/confirm"
import { Skeleton } from "@/components/ui/states"
import { refreshAll, usePoll } from "@/lib/api"
import { useApp } from "@/lib/app"
import {
  CI_POLL_MS,
  containerAction,
  failingRuns,
  formatDuration,
  rerunFailed,
  runDuration,
  runState,
  shortRepo,
  type CIContainer,
  type CIRun,
  type CIRunners,
  type CIRuns,
  type CISummary,
} from "@/lib/ci"
import { runHelloJob, type ClusterInfo, type Job } from "@/lib/jobs"
import { usePrefs } from "@/lib/prefs"
import { blockedNeeds, PROJECTS_POLL_MS, type ProjectList } from "@/lib/projects"
import { absoluteTime, relativeTime, useNow } from "@/lib/time"
import { resetsIn, usageAlerts, useUsageSummary } from "@/lib/usage"
import { cn, isMac } from "@/lib/utils"
import { isOpenable, moduleById, navOrder } from "@/modules/registry"
import { useWorkAttention } from "@/modules/work"
import type { Account } from "@/pages/Accounts"

function greeting(d: Date): string {
  const h = d.getHours()
  if (h < 5) return "Working late"
  if (h < 12) return "Good morning"
  if (h < 18) return "Good afternoon"
  return "Good evening"
}

/* ---------- needs attention ---------- */

interface Attention {
  key: string
  icon: ReactNode
  title: ReactNode
  meta: ReactNode
  action?: ReactNode
}

function AttentionRow({ a }: { a: Attention }) {
  return (
    <li className="flex items-center gap-3 px-5 py-2.5 transition-colors hover:bg-accent/30">
      <span className="flex size-7 shrink-0 items-center justify-center rounded-md border bg-background/60">{a.icon}</span>
      <div className="min-w-0 flex-1">
        <div className="truncate text-sm font-medium">{a.title}</div>
        <div className="truncate text-xs text-muted-foreground">{a.meta}</div>
      </div>
      {a.action && <div className="flex shrink-0 items-center gap-1">{a.action}</div>}
    </li>
  )
}

/* ---------- activity ---------- */

interface ActivityItem {
  key: string
  at: string
  icon: ReactNode
  title: ReactNode
  meta: ReactNode
  right?: ReactNode
  href?: string
}

/* ---------- page ---------- */

export default function Overview() {
  const { open, openProject: onOpenProject, addAccount: onAddAccount } = useApp()
  const { prefs, label } = usePrefs()
  const ciOn = isOpenable(moduleById("runners")!, prefs)
  const tiles = navOrder(prefs).filter((m) => m.overviewTile && isOpenable(m, prefs))
  const now = useNow(5000)
  const accounts = usePoll<Account[]>("/api/accounts", 15000)
  const cluster = usePoll<ClusterInfo>("/api/cluster", 15000)
  const jobs = usePoll<Job[]>("/api/jobs", 15000)
  const summary = usePoll<CISummary>("/api/ci/summary", CI_POLL_MS)
  const runners = usePoll<CIRunners>("/api/ci/runners", CI_POLL_MS)
  const runsPoll = usePoll<CIRuns>("/api/ci/runs", CI_POLL_MS)
  const projectsPoll = usePoll<ProjectList>("/api/projects", PROJECTS_POLL_MS)
  const usagePoll = useUsageSummary()
  const [confirm, setConfirm] = useState<ConfirmRequest | null>(null)
  const [helloBusy, setHelloBusy] = useState(false)

  const all = [accounts, cluster, jobs, summary, runners, runsPoll, projectsPoll]
  const refreshing = all.some((p) => p.refreshing)
  const updated = all.map((p) => p.updatedAt).filter((t): t is number => t !== null)

  const accs = accounts.data ?? []
  const expired = accs.filter((a) => a.status === "expired")
  const s = summary.data
  const runs = runsPoll.data?.runs ?? []
  const failing = failingRuns(runs)
  const fleet = runners.data?.runners ?? []
  const containers = runners.data?.containers ?? []
  const c = cluster.data
  const jobList = jobs.data ?? []
  const pl = projectsPoll.data
  const projectList = pl?.projects ?? []
  const blocked = blockedNeeds(projectList)
  const workAttention = useWorkAttention()


  const rerun = (r: CIRun) =>
    setConfirm({
      title: "Re-run failed jobs?",
      description: `${r.repo} · ${r.name} #${r.run_number} on ${r.branch}. GitHub queues only the jobs that failed.`,
      confirmLabel: "Re-run failed jobs",
      run: async () => {
        if (await rerunFailed(r)) setTimeout(refreshAll, 1500)
      },
    })
  const start = (ct: CIContainer) =>
    setConfirm({
      title: `Start ${ct.name}?`,
      description: "The container starts and the runner registers with GitHub again.",
      confirmLabel: "Start runner",
      run: async () => {
        if (await containerAction(ct.name, "start")) refreshAll()
      },
    })
  const hello = async () => {
    setHelloBusy(true)
    await runHelloJob()
    setHelloBusy(false)
  }

  /* needs attention */
  const attention: Attention[] = []
  for (const r of ciOn ? failing.slice(0, 6) : []) {
    attention.push({
      key: `run-${r.repo}-${r.id}`,
      icon: <RunIcon run={r} />,
      title: (
        <>
          {r.name} failed on <span className="font-mono text-[0.92em]">{r.branch}</span>
        </>
      ),
      meta: (
        <>
          {shortRepo(r.repo)} · #{r.run_number} · {relativeTime(r.created_at, now)}
        </>
      ),
      action: (
        <>
          <Button variant="secondary" size="sm" onClick={() => rerun(r)}>
            <RotateCw /> Re-run
          </Button>
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
        </>
      ),
    })
  }
  for (const r of ciOn ? fleet.filter((x) => x.status !== "online") : []) {
    attention.push({
      key: `runner-${r.repo}-${r.id}`,
      icon: <RunnerDot status="offline" />,
      title: (
        <>
          Runner <span className="font-mono text-[0.92em]">{r.name}</span> is offline
        </>
      ),
      meta: shortRepo(r.repo),
      action: (
        <Button variant="ghost" size="sm" onClick={() => open("runners")}>
          View <ArrowRight />
        </Button>
      ),
    })
  }
  for (const ct of ciOn ? containers.filter((x) => x.state !== "running") : []) {
    attention.push({
      key: `ct-${ct.name}`,
      icon: <ContainerIcon className="size-3.5 text-warning" />,
      title: (
        <>
          Container <span className="font-mono text-[0.92em]">{ct.name}</span> is stopped
        </>
      ),
      meta: ct.status,
      action: (
        <Button variant="secondary" size="sm" onClick={() => start(ct)}>
          <Play /> Start
        </Button>
      ),
    })
  }
  for (const { project, need } of blocked.slice(0, 6)) {
    const from = need.from ? projectList.find((p) => p.id === need.from) : undefined
    attention.push({
      key: `need-${project.id}-${need.what}`,
      icon: <OctagonAlert className="size-3.5 text-warning" />,
      title: (
        <>
          {project.name} is blocked on {need.what}
        </>
      ),
      meta: from ? `needs it from ${from.name}` : `${project.progress.done}/${project.progress.total} needs done`,
      action: (
        <Button variant="ghost" size="sm" onClick={() => onOpenProject(project.id)}>
          View <ArrowRight />
        </Button>
      ),
    })
  }
  attention.push(...workAttention)
  for (const a of expired) {
    attention.push({
      key: `acc-${a.provider}-${a.name}`,
      icon: <KeyRound className="size-3.5 text-warning" />,
      title: (
        <>
          {PROVIDERS.find((p) => p.id === a.provider)?.label ?? a.provider} sign-in expired
        </>
      ),
      meta: <span className="font-mono">{a.name}</span>,
      action: (
        <Button variant="ghost" size="sm" onClick={() => open("accounts")}>
          Fix <ArrowRight />
        </Button>
      ),
    })
  }
  for (const a of usageAlerts(usagePoll.data)) {
    attention.push({
      key: `usage-${a.provider}-${a.window.name}`,
      icon: <Gauge className="size-3.5 text-warning" />,
      title: (
        <>
          {a.providerLabel} {a.window.label} {label("usage_meter", "usage")} at {Math.round(a.percent)}%
        </>
      ),
      meta: resetsIn(a.window.resets_at, now) ? `resets in ${resetsIn(a.window.resets_at, now)}` : "reset time unknown",
      action: (
        <Button variant="ghost" size="sm" onClick={() => open("usage")}>
          View <ArrowRight />
        </Button>
      ),
    })
  }
  if (!ciOn) {
    // CI is an extension; nothing to report when it is not added.
  } else if (s && !s.configured) {
    attention.push({
      key: "ci-setup",
      icon: <Workflow className="size-3.5 text-info" />,
      title: "Watch your CI",
      meta: "Add repositories to ci.github.repos to see runners and runs",
      action: (
        <Button variant="ghost" size="sm" onClick={() => open("runners")}>
          Set up <ArrowRight />
        </Button>
      ),
    })
  } else if (s?.token_source === "none") {
    attention.push({
      key: "ci-token",
      icon: <KeyRound className="size-3.5 text-warning" />,
      title: "No GitHub token",
      meta: "Export GITHUB_TOKEN or sign in with gh auth login",
      action: (
        <Button variant="ghost" size="sm" onClick={() => open("runners")}>
          Details <ArrowRight />
        </Button>
      ),
    })
  }
  for (const e of ciOn ? (s?.errors ?? []) : []) {
    if (e.source === "github") continue
    attention.push({
      key: `err-${e.source}-${e.message}`,
      icon: <TriangleAlert className="size-3.5 text-danger" />,
      title: e.source === "docker" ? "Docker is not reachable" : `Cannot read ${shortRepo(e.source)}`,
      meta: e.message,
    })
  }
  const attentionLoading = (summary.loading && !s) || (runsPoll.loading && !runsPoll.data)

  /* activity */
  const activity: ActivityItem[] = [
    ...(ciOn ? runs : []).slice(0, 12).map(
      (r): ActivityItem => ({
        key: `run-${r.repo}-${r.id}`,
        at: r.created_at,
        icon: <RunIcon run={r} />,
        title: (
          <>
            <span className="font-medium">{r.name}</span>
            <span className="text-muted-foreground"> · {r.title || r.event}</span>
          </>
        ),
        meta: (
          <>
            {shortRepo(r.repo)} · <span className="font-mono">{r.branch}</span>
          </>
        ),
        right: runState(r) === "queued" ? "queued" : formatDuration(runDuration(r, now)),
        href: r.html_url,
      }),
    ),
    ...jobList.map(
      (j): ActivityItem => ({
        key: `job-${j.name}`,
        at: j.createdAt,
        icon: <Boxes className={cn("size-4", j.status === "Failed" ? "text-danger" : j.status === "Completed" ? "text-success" : "text-info")} />,
        title: (
          <>
            <span className="font-medium">Job</span>
            <span className="text-muted-foreground"> · {j.name}</span>
          </>
        ),
        meta: <>cluster · {j.status.toLowerCase() || "unknown"}</>,
      }),
    ),
  ]
    .sort((a, b) => Date.parse(b.at) - Date.parse(a.at))
    .slice(0, 8)

  const today = new Date(now)
  return (
    <div className="space-y-6">
      <PageHeader
        eyebrow={today.toLocaleDateString(undefined, { weekday: "long", day: "numeric", month: "long" })}
        title={label("overview_title", greeting(today))}
        description={
          attentionLoading
            ? "Checking your workspace…"
            : attention.length === 0
              ? "Everything is running. Nothing needs you right now."
              : `${attention.length} ${attention.length === 1 ? "thing needs" : "things need"} your attention.`
        }
        actions={<RefreshButton refreshing={refreshing} updatedAt={updated.length ? Math.min(...updated) : null} />}
      />

      <div className="grid grid-cols-2 gap-3 @3xl:grid-cols-3">
        {tiles.map((m) => {
          const Tile = m.overviewTile!
          return <Tile key={m.id} />
        })}
      </div>

      <SpriteBoard />

      <div className="grid gap-4 @4xl:grid-cols-[minmax(0,1.55fr)_minmax(0,1fr)]">
        <div className="space-y-4">
          <Card className="overflow-hidden">
            <div className="flex items-center justify-between px-5 pb-3 pt-4">
              <h2 className="flex items-center gap-2 text-sm font-semibold">
                {label("attention", "Needs attention")}
                {attention.length > 0 && (
                  <span className="rounded-full bg-danger-soft px-1.5 text-2xs tabular-nums text-danger-fg">{attention.length}</span>
                )}
              </h2>
            </div>
            {attentionLoading ? (
              <div className="space-y-2 border-t p-5">
                <Skeleton className="h-9" />
                <Skeleton className="h-9" />
              </div>
            ) : attention.length === 0 ? (
              <div className="flex items-center gap-3 border-t px-5 py-5">
                <span className="flex size-8 items-center justify-center rounded-full bg-success-soft text-success">
                  <CircleCheck className="size-4" />
                </span>
                <div>
                  <div className="text-sm font-medium">{label("all_clear", "All clear")}</div>
                  <div className="text-xs text-muted-foreground">No failed runs, offline runners, expired sign-ins or blocked needs.</div>
                </div>
              </div>
            ) : (
              <ul className="divide-y border-t">
                {attention.map((a) => (
                  <AttentionRow key={a.key} a={a} />
                ))}
              </ul>
            )}
          </Card>

          <Card className="overflow-hidden">
            <div className="flex items-center justify-between px-5 pb-3 pt-4">
              <h2 className="text-sm font-semibold">Recent activity</h2>
              {ciOn && (
                <Button variant="ghost" size="sm" onClick={() => open("runners")}>
                  All runs <ArrowRight />
                </Button>
              )}
            </div>
            {runsPoll.loading && !runsPoll.data ? (
              <div className="space-y-2 border-t p-5">
                {[0, 1, 2, 3].map((i) => (
                  <Skeleton key={i} className="h-8" />
                ))}
              </div>
            ) : activity.length === 0 ? (
              <p className="border-t px-5 py-6 text-center text-sm text-muted-foreground">
                No runs or jobs yet. Activity from CI and the local cluster shows up here.
              </p>
            ) : (
              <ol className="relative border-t py-1.5">
                <span aria-hidden className="absolute bottom-4 left-[29px] top-4 w-px bg-border" />
                {activity.map((it) => {
                  const body = (
                    <>
                      <span className="relative z-[1] flex size-6 shrink-0 items-center justify-center rounded-full bg-card">
                        {it.icon}
                      </span>
                      <div className="min-w-0 flex-1">
                        <div className="truncate text-sm">{it.title}</div>
                        <div className="truncate text-xs text-subtle-foreground">{it.meta}</div>
                      </div>
                      {it.right && (
                        <span className="shrink-0 font-mono text-2xs tabular-nums text-subtle-foreground">{it.right}</span>
                      )}
                      <time
                        dateTime={it.at}
                        title={absoluteTime(it.at)}
                        className="w-16 shrink-0 text-right text-xs tabular-nums text-muted-foreground"
                      >
                        {relativeTime(it.at, now)}
                      </time>
                    </>
                  )
                  return (
                    <li key={it.key}>
                      {it.href ? (
                        <a
                          href={it.href}
                          target="_blank"
                          rel="noreferrer"
                          className="flex items-center gap-3 px-4 py-1.5 outline-none transition-colors hover:bg-accent/30 focus-visible:bg-accent/50"
                        >
                          {body}
                        </a>
                      ) : (
                        <div className="flex items-center gap-3 px-4 py-1.5">{body}</div>
                      )}
                    </li>
                  )
                })}
              </ol>
            )}
          </Card>
        </div>

        <div className="space-y-4">
          <Card className="p-4">
            <h2 className="mb-3 text-sm font-semibold">Quick actions</h2>
            <div className="grid gap-2">
              <QuickAction
                icon={Play}
                title="Run hello job"
                hint={c?.running ? "Check the cluster end to end" : "Needs a running cluster"}
                disabled={!c?.running || helloBusy}
                onClick={() => void hello()}
              />
              <QuickAction icon={Plus} title="Add account" hint="Sign in another provider profile" onClick={onAddAccount} />
              {ciOn ? (
                <QuickAction
                  icon={ServerCog}
                  title="Open Runners & CI"
                  hint="Fleet, containers and every run"
                  onClick={() => open("runners")}
                />
              ) : (
                <QuickAction
                  icon={Blocks}
                  title="Browse extensions"
                  hint="Add Runners & CI, Containers and more"
                  onClick={() => open("settings", ["extensions"])}
                />
              )}
            </div>
            <p className="mt-3 flex items-center gap-1.5 text-xs text-subtle-foreground">
              Everything else:
              <kbd className="rounded border bg-muted px-1 font-sans text-2xs">{isMac() ? "⌘" : "Ctrl"}</kbd>
              <kbd className="rounded border bg-muted px-1 font-sans text-2xs">K</kbd>
            </p>
          </Card>

          {ciOn && (
          <Card className="overflow-hidden">
            <div className="flex items-center justify-between px-4 pb-2 pt-4">
              <h2 className="text-sm font-semibold">Fleet</h2>
              <span className="text-xs tabular-nums text-subtle-foreground">
                {containers.filter((x) => x.state === "running").length}/{containers.length} containers up
              </span>
            </div>
            {runners.loading && !runners.data ? (
              <div className="space-y-2 p-4 pt-1">
                <Skeleton className="h-7" />
                <Skeleton className="h-7" />
              </div>
            ) : containers.length === 0 && fleet.length === 0 ? (
              <p className="px-4 pb-4 text-xs text-muted-foreground">No self-hosted runners or runner containers found.</p>
            ) : (
              <ul className="pb-2">
                {containers.map((ct) => {
                  const r = fleet.find((x) => x.container === ct.name)
                  return (
                    <li key={ct.name} className="flex items-center gap-2.5 px-4 py-1.5 text-sm">
                      <RunnerDot status={r ? r.status : ct.state === "running" ? "unknown" : "offline"} busy={r?.busy} />
                      <span className="min-w-0 flex-1 truncate font-mono text-xs" title={ct.name}>
                        {r?.name ?? ct.runner_name ?? ct.name}
                      </span>
                      <span className="shrink-0 text-xs text-subtle-foreground">
                        {r ? (r.busy ? "busy" : r.status === "online" ? "idle" : "offline") : ct.state === "running" ? "up · not watched" : "stopped"}
                      </span>
                    </li>
                  )
                })}
                {fleet
                  .filter((r) => !r.container)
                  .map((r) => (
                    <li key={`${r.repo}-${r.id}`} className="flex items-center gap-2.5 px-4 py-1.5 text-sm">
                      <RunnerDot status={r.status} busy={r.busy} />
                      <span className="min-w-0 flex-1 truncate font-mono text-xs">{r.name}</span>
                      <span className="shrink-0 text-xs text-subtle-foreground">{r.busy ? "busy" : r.status}</span>
                    </li>
                  ))}
              </ul>
            )}
          </Card>
          )}
        </div>
      </div>

      <ConfirmDialog request={confirm} onClose={() => setConfirm(null)} />
    </div>
  )
}

function QuickAction({
  icon: Icon,
  title,
  hint,
  onClick,
  disabled,
}: {
  icon: LucideIcon
  title: string
  hint: string
  onClick: () => void
  disabled?: boolean
}) {
  return (
    <button
      onClick={onClick}
      disabled={disabled}
      className="group flex items-center gap-3 rounded-lg border bg-background/40 px-3 py-2.5 text-left outline-none transition-[border-color,background-color] duration-150 hover:border-border-strong hover:bg-accent/40 focus-visible:ring-2 focus-visible:ring-ring disabled:cursor-not-allowed disabled:opacity-55"
    >
      <span className="flex size-8 shrink-0 items-center justify-center rounded-md bg-brand-soft text-brand-fg">
        <Icon className="size-4" />
      </span>
      <span className="min-w-0 flex-1">
        <span className="block text-sm font-medium">{title}</span>
        <span className="block truncate text-xs text-muted-foreground">{hint}</span>
      </span>
      <ArrowRight className="size-4 text-subtle-foreground transition-transform group-hover:translate-x-0.5 group-disabled:hidden" />
    </button>
  )
}

