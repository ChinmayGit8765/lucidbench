import { useMemo, useState } from "react"
import { ExternalLink, Waypoints } from "lucide-react"

import { SetupCard } from "@/components/SetupCard"
import { PageHeader, RefreshButton } from "@/components/Shell"
import { Card } from "@/components/ui/card"
import { EmptyState, ErrorState, Skeleton } from "@/components/ui/states"
import { usePoll } from "@/lib/api"
import {
  columnsFor,
  issuesPath,
  LINEAR_POLL_MS,
  PRIORITY_TONE,
  type LinearCycle,
  type LinearFilter,
  type LinearIssue,
  type LinearIssues,
  type LinearMeta,
  type LinearStatus,
  type StateColumn,
} from "@/lib/linear"
import { cn, plural } from "@/lib/utils"

const select =
  "h-8 rounded-md border bg-background/60 px-2.5 text-sm outline-none transition-colors hover:border-border-strong focus-visible:border-border-strong focus-visible:ring-2 focus-visible:ring-ring/30"

const STATE_TONE: Record<string, string> = {
  triage: "var(--warning)",
  backlog: "var(--neutral)",
  unstarted: "var(--neutral)",
  started: "var(--info)",
  completed: "var(--success)",
  canceled: "var(--danger)",
}

export default function Linear() {
  const status = usePoll<LinearStatus>("/api/linear/status", 30000)
  return (
    <div className="space-y-6">
      <PageHeader
        icon={<Waypoints />}
        title="Linear"
        description="Your Linear issues as a board, grouped by state. Linear owns them: every card opens the issue there, and nothing here is synced back."
        actions={<RefreshButton refreshing={status.refreshing} updatedAt={status.updatedAt} />}
      />
      {status.error && !status.data ? (
        <ErrorState title="Could not check the Linear connector" message={status.error.message} onRetry={status.refresh} />
      ) : !status.data ? (
        <Skeleton className="h-64 rounded-xl" />
      ) : !status.data.configured ? (
        <Setup status={status.data} />
      ) : (
        <Board />
      )}
    </div>
  )
}

function Setup({ status }: { status: LinearStatus }) {
  const name = status.token_ref.replace(/^env:/, "")
  return (
    <SetupCard
      title="Connect Linear with an API key"
      intro="Lucidbench reads Linear through its API with a personal API key. The key stays in your environment; the config only names the variable."
      note={
        status.mcp ? (
          <>
            A Linear MCP server is configured in one of your AI clients, but that gives <em>those clients</em> access, not Lucidbench. The in-app connector needs an API key of its own.
          </>
        ) : undefined
      }
      steps={[
        <>
          In Linear, open <strong>Settings → Account → Security &amp; access</strong> and create a personal API key (the <strong>API</strong> section).
        </>,
        <>
          Set it as the environment variable <code>{name}</code> where the daemon starts (for the desktop app, a user environment variable), then restart Lucidbench.
        </>,
        <>
          To use a different variable, point <code>integrations.linear.token</code> at it, as <code>env:NAME</code>, in your config.
        </>,
      ]}
      vars={[{ name, set: status.configured }]}
    />
  )
}

function Board() {
  const [f, setF] = useState<LinearFilter>({ team: "", project: "", me: true, done: false })
  const meta = usePoll<LinearMeta>("/api/linear/meta", LINEAR_POLL_MS)
  const issues = usePoll<LinearIssues>(issuesPath(f), LINEAR_POLL_MS)
  const teams = useMemo(() => (meta.data?.teams ?? []).filter((t) => !f.team || t.id === f.team), [meta.data, f.team])
  const projects = useMemo(() => (meta.data?.projects ?? []).filter((p) => !f.team || p.team_ids.includes(f.team)), [meta.data, f.team])
  const cols = useMemo(() => columnsFor(issues.data?.issues ?? [], teams, f.done), [issues.data, teams, f.done])
  const cycles = teams.filter((t) => t.active_cycle)
  const total = issues.data?.issues.length ?? 0
  const err = issues.error ?? meta.error

  return (
    <>
      <div className="flex flex-wrap items-center gap-2">
        <select
          aria-label="Team"
          value={f.team}
          onChange={(e) => setF({ ...f, team: e.target.value, project: "" })}
          className={select}
        >
          <option value="">All teams</option>
          {(meta.data?.teams ?? []).map((t) => (
            <option key={t.id} value={t.id}>
              {t.name}
            </option>
          ))}
        </select>
        <select aria-label="Project" value={f.project} onChange={(e) => setF({ ...f, project: e.target.value })} className={select}>
          <option value="">All projects</option>
          {projects.map((p) => (
            <option key={p.id} value={p.id}>
              {p.name}
            </option>
          ))}
        </select>
        <Toggle on={f.me} onChange={(me) => setF({ ...f, me })}>
          Assigned to me
        </Toggle>
        <Toggle on={f.done} onChange={(done) => setF({ ...f, done })}>
          Show done
        </Toggle>
        <span className="ml-auto text-xs text-subtle-foreground">
          {issues.data ? plural(total, "issue") : ""}
          {meta.data && ` · ${meta.data.viewer.name}`}
        </span>
      </div>

      {cycles.length > 0 && (
        <div className="flex flex-wrap gap-2">
          {cycles.map((t) => (
            <CycleChip key={t.id} team={t.key} cycle={t.active_cycle!} />
          ))}
        </div>
      )}

      {issues.data?.truncated && (
        <p className="text-xs text-warning-fg">Showing the most recent {plural(total, "issue")}. Narrow the filters to see the rest.</p>
      )}

      {err && !issues.data ? (
        <ErrorState title="Could not read Linear" message={err.message} onRetry={() => (issues.refresh(), meta.refresh())} />
      ) : !issues.data ? (
        <div className="flex gap-3">
          {[0, 1, 2, 3].map((i) => (
            <Skeleton key={i} className="h-80 w-72 rounded-xl" />
          ))}
        </div>
      ) : total === 0 && cols.length === 0 ? (
        <Card className="border-dashed">
          <EmptyState icon={<Waypoints />} title="No issues match" description="Try clearing a filter, or show done issues." />
        </Card>
      ) : (
        <div className="-mx-5 overflow-x-auto px-5 pb-4 md:-mx-8 md:px-8">
          <div className="flex min-h-[26rem] items-start gap-3">
            {cols.map((c) => (
              <Column key={c.key} col={c} />
            ))}
          </div>
        </div>
      )}
    </>
  )
}

