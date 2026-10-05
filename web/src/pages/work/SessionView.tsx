import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from "react"
import {
  ArrowLeft,
  ChevronRight,
  CircleDollarSign,
  Copy,
  ExternalLink,
  FileDiff,
  FolderGit2,
  GitBranch,
  GitCommitHorizontal,
  GitPullRequest,
  ListTree,
  RefreshCw,
  ScrollText,
  ShieldCheck,
  Square,
  Timer,
  Trash2,
  TriangleAlert,
  UserCog,
} from "lucide-react"
import { toast } from "sonner"

import { copyText } from "@/components/CopyCommand"
import { ProviderTile, providerInfo } from "@/components/ProviderMark"
import { Badge, StatusPill } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card } from "@/components/ui/card"
import { ConfirmDialog, type ConfirmRequest } from "@/components/ui/confirm"
import { EmptyState, ErrorState, Skeleton } from "@/components/ui/states"
import { Tabs } from "@/components/ui/tabs"
import { ApiError, errorMessage, getJSON, request } from "@/lib/api"
import { useApp } from "@/lib/app"
import { useNow } from "@/lib/time"
import { cn } from "@/lib/utils"
import {
  buildSteps,
  elapsedOf,
  formatCost,
  formatElapsed,
  openPR,
  removeWorktree,
  SANDBOX_NOTICE,
  sessionPath,
  STATUS_INFO,
  stopSession,
  type WorkEvent,
  type WorkSession,
} from "@/lib/work"
import { DiffView, Timeline } from "@/pages/work/Timeline"

/** Follows one session: the record plus its events over server-sent events. */
function useSession(id: string) {
  const [session, setSession] = useState<WorkSession | null>(null)
  const [events, setEvents] = useState<WorkEvent[]>([])
  const [error, setError] = useState<string | null>(null)
  const [missing, setMissing] = useState(false)

  useEffect(() => {
    let alive = true
    setSession(null)
    setEvents([])
    setError(null)
    setMissing(false)
    getJSON<WorkSession>(sessionPath(id))
      .then((s) => alive && setSession(s))
      .catch((e) => {
        if (!alive) return
        if (e instanceof ApiError && e.status === 404) setMissing(true)
        else setError(errorMessage(e))
      })
    const es = new EventSource(`${sessionPath(id)}/events`)
    es.onmessage = (m) => alive && setEvents((evs) => [...evs, JSON.parse(m.data as string) as WorkEvent])
    es.addEventListener("session", (m) => alive && setSession(JSON.parse((m as MessageEvent).data as string) as WorkSession))
    es.addEventListener("end", () => es.close())
    return () => {
      alive = false
      es.close()
    }
  }, [id])
  return { session, setSession, events, error, missing }
}

function Meta({ icon: Icon, children, title, onClick }: { icon: typeof Timer; children: ReactNode; title?: string; onClick?: () => void }) {
  const cls = "inline-flex min-w-0 items-center gap-1.5 text-xs text-muted-foreground [&_svg]:size-3.5 [&_svg]:shrink-0 [&_svg]:text-subtle-foreground"
  return onClick ? (
    <button onClick={onClick} title={title} className={cn(cls, "rounded px-0.5 hover:text-foreground")}>
      <Icon />
      {children}
    </button>
  ) : (
    <span title={title} className={cls}>
      <Icon />
      {children}
    </span>
  )
}

