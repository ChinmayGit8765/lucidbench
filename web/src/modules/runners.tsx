import { lazy, useEffect, useState } from "react"
import {
  ArrowRight,
  Container as ContainerIcon,
  ExternalLink,
  KeyRound,
  Play,
  RotateCw,
  ServerCog,
  TriangleAlert,
  Workflow,
} from "lucide-react"

import { RunBars, RunIcon, RunnerDot } from "@/components/ci"
import type { Command } from "@/components/CommandPalette"
import { StatTile } from "@/components/StatTile"
import { StatusPill } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { ConfirmDialog, type ConfirmRequest } from "@/components/ui/confirm"
import { getJSON, refreshAll, usePoll } from "@/lib/api"
import { useApp } from "@/lib/app"
import {
  CI_POLL_MS,
  containerAction,
  failingRuns,
  isFailure,
  rerunFailed,
  runState,
  shortRepo,
  type CIContainer,
  type CIRun,
  type CIRunners,
  type CIRuns,
  type CISummary,
} from "@/lib/ci"
import { relativeTime, useNow } from "@/lib/time"
import { cn } from "@/lib/utils"
import type { AttentionItem, ModuleDef } from "@/modules/types"

function RunnersTile() {
  const { open } = useApp()
  const now = useNow(5000)
  const summary = usePoll<CISummary>("/api/ci/summary", CI_POLL_MS)
  const runsPoll = usePoll<CIRuns>("/api/ci/runs", CI_POLL_MS)
  const s = summary.data
  const runs = runsPoll.data?.runs ?? []
  const failing = failingRuns(runs)
  const recent = runs.slice(0, 30)
  const pass = recent.filter((r) => runState(r) === "success").length
  const fail = recent.filter(isFailure).length
  const rate24 = s?.runs_24h.pass_rate ?? null
  const rate = rate24 ?? (pass + fail > 0 ? pass / (pass + fail) : null)
  return (
    <StatTile
      icon={ServerCog}
      label="Runners & CI"
      onOpen={() => open("runners")}
      loading={(runsPoll.loading && !runsPoll.data) || (summary.loading && !s)}
      value={
        <span className={cn(rate !== null && rate < 0.8 && "text-danger-fg")}>
          {rate === null ? "-" : `${Math.round(rate * 100)}%`}
          <span className="text-base font-normal text-subtle-foreground"> pass</span>
        </span>
      }
      aside={
        failing.length > 0 ? (
          <StatusPill tone="danger">{failing.length} failing</StatusPill>
        ) : s && s.runners.busy > 0 ? (
          <StatusPill tone="info" pulse>
            {s.runners.busy} busy
          </StatusPill>
        ) : runs.length > 0 ? (
          <StatusPill tone="success">green</StatusPill>
        ) : undefined
      }
      sub={
        s
          ? `${s.runners.online}/${s.runners.total} runners online · ${rate24 !== null ? "last 24h" : `last ${recent.length} runs`}`
          : (summary.error?.message ?? "not configured")
      }
      footer={<RunBars runs={runs} slots={24} now={now} className="h-6" />}
    />
  )
}

/** "Re-run last failed run": fetches fresh runs while the palette is open. */
function useRunnerCommands(paletteOpen: boolean): Command[] {
  const [lastFailed, setLastFailed] = useState<CIRun | null | undefined>(undefined)
  useEffect(() => {
    if (!paletteOpen) return
    let cancelled = false
    setLastFailed(undefined)
    getJSON<CIRuns>("/api/ci/runs")
      .then((r) => !cancelled && setLastFailed(failingRuns(r.runs)[0] ?? null))
      .catch(() => !cancelled && setLastFailed(null))
    return () => {
      cancelled = true
    }
  }, [paletteOpen])
  return [
    {
      id: "rerun-last-failed",
      label: "Re-run last failed run",
      group: "Actions",
      icon: RotateCw,
      disabled: !lastFailed,
      hint:
        lastFailed === undefined
          ? "checking…"
          : lastFailed
            ? `${lastFailed.name} · ${shortRepo(lastFailed.repo)}`
            : "nothing failing",
      keywords: "ci github retry",
      run: () => {
        if (lastFailed) void rerunFailed(lastFailed).then((ok) => ok && setTimeout(refreshAll, 1500))
      },
    },
  ]
}

