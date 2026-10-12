import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState, type CSSProperties, type ReactNode } from "react"
import {
  AppWindow,
  ArrowDown,
  ArrowLeft,
  ChevronRight,
  CircleDollarSign,
  Copy,
  ExternalLink,
  FileDiff,
  FileText,
  FolderGit2,
  GitBranch,
  GitCommitHorizontal,
  GitPullRequest,
  Hourglass,
  KanbanSquare,
  Lightbulb,
  ListTree,
  Play,
  RefreshCw,
  ScrollText,
  SendHorizontal,
  ShieldCheck,
  Square,
  SquareCheck,
  Terminal,
  Timer,
  Trash2,
  TriangleAlert,
  UserCog,
} from "lucide-react"
import { toast } from "sonner"

import { celebrate } from "@/components/Celebrate"
import { copyText } from "@/components/CopyCommand"
import { StateSprite } from "@/components/StateSprite"
import { ProviderMark, ProviderTile, providerInfo, tintVar } from "@/components/ProviderMark"
import { Badge, StatusPill } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card } from "@/components/ui/card"
import { ConfirmDialog, type ConfirmRequest } from "@/components/ui/confirm"
import { EmptyState, ErrorState, Skeleton } from "@/components/ui/states"
import { Tabs } from "@/components/ui/tabs"
import { ApiError, errorMessage, getJSON, request } from "@/lib/api"
import { useApp } from "@/lib/app"
import { DEFAULT_BOARD } from "@/lib/boards"
import { cardIdeaId } from "@/lib/ideas"
import { useNow } from "@/lib/time"
import { cn, plural } from "@/lib/utils"
import {
  buildSteps,
  checksLabel,
  elapsedOf,
  endSession,
  followUp,
  formatCost,
  formatElapsed,
  openPR,
  PR_INFO,
  refreshPR,
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

function Meta({ icon: Icon, children, title, onClick, expanded }: { icon: typeof Timer; children: ReactNode; title?: string; onClick?: () => void; expanded?: boolean }) {
  const cls = "inline-flex min-w-0 items-center gap-1.5 text-xs text-muted-foreground [&_svg]:size-3.5 [&_svg]:shrink-0 [&_svg]:text-subtle-foreground"
  return onClick ? (
    <button onClick={onClick} title={title} aria-expanded={expanded} className={cn(cls, "rounded px-0.5 hover:text-foreground")}>
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
  const [safety, setSafety] = useState(false)
  const running = session?.status === "running"
  const now = useNow(running ? 1000 : 30000)
  const steps = useMemo(() => buildSteps(events, session?.worktree ?? ""), [events, session?.worktree])
  const end = useRef<HTMLDivElement>(null)
  const head = useRef<HTMLDivElement>(null)
  const [headHeight, setHeadHeight] = useState(0)
  // Following the bottom: true until the user scrolls up; events that arrive
  // while they read further up are counted for the "new events" pill.
  const [atBottom, setAtBottom] = useState(true)
  const [seen, setSeen] = useState(0)

  // A PR that is not merged yet: ask again now and then, so its state and
  // checks follow GitHub. The server asks gh at most once a minute.
  const prWatched = !!session?.pr_url && session.pr_state !== "merged" && !session.removed
  useEffect(() => {
    if (!prWatched) return
    const t = setInterval(() => {
      getJSON<WorkSession>(sessionPath(id))
        .then(setSession)
        .catch(() => undefined)
    }, 30000)
    return () => clearInterval(t)
  }, [prWatched, id, setSession])

  // The sticky header's height, so the turn separators stick just below it.
  const loaded = session !== null
  useEffect(() => {
    const el = head.current
    if (!el) return
    const ro = new ResizeObserver(() => setHeadHeight(el.offsetHeight))
    ro.observe(el)
    return () => ro.disconnect()
  }, [loaded])

  useEffect(() => {
    const main = end.current?.closest("main")
    if (!main) return
    const onScroll = () => setAtBottom(main.scrollTop + main.clientHeight >= main.scrollHeight - 160)
    main.addEventListener("scroll", onScroll, { passive: true })
    return () => main.removeEventListener("scroll", onScroll)
  }, [loaded])
  // At the bottom every event is seen; scrolled up, the pill counts the rest.
  useEffect(() => {
    if (atBottom) setSeen(events.length)
  }, [atBottom, events.length])
  useEffect(() => {
    // Following is instant: a smooth scroll still under way when the next
    // event lands would read as the user scrolling up and stop the follow.
    if (running && tab === "timeline" && atBottom) end.current?.scrollIntoView({ block: "end" })
  }, [events.length, running, tab, atBottom])
  const toBottom = () => end.current?.scrollIntoView({ block: "end", behavior: "smooth" })
  const unseen = tab === "timeline" && !atBottom ? Math.max(0, events.length - seen) : 0

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
  const stopNow = async () => {
    try {
      setSession(await stopSession(session.id))
    } catch (e) {
      toast.error("Could not stop the session", { description: errorMessage(e) })
    }
  }
  const stop = () =>
    setConfirm({
      title: `Stop ${label}?`,
      description:
        "Lucidbench stops the agent's process and this turn ends. Whatever it already changed stays in the worktree, and the agent waits for your next message; use the refresh on Changes to see anything written after the stop.",
      confirmLabel: "Stop agent",
      danger: true,
      run: stopNow,
    })
  const commands = session.allowed_commands?.length ?? 0

  return (
    <div className="space-y-4" style={{ "--session-head": `${headHeight}px` } as CSSProperties}>
      <div ref={head} className="sticky top-0 z-20 -mx-5 -mt-3 border-b bg-background/85 px-5 pb-0 pt-2.5 backdrop-blur-md md:-mx-8 md:px-8" data-testid="session-header">
        <div className="flex items-start gap-3">
          <ProviderTile provider={session.provider} size="md" className="mt-0.5" />
          <div className="min-w-0 flex-1">
            <div className="flex min-w-0 items-baseline gap-2">
              <button onClick={() => open("work")} className="inline-flex shrink-0 items-center gap-1 text-xs font-medium text-subtle-foreground hover:text-foreground" title="All sessions">
                <ArrowLeft className="size-3" /> Work · {session.project}
              </button>
            </div>
            <h1 className="truncate text-lg font-semibold leading-snug tracking-[-0.015em]" title={session.title}>
              {session.title}
            </h1>
            <div className="mt-0.5 flex min-w-0 flex-wrap items-center gap-x-3 gap-y-0.5">
              <Meta icon={GitBranch} title={`${session.branch} (click to copy)`} onClick={() => void copyText(session.branch, "Branch copied")}>
                <span className={cn("max-w-[16rem] truncate font-mono", session.removed && "line-through")}>{session.branch}</span>
              </Meta>
              <Meta
                icon={FolderGit2}
                title={session.removed ? `Worktree removed: ${session.worktree_hint}` : `${session.worktree_hint} (click to copy)`}
                onClick={() => void copyText(session.worktree, "Path copied")}
              >
                <span className={cn("max-w-[11rem] truncate font-mono", session.removed && "line-through")}>{session.worktree_hint.split(/[\\/]/).pop()}</span>
              </Meta>
              {session.pr_url && session.pr_state && (
                <Meta icon={GitPullRequest} title={checksLabel(session.pr_checks) || PR_INFO[session.pr_state].label}>
                  <span className={cn(session.pr_state === "merged" && "text-success-fg", session.pr_state === "closed" && "text-danger-fg")}>{PR_INFO[session.pr_state].label}</span>
                  {session.pr_checks && session.pr_state !== "merged" && <span className="font-mono">· {checksLabel(session.pr_checks)}</span>}
                </Meta>
              )}
              {session.card && (
                <Meta icon={KanbanSquare} title="Open the card" onClick={() => open("boards", [session.board || DEFAULT_BOARD, session.card!])}>
                  card
                </Meta>
              )}
              {session.brief && (
                <Meta icon={FileText} title={`Open the brief: ${session.brief}`} onClick={() => open("memory", session.brief!.split("/"))}>
                  brief
                </Meta>
              )}
              {session.card && (
                <Meta icon={Lightbulb} title="The whole idea: council, brief, card, sessions and PRs" onClick={() => open("ideas", [cardIdeaId(session.board || DEFAULT_BOARD, session.card!)])}>
                  idea
                </Meta>
              )}
              {session.browser && (
                <Meta icon={AppWindow} title="The agent browser was attached to this session (open Live browser to watch it)">
                  browser
                </Meta>
              )}
              <Meta
                icon={session.harness === "mine" ? UserCog : ShieldCheck}
                title="How the agent runs: its harness, what it may run without asking, and the sandbox note"
                onClick={() => setSafety((s) => !s)}
                expanded={safety}
              >
                <span>
                  {session.harness === "mine" ? "your harness" : "clean harness"}
                  {session.profile ? ` · ${session.profile}` : ""}
                  {commands > 0 ? ` · ${plural(commands, "command")} allowed` : ""}
                </span>
                <ChevronRight className={cn("transition-transform", safety && "rotate-90")} />
              </Meta>
            </div>
            {safety && (
              <div className="mt-1.5 max-w-3xl space-y-1.5 pb-1 text-2xs text-subtle-foreground" data-testid="session-safety">
                <p className="leading-4">{SANDBOX_NOTICE}</p>
                <p>{session.harness === "mine" ? "The CLI loaded your own settings, hooks and skills." : "No user settings, hooks or MCP servers."}</p>
                {commands > 0 && (
                  <div>
                    <span className="inline-flex items-center gap-1.5">
                      <Terminal className="size-3" /> May run without asking:
                    </span>
                    <ul aria-label="Allowed commands" className="mt-1 flex flex-wrap gap-1">
                      {session.allowed_commands!.map((c) => (
                        <li key={c} className="rounded-full border bg-background/60 px-2 py-0.5 font-mono text-muted-foreground">
                          {c}
                        </li>
                      ))}
                    </ul>
                  </div>
                )}
              </div>
            )}
          </div>
          <div className="flex shrink-0 flex-col items-end gap-1">
            <div className="flex items-center gap-2">
              {running && <StateSprite state="working" className="-my-3 size-9" label="The agent is working" />}
              <StatusPill tone={st.tone} pulse={running}>
                {st.label}
              </StatusPill>
              {running && (
                <Button variant="secondary" size="sm" onClick={stop} className="border-danger/40 text-danger-fg hover:bg-danger-soft">
                  <Square className="fill-current" /> Stop
                </Button>
              )}
            </div>
            <div className="flex items-center gap-2.5">
              {(session.turns?.length ?? 0) > 1 && (
                <span className="font-mono text-2xs tabular-nums text-subtle-foreground" title="Turns in this session">
                  {session.turns!.length} turns
                </span>
              )}
              <Meta icon={Timer} title="Time the agent worked">
                <span className="font-mono tabular-nums">{formatElapsed(elapsedOf(session, now))}</span>
              </Meta>
              <Meta
                icon={CircleDollarSign}
                title={
                  session.usage
                    ? `All turns: ${session.usage.input_tokens ?? 0} in · ${session.usage.output_tokens ?? 0} out · ${session.usage.model ?? ""}${session.usage.note ? ` · ${session.usage.note}` : ""}`
                    : "Cost is known when the turn ends"
                }
              >
                <span className="font-mono tabular-nums">{running ? (session.usage?.cost_usd ? `${formatCost(session.usage.cost_usd)} + …` : "…") : formatCost(session.usage?.cost_usd)}</span>
              </Meta>
            </div>
          </div>
        </div>
        <Tabs
          label="Session view"
          value={tab}
          onChange={setTab}
          className="mt-1.5 border-b-0"
          items={[
            { id: "timeline", label: "Timeline", icon: ListTree, count: steps.filter((s) => s.type === "tools").length || undefined },
            ...(session.diff ? [{ id: "changes", label: "Changes", icon: FileDiff, count: session.diff.files.length }] : []),
            { id: "raw", label: "Raw log", icon: ScrollText },
          ]}
        />
      </div>

      {tab === "timeline" && (
        <>
          <Timeline session={session} steps={steps} now={now} onOpenChanges={session.diff ? () => setTab("changes") : undefined} />
          {session.diff && <Changes session={session} onChange={setSession} setConfirm={setConfirm} />}
        </>
      )}
      {tab === "changes" && session.diff && <Changes session={session} onChange={setSession} setConfirm={setConfirm} expanded />}
      {tab === "raw" && <RawLog id={session.id} version={events.length} />}
      <div ref={end} />

      <Composer session={session} onChange={setSession} onStop={stop} onStopNow={stopNow} setConfirm={setConfirm} unseen={unseen} onJump={toBottom} />
      <EndedBar session={session} onChange={setSession} setConfirm={setConfirm} />

      <ConfirmDialog request={confirm} onClose={() => setConfirm(null)} />
    </div>
  )
}

/* ---------- composer ---------- */

/** A dialog is open: keys belong to it, not to the page behind. */
const dialogOpen = () => document.querySelector("[role=dialog], [role=alertdialog]") !== null

/**
 * The bottom of a session, like a coding agent's prompt box: write the next
 * message while the agent waits (Enter sends, Shift+Enter is a new line, Esc
 * stops a running turn), or end the session or open its PR.
 */
function Composer({
  session,
  onChange,
  onStop,
  onStopNow,
  setConfirm,
  unseen,
  onJump,
}: {
  session: WorkSession
  onChange: (s: WorkSession) => void
  onStop: () => void
  onStopNow: () => Promise<void>
  setConfirm: (c: ConfirmRequest | null) => void
  unseen: number
  onJump: () => void
}) {
  const [text, setText] = useState("")
  const [sending, setSending] = useState(false)
  const area = useRef<HTMLTextAreaElement>(null)
  const running = session.status === "running"
  const waiting = session.status === "waiting" && !session.removed
  const turns = session.turns?.length ?? 1
  const label = providerInfo(session.provider)?.label ?? session.provider
  const model = session.model || session.usage?.model
  const canSend = waiting && text.trim() !== "" && !sending

  // When a turn ends, put the cursor back in the box.
  useEffect(() => {
    if (waiting) area.current?.focus({ preventScroll: true })
  }, [waiting])
  // The box grows with what is written, up to a few lines.
  useLayoutEffect(() => {
    const el = area.current
    if (!el) return
    el.style.height = "auto"
    el.style.height = `${Math.min(el.scrollHeight, 168)}px`
  }, [text])
  // Esc stops a running turn, as in a coding agent's terminal, unless a
  // dialog is open or another field has the keyboard.
  useEffect(() => {
    if (!running) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== "Escape" || e.defaultPrevented || dialogOpen()) return
      const t = e.target as HTMLElement | null
      if (t && t !== area.current && t.closest("input, textarea, select, [contenteditable=true]")) return
      e.preventDefault()
      toast(`Stopping ${label}…`)
      void onStopNow()
    }
    window.addEventListener("keydown", onKey)
    return () => window.removeEventListener("keydown", onKey)
  }, [running, label, onStopNow])

  if (!running && !waiting) return null

  const send = async () => {
    if (!canSend) return
    setSending(true)
    try {
      onChange(await followUp(session.id, text.trim()))
      setText("")
    } catch (e) {
      toast.error("Could not send the follow-up", { description: errorMessage(e) })
    } finally {
      setSending(false)
    }
  }
  const end = () =>
    setConfirm({
      title: "End this session?",
      description: "The agent stops waiting for follow-ups. The worktree, the branch and any PR stay; removing the worktree is separate.",
      confirmLabel: "End session",
      run: async () => {
        try {
          onChange(await endSession(session.id))
        } catch (e) {
          toast.error("Could not end the session", { description: errorMessage(e) })
        }
      },
    })
  const d = session.diff
  const prReady = !session.pr_url && !!d && d.commits.length > 0
  const pr = () =>
    setConfirm({
      title: "Push and open a draft PR?",
      description: `Pushes ${session.branch} to origin and runs gh pr create --draft against ${session.base_ref.replace(/^origin\//, "")}. Nothing is merged, and the session keeps waiting for you.`,
      confirmLabel: "Push and open PR",
      run: async () => {
        try {
          const s = await openPR(session.id)
          onChange(s)
          toast.success("Draft PR opened", { description: s.pr_url })
        } catch (e) {
          toast.error("Could not open the PR", { description: errorMessage(e) })
        }
      },
    })

  return (
    <div className="sticky bottom-0 z-20 -mx-5 border-t bg-background/90 px-5 pb-2.5 pt-2 backdrop-blur-md md:-mx-8 md:px-8" data-testid="composer">
      {unseen > 0 && (
        <button
          onClick={onJump}
          className="absolute -top-11 left-1/2 inline-flex -translate-x-1/2 items-center gap-1.5 rounded-full border bg-elevated px-3 py-1.5 text-xs font-medium shadow-md transition-colors animate-in fade-in-0 slide-in-from-bottom-1 hover:bg-accent"
          data-testid="new-events"
        >
          <ArrowDown className="size-3.5" /> {plural(unseen, "new event")}
        </button>
      )}
      <Card className={cn("flex items-end gap-2 overflow-hidden py-1.5 pl-3 pr-1.5 transition-colors focus-within:border-border-strong", waiting && "border-warning/40")}>
        <label htmlFor="follow-up" className="sr-only">
          Follow-up for the agent
        </label>
        <textarea
          id="follow-up"
          ref={area}
          value={text}
          onChange={(e) => setText(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) {
              e.preventDefault()
              void send()
            }
          }}
          rows={1}
          maxLength={32000}
          disabled={sending}
          placeholder={running ? `${label} is working. Write your next message; it sends once this turn ends, or press Esc to stop.` : `Reply to ${label}…`}
          data-testid="composer-input"
          className="block max-h-[168px] min-h-9 flex-1 resize-none bg-transparent py-1.5 text-[0.9375rem] leading-6 outline-none placeholder:text-subtle-foreground/70"
        />
        {running ? (
          <Button variant="secondary" size="sm" onClick={onStop} className="mb-0.5 border-danger/40 text-danger-fg hover:bg-danger-soft" title="Stop this turn (Esc)" data-testid="composer-stop">
            <Square className="fill-current" /> Stop
          </Button>
        ) : (
          <Button size="sm" className="mb-0.5" onClick={() => void send()} disabled={!canSend} aria-label="Send follow-up" title="Send (Enter)" data-testid="send">
            <SendHorizontal /> Send
          </Button>
        )}
      </Card>
      <div className="mt-1.5 flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1 text-2xs">
        {waiting && (
          <span className="inline-flex items-center gap-1.5 font-medium text-warning-fg" role="status" data-testid="waiting-banner">
            <Hourglass className="size-3" />
            {label} is waiting for you
          </span>
        )}
        <span className="inline-flex items-center gap-1.5 rounded-full border bg-background/60 px-2 py-px text-muted-foreground" title={model ? `${label} · ${model}` : label} data-testid="composer-chip">
          <span style={{ color: tintVar(session.provider) }}>
            <ProviderMark provider={session.provider} className="size-3" />
          </span>
          {label}
          {model && <span className="max-w-28 truncate font-mono">· {model}</span>}
        </span>
        <span className="rounded-full border bg-background/60 px-2 py-px font-mono tabular-nums text-muted-foreground" data-testid="turn-indicator">
          turn {turns}
        </span>
        <span className="text-subtle-foreground max-[1200px]:hidden">
          <Kbd>Enter</Kbd> send · <Kbd>Shift</Kbd>+<Kbd>Enter</Kbd> new line{running && <> · <Kbd>Esc</Kbd> stop</>}
        </span>
        <span className="flex-1" />
        {waiting && (
          <>
            <Button variant="ghost" size="sm" className="h-6 px-2" onClick={end} data-testid="end-session">
              <SquareCheck /> End session
            </Button>
            {session.pr_url ? (
              <Button asChild variant="ghost" size="sm" className="h-6 px-2">
                <a href={session.pr_url} target="_blank" rel="noreferrer">
                  <GitPullRequest className="text-success" /> View PR
                </a>
              </Button>
            ) : (
              <Button variant="ghost" size="sm" className="h-6 px-2" onClick={pr} disabled={!prReady} title={prReady ? "Push the branch and open a draft PR" : "No commits yet: nothing to open a PR with"} data-testid="composer-open-pr">
                <GitPullRequest /> Open PR
              </Button>
            )}
          </>
        )}
      </div>
    </div>
  )
}

