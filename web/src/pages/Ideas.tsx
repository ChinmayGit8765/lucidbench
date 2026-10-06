import { useMemo, useState } from "react"
import { ChevronDown, GitPullRequest, Lightbulb, PenLine, Search, SquareTerminal, Vote, WandSparkles, X } from "lucide-react"

import { PageHeader, RefreshButton } from "@/components/Shell"
import { StatusPill } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card } from "@/components/ui/card"
import { EmptyState, ErrorState, Skeleton } from "@/components/ui/states"
import { usePoll } from "@/lib/api"
import { useApp } from "@/lib/app"
import { newBraindump } from "@/lib/council"
import { IDEAS_PATH, IDEAS_POLL_MS, STAGE_INFO, STAGES, stageIndex, type IdeaSummary, type Stage } from "@/lib/ideas"
import { absoluteTime, relativeTime, useNow } from "@/lib/time"
import { cn, plural } from "@/lib/utils"
import { formatCost, PR_INFO } from "@/lib/work"
import type { ModulePageProps } from "@/modules/types"
import { IdeaView, StageIcon } from "@/pages/ideas/IdeaView"

/** /ideas lists every idea; /ideas/<id> follows one (a card's id is card:<board>/<card>). */
export default function Ideas({ subpath }: ModulePageProps) {
  const id = subpath.join("/")
  return id ? <IdeaView key={id} id={id} /> : <IdeaList />
}

/** Six dots, filled up to the idea's stage. */
export function StageDots({ stage, className }: { stage: Stage; className?: string }) {
  const at = stageIndex(stage)
  return (
    <span className={cn("inline-flex items-center gap-1", className)} aria-label={`Stage: ${STAGE_INFO[stage].label}`}>
      {STAGES.map((s, i) => (
        <span
          key={s}
          title={STAGE_INFO[s].label}
          className={cn("h-1.5 w-3.5 rounded-full", i < at ? "bg-success/70" : i === at ? (stage === "merged" ? "bg-success" : "bg-brand") : "bg-border")}
        />
      ))}
    </span>
  )
}