export function SessionView({ id }: { id: string }) {
  const { open } = useApp()
  const { session, setSession, events, error, missing } = useSession(id)
  const [tab, setTab] = useState("timeline")
  const [confirm, setConfirm] = useState<ConfirmRequest | null>(null)
  const running = session?.status === "running"
  const now = useNow(running ? 1000 : 30000)
  const steps = useMemo(() => buildSteps(events, session?.worktree ?? ""), [events, session?.worktree])
  const end = useRef<HTMLDivElement>(null)
  const pinned = useRef(true)

  // While the agent runs, stay at the bottom unless the user scrolled up.
  useEffect(() => {
    const main = end.current?.closest("main")
    if (!main) return
    const onScroll = () => {
      pinned.current = main.scrollTop + main.clientHeight >= main.scrollHeight - 160
    }
    main.addEventListener("scroll", onScroll, { passive: true })
    return () => main.removeEventListener("scroll", onScroll)
  }, [session !== null])
  useEffect(() => {
    if (running && tab === "timeline" && pinned.current) end.current?.scrollIntoView({ block: "end" })
  }, [events.length, running, tab])

  if (missing)
    return (
      <Card>
        <EmptyState icon={<ListTree />} title="No such session" description="It may have been made by another Lucidbench data folder.">
          <Button variant="secondary" onClick={() => open("work")}>
            <ArrowLeft /> All sessions
          </Button>
        </EmptyState>
      </Card>
    )
  if (error) return <ErrorState title="Could not load the session" message={error} />
  if (!session)
    return (
      <div className="space-y-4">
        <Skeleton className="h-24 rounded-xl" />
        <Skeleton className="h-10 w-2/3" />
        <Skeleton className="h-10 w-1/2" />
        <Skeleton className="h-40 rounded-xl" />
      </div>
    )

  const st = STATUS_INFO[session.status]
  const label = providerInfo(session.provider)?.label ?? session.provider
  const stop = () =>
    setConfirm({
      title: `Stop ${label}?`,
      description: "Lucidbench stops the agent's process. Whatever it already changed stays in the worktree; use the refresh on Changes to see anything written after the stop.",
      confirmLabel: "Stop agent",
      danger: true,
      run: async () => {
        try {
          setSession(await stopSession(session.id))
        } catch (e) {
          toast.error("Could not stop the session", { description: errorMessage(e) })
        }
      },
    })

  return (
    <div className="space-y-5">
      <div className="sticky top-0 z-20 -mx-5 -mt-3 border-b bg-background/85 px-5 pb-0 pt-3 backdrop-blur-md md:-mx-8 md:px-8">
        <div className="flex items-start gap-3">
          <ProviderTile provider={session.provider} size="lg" />
          <div className="min-w-0 flex-1">
            <button onClick={() => open("work")} className="mb-0.5 inline-flex items-center gap-1 text-xs font-medium text-subtle-foreground hover:text-foreground">
              <ArrowLeft className="size-3" /> Work · {session.project}
            </button>
            <h1 className="truncate text-xl font-semibold tracking-[-0.02em]" title={session.title}>
              {session.title}
            </h1>
            <div className="mt-1.5 flex flex-wrap items-center gap-x-3.5 gap-y-1">
              <Meta icon={GitBranch} title="Copy branch name" onClick={() => void copyText(session.branch, "Branch copied")}>
                <span className={cn("truncate font-mono", session.removed && "line-through")}>{session.branch}</span>
              </Meta>
              <Meta
                icon={FolderGit2}
                title={session.removed ? `Worktree removed: ${session.worktree_hint}` : `${session.worktree_hint} (click to copy)`}
                onClick={() => void copyText(session.worktree, "Path copied")}
              >
                <span className={cn("max-w-[22rem] truncate font-mono", session.removed && "line-through")}>{session.worktree_hint}</span>
              </Meta>
              <Meta icon={session.harness === "mine" ? UserCog : ShieldCheck} title={session.harness === "mine" ? "The CLI loaded your own settings, hooks and skills" : "No user settings, hooks or MCP servers"}>
                {session.harness === "mine" ? "your harness" : "clean harness"}
                {session.profile ? ` · ${session.profile}` : ""}
              </Meta>
            </div>
            <p className="mt-1 max-w-3xl text-2xs leading-4 text-subtle-foreground">{SANDBOX_NOTICE}</p>
          </div>
          <div className="flex shrink-0 flex-col items-end gap-2">
            <div className="flex items-center gap-2">
              <StatusPill tone={st.tone} pulse={running}>
                {st.label}
              </StatusPill>
              {running && (
                <Button variant="secondary" size="sm" onClick={stop} className="border-danger/40 text-danger-fg hover:bg-danger-soft">
                  <Square className="fill-current" /> Stop
                </Button>
              )}
            </div>
            <div className="flex items-center gap-3">
              <Meta icon={Timer} title="Elapsed">
                <span className="font-mono tabular-nums">{formatElapsed(elapsedOf(session, now))}</span>
              </Meta>
              <Meta icon={CircleDollarSign} title={session.usage ? `${session.usage.input_tokens ?? 0} in · ${session.usage.output_tokens ?? 0} out · ${session.usage.model ?? ""}${session.usage.note ? ` · ${session.usage.note}` : ""}` : "Cost is known when the run ends"}>
                <span className="font-mono tabular-nums">{running ? "…" : formatCost(session.usage?.cost_usd)}</span>
              </Meta>
            </div>
          </div>
        </div>
        <Tabs
          label="Session view"
          value={tab}
          onChange={setTab}
          className="mt-3 border-b-0"
          items={[
            { id: "timeline", label: "Timeline", icon: ListTree, count: steps.filter((s) => s.type === "tools").length || undefined },
            ...(session.diff ? [{ id: "changes", label: "Changes", icon: FileDiff, count: session.diff.files.length }] : []),
            { id: "raw", label: "Raw log", icon: ScrollText },
          ]}
        />
      </div>

      {tab === "timeline" && (
        <>
          <Timeline session={session} steps={steps} now={now} />
          {session.diff && <Changes session={session} onChange={setSession} setConfirm={setConfirm} />}
        </>
      )}
      {tab === "changes" && session.diff && <Changes session={session} onChange={setSession} setConfirm={setConfirm} expanded />}
      {tab === "raw" && <RawLog id={session.id} version={events.length} />}
      <div ref={end} />

      <ConfirmDialog request={confirm} onClose={() => setConfirm(null)} />
    </div>
  )
}