function Kbd({ children }: { children: ReactNode }) {
  return <kbd className="rounded border bg-muted/60 px-1 font-sans text-[0.625rem] text-muted-foreground">{children}</kbd>
}

/**
 * The bottom of a session whose PR was merged or closed: nothing waits for a
 * follow-up any more, so the next step is removing the worktree, in one click.
 */
function EndedBar({
  session,
  onChange,
  setConfirm,
}: {
  session: WorkSession
  onChange: (s: WorkSession) => void
  setConfirm: (c: ConfirmRequest | null) => void
}) {
  const [busy, setBusy] = useState(false)
  if ((session.status !== "merged" && session.status !== "closed") || session.removed) return null
  const merged = session.status === "merged"
  const discard = (reason: string) =>
    setConfirm({
      title: "Discard the leftover changes?",
      description: `${reason.trim().replace(/[.;:]$/, "")}. Removing the worktree deletes the folder and anything in it that is not committed; the ${session.branch} branch stays in the repository.`,
      confirmLabel: "Discard and remove",
      danger: true,
      run: async () => {
        try {
          onChange(await removeWorktree(session.id, true))
          toast.success("Worktree removed")
        } catch (e) {
          toast.error("Could not remove the worktree", { description: errorMessage(e) })
        }
      },
    })
  const remove = async () => {
    setBusy(true)
    try {
      onChange(await removeWorktree(session.id, false))
      toast.success("Worktree removed", { description: `${session.branch} stays in the repository.` })
    } catch (e) {
      if (e instanceof ApiError && e.status === 409) discard(e.message)
      else toast.error("Could not remove the worktree", { description: errorMessage(e) })
    } finally {
      setBusy(false)
    }
  }
  return (
    <div className="sticky bottom-0 z-20 -mx-5 border-t bg-background/90 px-5 py-2.5 backdrop-blur-md md:-mx-8 md:px-8" data-testid="ended-bar">
      <div
        className={cn(
          "flex flex-wrap items-center gap-x-3 gap-y-2 rounded-xl border px-4 py-2.5",
          merged ? "border-success/40 bg-success-soft/60" : "border-border bg-muted/40",
        )}
        role="status"
      >
        <GitPullRequest className={cn("size-4 shrink-0", merged ? "text-success" : "text-subtle-foreground")} />
        <div className="min-w-0 flex-1 text-sm">
          <span className="font-medium">{merged ? "Merged — remove the worktree?" : "PR closed — remove the worktree?"}</span>{" "}
          <span className="text-muted-foreground max-[720px]:hidden">
            {merged ? "The work is on the base branch; the session has ended." : "The PR was closed without merging; reopen it on GitHub to carry on."}
          </span>
        </div>
        <Button size="sm" variant={merged ? "default" : "secondary"} onClick={() => void remove()} disabled={busy} data-testid="remove-worktree">
          <Trash2 /> Remove worktree
        </Button>
      </div>
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
  const { open } = useApp()
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
  // Why Open PR is off, in words, shown next to the button.
  const prBlocked = session.removed
    ? "The worktree was removed"
    : session.status === "running"
      ? "Wait for the turn to finish"
      : noCommits
        ? d.uncommitted.length > 0
          ? `No commits yet: commit the ${plural(d.uncommitted.length, "uncommitted change")} first`
          : "No commits yet: nothing to open a PR with"
        : null
  const [checking, setChecking] = useState(false)
  const checkPR = async () => {
    setChecking(true)
    try {
      const s = await refreshPR(session.id)
      onChange(s)
      if (s.pr_state === "merged") celebrate({ key: `pr:${s.id}`, title: "PR merged", detail: s.title || s.branch })
      else toast(s.pr_state ? `The PR is ${PR_INFO[s.pr_state].label.toLowerCase()}` : "GitHub did not answer", { description: checksLabel(s.pr_checks) || undefined })
    } catch (e) {
      toast.error("Could not read the PR", { description: errorMessage(e) })
    } finally {
      setChecking(false)
    }
  }
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
            <>
              {session.pr_state !== "merged" && (
                <Button variant="ghost" size="sm" onClick={() => void checkPR()} disabled={checking} title="Ask GitHub for the PR's state now" data-testid="check-pr">
                  <RefreshCw className={cn(checking && "animate-spin")} /> Check PR
                </Button>
              )}
              <Button asChild variant="secondary" size="sm">
                <a href={session.pr_url} target="_blank" rel="noreferrer">
                  <GitPullRequest className="text-success" /> {session.pr_state === "merged" ? "View merged PR" : "View draft PR"} <ExternalLink />
                </a>
              </Button>
            </>
          ) : (
            <>
              {prBlocked && (
                <span className="text-xs text-muted-foreground" data-testid="pr-blocked">
                  {prBlocked}
                </span>
              )}
              <Button size="sm" onClick={pr} disabled={!!prBlocked} title={prBlocked ?? "Push the branch and open a draft PR"} data-testid="open-pr">
                <GitPullRequest /> Open PR
              </Button>
            </>
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
        <div
          className={cn(
            "mx-5 mb-3 flex items-center gap-2 rounded-lg border px-3 py-2 text-xs",
            session.pr_state === "closed" ? "border-danger/30 bg-danger-soft text-danger-fg" : "border-success/30 bg-success-soft text-success-fg",
          )}
        >
          <GitPullRequest className="size-3.5" />
          {session.pr_state === "merged"
            ? "PR merged. The card is in Done."
            : session.pr_state === "closed"
              ? "PR closed without merging."
              : session.pr_state === "open"
                ? "PR open. Merging is your call on GitHub."
                : "Draft PR opened. Merging is your call on GitHub."}
          {session.pr_checks && session.pr_state !== "merged" && (
            <span className="font-mono opacity-90" title="Status checks on the PR">
              · {checksLabel(session.pr_checks)}
            </span>
          )}
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
        <div className="flex flex-wrap items-center gap-3 border-t px-5 py-4">
          <p className="min-w-0 flex-1 text-sm text-muted-foreground">
            {d.uncommitted.length > 0
              ? "Nothing committed yet."
              : session.status === "running"
                ? "No changes yet."
                : session.status === "waiting"
                  ? "No changes yet. If the agent asked you something, answer it below."
                  : "The agent changed nothing. If it asked you something, start a new session with your answers in the prompt."}
          </p>
          {d.uncommitted.length === 0 && session.status !== "running" && session.status !== "waiting" && (
            <Button size="sm" variant="secondary" onClick={() => open("work", session.card ? ["new", session.card] : ["new", "project", session.project])}>
              <Play /> Answer and start again
            </Button>
          )}
        </div>
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