function Toggle({ on, onChange, children }: { on: boolean; onChange: (on: boolean) => void; children: string }) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={on}
      onClick={() => onChange(!on)}
      className={cn(
        "inline-flex h-8 items-center gap-2 rounded-md border px-2.5 text-sm transition-colors",
        on ? "border-brand/40 bg-brand-soft text-brand-fg" : "bg-background/60 text-muted-foreground hover:border-border-strong hover:text-foreground",
      )}
    >
      <span className={cn("size-1.5 rounded-full", on ? "bg-brand" : "bg-border-strong")} />
      {children}
    </button>
  )
}

const day = (iso: string) => new Date(iso).toLocaleDateString(undefined, { month: "short", day: "numeric" })

function CycleChip({ team, cycle }: { team: string; cycle: LinearCycle }) {
  const pct = Math.round(cycle.progress * 100)
  return (
    <div className="flex items-center gap-3 rounded-lg border bg-card px-3 py-2 shadow-card">
      <span className="rounded border bg-background/60 px-1.5 py-px font-mono text-2xs text-muted-foreground">{team}</span>
      <div className="min-w-0">
        <div className="text-sm font-medium">{cycle.name || `Cycle ${cycle.number}`}</div>
        <div className="text-2xs text-subtle-foreground">
          {day(cycle.starts_at)} to {day(cycle.ends_at)}
        </div>
      </div>
      <div className="w-24">
        <div className="h-1.5 overflow-hidden rounded-full bg-muted">
          <div className="h-full rounded-full bg-brand" style={{ width: `${pct}%` }} />
        </div>
        <div className="mt-1 text-right text-2xs tabular-nums text-subtle-foreground">{pct}% done</div>
      </div>
    </div>
  )
}

function Column({ col }: { col: StateColumn }) {
  const tone = col.color || STATE_TONE[col.type] || "var(--neutral)"
  return (
    <section aria-label={`${col.name} column`} className="flex max-h-[calc(100vh-17rem)] w-72 shrink-0 flex-col rounded-xl border bg-muted/40">
      <header className="flex items-center gap-2 px-3 pb-2 pt-3">
        <span className="size-2 rounded-full" style={{ background: tone }} />
        <h2 className="truncate text-sm font-semibold">{col.name}</h2>
        <span className="rounded-full bg-background/80 px-1.5 text-2xs tabular-nums text-muted-foreground">{col.issues.length}</span>
      </header>
      <div className="flex min-h-16 flex-1 flex-col gap-2 overflow-y-auto px-2 pb-2">
        {col.issues.map((i) => (
          <IssueCard key={i.id} issue={i} />
        ))}
        {col.issues.length === 0 && <div className="px-2 py-3 text-xs text-subtle-foreground">Nothing here.</div>}
      </div>
    </section>
  )
}

function IssueCard({ issue }: { issue: LinearIssue }) {
  return (
    <a
      href={issue.url}
      target="_blank"
      rel="noreferrer"
      aria-label={`${issue.identifier}: ${issue.title}, open in Linear`}
      className="group block rounded-lg border bg-card p-3 shadow-card transition-[border-color,box-shadow] hover:border-border-strong focus-visible:ring-2 focus-visible:ring-ring/40"
    >
      <div className="flex items-center gap-2">
        <span className="font-mono text-2xs text-subtle-foreground">{issue.identifier}</span>
        {issue.priority > 0 && issue.priority <= 4 && (
          <span title={issue.priority_label} className="size-1.5 rounded-full" style={{ background: PRIORITY_TONE[issue.priority] }} />
        )}
        <ExternalLink className="ml-auto size-3 text-subtle-foreground opacity-0 transition-opacity group-hover:opacity-100" />
      </div>
      <div className="mt-1 text-sm leading-snug">{issue.title}</div>
      <div className="mt-2.5 flex flex-wrap items-center gap-1.5">
        {issue.labels.slice(0, 3).map((l) => (
          <span
            key={l.name}
            className="rounded px-1.5 py-px text-2xs font-medium"
            style={l.color ? { background: `color-mix(in oklch, ${l.color} 16%, transparent)`, color: l.color } : undefined}
          >
            {l.name}
          </span>
        ))}
        {issue.project_name && <span className="truncate rounded border bg-background/60 px-1.5 py-px text-2xs text-muted-foreground">{issue.project_name}</span>}
        {issue.assignee && (
          <span title={issue.assignee.name} className="ml-auto flex size-5 items-center justify-center rounded-full border bg-background/60 text-2xs font-medium text-muted-foreground">
            {issue.assignee.name.slice(0, 1).toUpperCase()}
          </span>
        )}
      </div>
    </a>
  )
}