/* ---------- changes ---------- */

/** A server message as a sentence: capitalised, with a full stop. */
const sentence = (s: string) => {
  const t = s.trim().replace(/[.;:]$/, "")
  return t.charAt(0).toUpperCase() + t.slice(1) + "."
}

function Changes({
  session,
  onChange,
  setConfirm,
  expanded = false,
}: {
  session: WorkSession
  onChange: (s: WorkSession) => void
  setConfirm: (c: ConfirmRequest | null) => void
  expanded?: boolean
}) {
  const d = session.diff!
  const [openFiles, setOpenFiles] = useState<Set<string>>(() => new Set(expanded || d.files.length === 1 ? d.files.map((f) => f.path) : []))
  const toggle = (p: string) =>
    setOpenFiles((s) => {
      const n = new Set(s)
      if (n.has(p)) n.delete(p)
      else n.add(p)
      return n
    })
  const noCommits = d.commits.length === 0
  const base = session.base_ref.replace(/^origin\//, "")
  const [refreshing, setRefreshing] = useState(false)
  const refresh = async () => {
    setRefreshing(true)
    try {
      onChange(await getJSON<WorkSession>(`${sessionPath(session.id)}?refresh=1`))
    } catch (e) {
      toast.error("Could not read the worktree", { description: errorMessage(e) })
    } finally {
      setRefreshing(false)
    }
  }

  const pr = () =>
    setConfirm({
      title: "Push and open a draft PR?",
      description: `Pushes ${session.branch} to origin and runs gh pr create --draft against ${base}. Nothing is merged: you review and merge on GitHub.`,
      confirmLabel: "Push and open PR",
      run: async () => {
        try {
          const s = await openPR(session.id)
          onChange(s)
          toast.success("Draft PR opened", { description: s.pr_url })
        } catch (e) {
          toast.error("Could not open the PR", { description: errorMessage(e) })
          onChange(await getJSON<WorkSession>(sessionPath(session.id)).catch(() => session))
        }
      },
    })

  const remove = (discard: boolean, reason?: string) =>
    setConfirm({
      title: discard ? "Discard the unpushed work?" : "Remove the worktree?",
      description: discard
        ? `${sentence(reason ?? "the worktree has work that is not on the remote")} Removing it deletes the folder and any uncommitted changes in it; the ${session.branch} branch and its commits stay in the repository.`
        : `Deletes ${session.worktree_hint}. The ${session.branch} branch stays in the repository.`,
      confirmLabel: discard ? "Discard and remove" : "Remove worktree",
      danger: true,
      run: async () => {
        try {
          onChange(await removeWorktree(session.id, discard))
          toast.success("Worktree removed")
        } catch (e) {
          if (!discard && e instanceof ApiError && e.status === 409) {
            // Ask again, now naming what would be lost.
            setTimeout(() => remove(true, e.message), 0)
            return
          }
          toast.error("Could not remove the worktree", { description: errorMessage(e) })
        }
      },
    })

  return (
    <Card className="overflow-hidden">
      <div className="flex flex-wrap items-center gap-3 px-5 pb-3 pt-4">
        <span className="flex size-7 items-center justify-center rounded-md border bg-background/60 text-subtle-foreground">
          <FileDiff className="size-3.5" />
        </span>
        <div className="min-w-0 flex-1">
          <h2 className="text-sm font-semibold">Changes</h2>
          <p className="text-xs text-muted-foreground">
            <span className="font-mono text-success-fg">+{d.added}</span> <span className="font-mono text-danger-fg">−{d.deleted}</span> in {d.files.length}{" "}
            {d.files.length === 1 ? "file" : "files"} · {d.commits.length} {d.commits.length === 1 ? "commit" : "commits"} on top of{" "}
            <span className="font-mono">{base}</span>
          </p>
        </div>
        <div className="flex items-center gap-2">
          {session.pr_url ? (
            <Button asChild variant="secondary" size="sm">
              <a href={session.pr_url} target="_blank" rel="noreferrer">
                <GitPullRequest className="text-success" /> View draft PR <ExternalLink />
              </a>
            </Button>
          ) : (
            <Button size="sm" onClick={pr} disabled={noCommits || session.removed || session.status === "running"} title={noCommits ? "The branch has no commits yet" : undefined}>
              <GitPullRequest /> Open PR
            </Button>
          )}
          {!session.removed && (
            <Button
              variant="ghost"
              size="icon-sm"
              title="Read the worktree again, for changes made after the run"
              aria-label="Refresh changes"
              disabled={session.status === "running" || refreshing}
              onClick={() => void refresh()}
            >
              <RefreshCw className={cn(refreshing && "animate-spin")} />
            </Button>
          )}
          {session.removed ? (
            <Badge>worktree removed</Badge>
          ) : (
            <Button variant="ghost" size="sm" onClick={() => remove(false)} disabled={session.status === "running"} className="hover:text-danger-fg">
              <Trash2 /> Remove worktree
            </Button>
          )}
        </div>
      </div>

      {session.pr_url && (
        <div className="mx-5 mb-3 flex items-center gap-2 rounded-lg border border-success/30 bg-success-soft px-3 py-2 text-xs text-success-fg">
          <GitPullRequest className="size-3.5" />
          Draft PR opened. Merging is your call on GitHub.
          <a href={session.pr_url} target="_blank" rel="noreferrer" className="ml-auto truncate font-mono underline-offset-2 hover:underline">
            {session.pr_url.replace(/^https?:\/\/(www\.)?github\.com\//, "")}
          </a>
        </div>
      )}

      {d.uncommitted.length > 0 && !session.removed && (
        <div className="mx-5 mb-3 rounded-lg border border-warning/40 bg-warning-soft px-3 py-2 text-xs text-warning-fg">
          <div className="flex items-center gap-1.5 font-medium">
            <TriangleAlert className="size-3.5" />
            {d.uncommitted.length} uncommitted {d.uncommitted.length === 1 ? "change" : "changes"} in the worktree, not part of a PR
          </div>
          <ul className="mt-1 space-y-0.5 font-mono opacity-90">
            {d.uncommitted.slice(0, 8).map((l) => (
              <li key={l} className="truncate">
                {l}
              </li>
            ))}
          </ul>
        </div>
      )}

      {d.commits.length > 0 && (
        <ul className="border-t px-5 py-2">
          {d.commits.map((c) => (
            <li key={c.sha} className="flex items-center gap-2 py-1 text-sm">
              <GitCommitHorizontal className="size-3.5 shrink-0 text-subtle-foreground" />
              <button onClick={() => void copyText(c.sha, "Commit copied")} className="shrink-0 font-mono text-xs text-subtle-foreground hover:text-foreground" title="Copy the full hash">
                {c.sha.slice(0, 7)}
              </button>
              <span className="truncate">{c.subject}</span>
            </li>
          ))}
        </ul>
      )}

      {d.files.length > 0 ? (
        <ul className="divide-y border-t">
          {d.files.map((f) => {
            const on = openFiles.has(f.path)
            const total = Math.max(1, f.added + f.deleted)
            return (
              <li key={f.path}>
                <button onClick={() => toggle(f.path)} aria-expanded={on} className="flex w-full items-center gap-2.5 px-5 py-2 text-left transition-colors hover:bg-accent/30">
                  <ChevronRight className={cn("size-3.5 shrink-0 text-subtle-foreground transition-transform", on && "rotate-90")} />
                  <span className="min-w-0 flex-1 truncate font-mono text-xs">{f.path}</span>
                  {f.binary ? (
                    <Badge>binary</Badge>
                  ) : (
                    <>
                      <span className="font-mono text-2xs tabular-nums">
                        <span className="text-success-fg">+{f.added}</span> <span className="text-danger-fg">−{f.deleted}</span>
                      </span>
                      <span className="flex h-1.5 w-16 overflow-hidden rounded-full bg-muted" aria-hidden>
                        <span className="h-full bg-success" style={{ width: `${(f.added / total) * 100}%` }} />
                        <span className="h-full bg-danger" style={{ width: `${(f.deleted / total) * 100}%` }} />
                      </span>
                    </>
                  )}
                </button>
                {on && (
                  <div className="px-5 pb-3">
                    {f.patch ? <DiffView text={f.patch} /> : <p className="text-xs text-muted-foreground">No text diff to show.</p>}
                    {f.cut && <p className="mt-1 text-2xs text-subtle-foreground">Cut short: open the worktree to see the whole file.</p>}
                  </div>
                )}
              </li>
            )
          })}
        </ul>
      ) : (
        <p className="border-t px-5 py-4 text-sm text-muted-foreground">
          {d.uncommitted.length > 0 ? "Nothing committed yet." : "The agent changed nothing."}
        </p>
      )}
    </Card>
  )
}

/* ---------- raw log ---------- */

function RawLog({ id, version }: { id: string; version: number }) {
  const [text, setText] = useState<string | null>(null)
  const [err, setErr] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const load = useCallback(async () => {
    setBusy(true)
    try {
      setText(await (await request(`${sessionPath(id)}/raw`)).text())
      setErr(null)
    } catch (e) {
      setErr(errorMessage(e))
    } finally {
      setBusy(false)
    }
  }, [id])
  // Reload as events arrive, at most once a second.
  useEffect(() => {
    const t = setTimeout(() => void load(), text === null ? 0 : 1000)
    return () => clearTimeout(t)
  }, [load, version]) // text only picks the delay, so it is left out

  const lines = (text ?? "").replace(/\n$/, "").split("\n").filter((l, i, a) => l !== "" || i < a.length - 1)
  return (
    <Card className="overflow-hidden">
      <div className="flex items-center gap-2 px-4 py-2.5">
        <p className="min-w-0 flex-1 text-xs text-muted-foreground">
          The CLI's own JSON for each step, as Lucidbench received it. Lines the CLI printed outside a step (start-up, the final result) are not kept.
        </p>
        <Button variant="ghost" size="sm" onClick={() => void load()} disabled={busy}>
          <RefreshCw className={cn(busy && "animate-spin")} /> Reload
        </Button>
        <Button variant="ghost" size="sm" disabled={!text} onClick={() => void copyText(text ?? "", "Log copied")}>
          <Copy /> Copy
        </Button>
      </div>
      {err ? (
        <ErrorState className="m-4" title="Could not load the raw log" message={err} onRetry={() => void load()} />
      ) : text === null ? (
        <div className="space-y-2 p-4">
          <Skeleton className="h-3.5 w-3/4" />
          <Skeleton className="h-3.5 w-1/2" />
        </div>
      ) : lines.length === 0 || (lines.length === 1 && lines[0] === "") ? (
        <p className="border-t p-6 text-center text-sm text-muted-foreground">Nothing yet.</p>
      ) : (
        <pre className="max-h-[70vh] overflow-auto border-t bg-background py-2 font-mono text-xs leading-5">
          {lines.map((l, i) => (
            <div key={i} className="flex hover:bg-accent/40">
              <span className="w-12 shrink-0 select-none pr-3 text-right tabular-nums text-subtle-foreground/70">{i + 1}</span>
              <span className="whitespace-pre-wrap break-all pr-4">{l || " "}</span>
            </div>
          ))}
        </pre>
      )}
    </Card>
  )
}