function IdeaList() {
  const { open } = useApp()
  const now = useNow(30000)
  const poll = usePoll<IdeaSummary[]>(IDEAS_PATH, IDEAS_POLL_MS)
  const all = poll.data ?? []
  const [stage, setStage] = useState<Stage | "">("")
  const [project, setProject] = useState("")
  const [q, setQ] = useState("")
  const projects = useMemo(() => [...new Set(all.map((i) => i.project).filter(Boolean) as string[])].sort(), [all])
  const counts = useMemo(() => Object.fromEntries(STAGES.map((s) => [s, all.filter((i) => i.stage === s).length])) as Record<Stage, number>, [all])
  const shown = all.filter(
    (i) =>
      (!stage || i.stage === stage) &&
      (!project || i.project === project) &&
      (!q.trim() || `${i.title} ${i.status} ${i.project ?? ""} ${i.id}`.toLowerCase().includes(q.trim().toLowerCase())),
  )
  const spent = all.reduce((n, i) => n + i.cost_usd, 0)
  const filtered = !!(stage || project || q.trim())

  return (
    <div className="space-y-6">
      <PageHeader
        icon={<Lightbulb />}
        title="Ideas"
        description="Every idea from braindump to merged PR: what the council decided, the card it became, the agent sessions and the pull requests."
        actions={
          <>
            <RefreshButton refreshing={poll.refreshing} updatedAt={poll.updatedAt} />
            <Button size="sm" onClick={() => newBraindump(open)}>
              <PenLine /> New braindump
            </Button>
          </>
        }
      />

      {poll.error && !poll.data && <ErrorState title="Could not load the ideas" message={poll.error.message} onRetry={poll.refresh} />}

      {poll.loading && !poll.data ? (
        <div className="space-y-3">
          <Skeleton className="h-20 rounded-xl" />
          <Skeleton className="h-64 rounded-xl" />
        </div>
      ) : all.length === 0 ? (
        <Card>
          <EmptyState
            icon={<Lightbulb />}
            satellites={[
              <span key="v" className="flex size-6 items-center justify-center rounded-md border bg-elevated text-subtle-foreground">
                <Vote className="size-3.5" />
              </span>,
              <span key="w" className="flex size-6 items-center justify-center rounded-md border bg-elevated text-subtle-foreground">
                <SquareTerminal className="size-3.5" />
              </span>,
              <span key="p" className="flex size-6 items-center justify-center rounded-md border bg-elevated text-subtle-foreground">
                <GitPullRequest className="size-3.5" />
              </span>,
            ]}
            title="No ideas yet"
            description="An idea starts as a braindump the council turns into a brief, or as a card you add to a board. Follow it here all the way to the merged PR."
          >
            <Button onClick={() => newBraindump(open)}>
              <PenLine /> Write a braindump
            </Button>
            <Button variant="secondary" onClick={() => open("studio", ["braindump"])}>
              <WandSparkles /> Shape it in Prompt Studio
            </Button>
          </EmptyState>
        </Card>
      ) : (
        <>
          <Card className="grid grid-cols-3 divide-x overflow-hidden @3xl:grid-cols-6">
            {STAGES.map((s) => {
              const on = stage === s
              return (
                <button
                  key={s}
                  onClick={() => setStage(on ? "" : s)}
                  aria-pressed={on}
                  title={STAGE_INFO[s].blurb}
                  className={cn("min-w-0 px-4 py-3 text-left outline-none transition-colors focus-visible:bg-accent/50", on ? "bg-brand-soft" : "hover:bg-accent/40")}
                >
                  <div className="flex items-center gap-1.5 text-2xs font-medium uppercase tracking-[0.08em] text-subtle-foreground">
                    <StageIcon stage={s} className={cn("size-3", on && "text-brand")} />
                    {STAGE_INFO[s].label}
                  </div>
                  <div className={cn("mt-1 text-xl font-semibold tabular-nums tracking-tight", counts[s] === 0 && "text-subtle-foreground")}>{counts[s]}</div>
                </button>
              )
            })}
          </Card>

          <div className="flex flex-wrap items-center gap-2">
            <label className="flex h-8 min-w-56 flex-1 items-center gap-2 rounded-md border bg-background/50 px-2.5 focus-within:border-ring @3xl:max-w-sm">
              <Search className="size-3.5 text-subtle-foreground" />
              <input value={q} onChange={(e) => setQ(e.target.value)} placeholder="Search ideas" aria-label="Search ideas" className="min-w-0 flex-1 bg-transparent text-sm outline-none placeholder:text-subtle-foreground" />
            </label>
            <label className="relative flex items-center">
              <span className="sr-only">Project</span>
              <select
                value={project}
                onChange={(e) => setProject(e.target.value)}
                className={cn("h-8 appearance-none rounded-md border bg-background/60 pl-2.5 pr-7 text-xs outline-none hover:border-border-strong focus-visible:border-ring", !project && "text-muted-foreground")}
              >
                <option value="">All projects</option>
                {projects.map((p) => (
                  <option key={p} value={p}>
                    {p}
                  </option>
                ))}
              </select>
              <ChevronDown className="pointer-events-none absolute right-2 size-3.5 text-subtle-foreground" />
            </label>
            <div role="group" aria-label="Stage" className="flex flex-wrap gap-1">
              {(["", ...STAGES] as (Stage | "")[]).map((s) => (
                <button
                  key={s || "all"}
                  onClick={() => setStage(s)}
                  aria-pressed={stage === s}
                  className={cn(
                    "h-7 rounded-full border px-2.5 text-xs transition-colors",
                    stage === s ? "border-brand/50 bg-brand-soft font-medium text-brand-fg" : "text-muted-foreground hover:bg-accent/50",
                  )}
                >
                  {s ? STAGE_INFO[s].label : "All"}
                </button>
              ))}
            </div>
            {filtered && (
              <button onClick={() => (setStage(""), setProject(""), setQ(""))} className="inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground">
                <X className="size-3" /> Clear
              </button>
            )}
            <span className="ml-auto text-xs tabular-nums text-subtle-foreground">
              {filtered ? `${shown.length} of ${plural(all.length, "idea")}` : plural(all.length, "idea")} · {formatCost(spent)} spent
            </span>
          </div>

          <Card className="overflow-hidden">
            {shown.length === 0 ? (
              <p className="px-5 py-10 text-center text-sm text-muted-foreground">No idea matches these filters.</p>
            ) : (
              <div className="overflow-x-auto">
                <table className="w-full table-fixed text-sm">
                  <colgroup>
                    <col />
                    <col className="w-40" />
                    <col className="w-24" />
                    <col className="w-32" />
                    <col className="w-20" />
                    <col className="w-24" />
                  </colgroup>
                  <thead>
                    <tr className="border-b bg-muted/40 text-left text-2xs font-medium uppercase tracking-[0.06em] text-subtle-foreground">
                      <th className="px-5 py-2 font-medium">Idea</th>
                      <th className="px-3 py-2 font-medium">Stage</th>
                      <th className="px-3 py-2 font-medium">Sessions</th>
                      <th className="px-3 py-2 font-medium">Pull request</th>
                      <th className="px-3 py-2 text-right font-medium">Cost</th>
                      <th className="px-3 py-2 pr-5 text-right font-medium">Updated</th>
                    </tr>
                  </thead>
                  <tbody>
                    {shown.map((i) => (
                      <tr key={i.id} onClick={() => open("ideas", [i.id])} className="cursor-pointer border-b transition-colors last:border-b-0 hover:bg-accent/40">
                        <td className="px-5 py-2.5">
                          <button
                            onClick={(e) => {
                              e.stopPropagation()
                              open("ideas", [i.id])
                            }}
                            className="block max-w-full text-left outline-none focus-visible:underline"
                          >
                            <span className="block truncate font-medium">{i.title || "Untitled idea"}</span>
                            <span className="block truncate text-xs text-muted-foreground">
                              {i.status}
                              {i.project ? ` · ${i.project}` : ""}
                              {!i.council && " · card made by hand"}
                            </span>
                          </button>
                        </td>
                        <td className="px-3 py-2.5">
                          <span className="flex flex-col gap-1">
                            <StatusPill tone={STAGE_INFO[i.stage].tone} className="self-start">
                              {STAGE_INFO[i.stage].label}
                            </StatusPill>
                            <StageDots stage={i.stage} />
                          </span>
                        </td>
                        <td className="px-3 py-2.5 text-xs tabular-nums text-muted-foreground">{i.sessions || "-"}</td>
                        <td className="px-3 py-2.5">
                          {i.pr_url ? (
                            <a
                              href={i.pr_url}
                              target="_blank"
                              rel="noreferrer"
                              onClick={(e) => e.stopPropagation()}
                              className="inline-flex items-center gap-1.5 text-xs hover:underline"
                              title={i.pr_url}
                            >
                              <GitPullRequest className={cn("size-3.5", i.pr_state === "merged" ? "text-success" : "text-subtle-foreground")} />
                              #{i.pr_url.split("/").pop()}
                              <span className="text-subtle-foreground">{i.pr_state ? PR_INFO[i.pr_state].label.replace(/ PR$|^PR /, "") : ""}</span>
                            </a>
                          ) : (
                            <span className="text-xs text-subtle-foreground">-</span>
                          )}
                        </td>
                        <td className="px-3 py-2.5 text-right font-mono text-xs tabular-nums">{formatCost(i.cost_usd)}</td>
                        <td className="px-3 py-2.5 pr-5 text-right text-xs tabular-nums text-muted-foreground" title={i.updated ? absoluteTime(i.updated) : "No time recorded"}>
                          {i.updated ? relativeTime(i.updated, now) : "-"}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </Card>
        </>
      )}
    </div>
  )
}