/** Re-runs a run's failed jobs after the user confirms. */
function RerunButton({ run: r }: { run: CIRun }) {
  const [confirm, setConfirm] = useState<ConfirmRequest | null>(null)
  return (
    <>
      <Button
        variant="secondary"
        size="sm"
        onClick={() =>
          setConfirm({
            title: "Re-run failed jobs?",
            description: `${r.repo} · ${r.name} #${r.run_number} on ${r.branch}. GitHub queues only the jobs that failed.`,
            confirmLabel: "Re-run failed jobs",
            run: async () => {
              if (await rerunFailed(r)) setTimeout(refreshAll, 1500)
            },
          })
        }
      >
        <RotateCw /> Re-run
      </Button>
      <ConfirmDialog request={confirm} onClose={() => setConfirm(null)} />
    </>
  )
}

/** Starts a stopped runner container after the user confirms. */
function StartContainerButton({ container: ct }: { container: CIContainer }) {
  const [confirm, setConfirm] = useState<ConfirmRequest | null>(null)
  return (
    <>
      <Button
        variant="secondary"
        size="sm"
        onClick={() =>
          setConfirm({
            title: `Start ${ct.name}?`,
            description: "The container starts and the runner registers with GitHub again.",
            confirmLabel: "Start runner",
            run: async () => {
              if (await containerAction(ct.name, "start")) refreshAll()
            },
          })
        }
      >
        <Play /> Start
      </Button>
      <ConfirmDialog request={confirm} onClose={() => setConfirm(null)} />
    </>
  )
}

/** Needs attention: failed runs, offline runners, stopped runner containers and CI setup problems. */
function useRunnerAttention(): AttentionItem[] | null {
  const { open } = useApp()
  const now = useNow(5000)
  const summary = usePoll<CISummary>("/api/ci/summary", CI_POLL_MS)
  const runners = usePoll<CIRunners>("/api/ci/runners", CI_POLL_MS)
  const runsPoll = usePoll<CIRuns>("/api/ci/runs", CI_POLL_MS)
  const answered = (p: { data: unknown; error: unknown }) => p.data !== null || p.error !== null
  if (!answered(summary) || !answered(runsPoll)) return null
  const s = summary.data
  const out: AttentionItem[] = []
  const view = (
    <Button variant="ghost" size="sm" onClick={() => open("runners")}>
      View <ArrowRight />
    </Button>
  )
  for (const r of failingRuns(runsPoll.data?.runs ?? []).slice(0, 6)) {
    out.push({
      key: `run-${r.repo}-${r.id}`,
      severity: "danger",
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
          <RerunButton run={r} />
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
  for (const r of (runners.data?.runners ?? []).filter((x) => x.status !== "online")) {
    out.push({
      key: `runner-${r.repo}-${r.id}`,
      severity: "warning",
      icon: <RunnerDot status="offline" />,
      title: (
        <>
          Runner <span className="font-mono text-[0.92em]">{r.name}</span> is offline
        </>
      ),
      meta: shortRepo(r.repo),
      action: view,
    })
  }
  for (const ct of (runners.data?.containers ?? []).filter((x) => x.state !== "running")) {
    out.push({
      key: `ct-${ct.name}`,
      severity: "warning",
      icon: <ContainerIcon className="size-3.5 text-warning" />,
      title: (
        <>
          Container <span className="font-mono text-[0.92em]">{ct.name}</span> is stopped
        </>
      ),
      meta: ct.status,
      action: <StartContainerButton container={ct} />,
    })
  }
  if (s && !s.configured) {
    out.push({
      key: "ci-setup",
      severity: "info",
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
    out.push({
      key: "ci-token",
      severity: "warning",
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
  for (const e of s?.errors ?? []) {
    if (e.source === "github") continue
    out.push({
      key: `err-${e.source}-${e.message}`,
      severity: "danger",
      icon: <TriangleAlert className="size-3.5 text-danger" />,
      title: e.source === "docker" ? "Docker is not reachable" : `Cannot read ${shortRepo(e.source)}`,
      meta: e.message,
    })
  }
  return out
}

export const runners: ModuleDef = {
  id: "runners",
  title: "Runners & CI",
  icon: ServerCog,
  route: "/runners",
  section: "infrastructure",
  kind: "extension",
  category: "devops",
  order: 0,
  defaultEnabled: true,
  description: "Self-hosted GitHub Actions runners, their containers, and every recent workflow run.",
  requires: { docker: true, optional: { clis: ["gh"] } },
  keywords: "ci github actions workflow runs runners",
  component: lazy(() => import("@/pages/Runners")),
  useCommands: useRunnerCommands,
  overviewTile: RunnersTile,
  useAttention: useRunnerAttention,
}
