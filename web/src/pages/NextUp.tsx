import { useEffect, useMemo, useState } from "react"
import {
  ArrowRight,
  BellOff,
  Bot,
  ChevronDown,
  CircleAlert,
  Compass,
  ExternalLink,
  EyeOff,
  GitPullRequest,
  KanbanSquare,
  Lock,
  Pin,
  Play,
  RotateCcw,
  Sparkles,
  SquareTerminal,
  Vote,
} from "lucide-react"
import { toast } from "sonner"

import { PageHeader, RefreshButton } from "@/components/Shell"
import { Badge, StatusPill, type Tone } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card } from "@/components/ui/card"
import { ConfirmDialog, type ConfirmRequest } from "@/components/ui/confirm"
import { Menu } from "@/components/ui/menu"
import { EmptyState, ErrorState, Skeleton } from "@/components/ui/states"
import { errorMessage, usePoll } from "@/lib/api"
import { useApp } from "@/lib/app"
import { startCouncil } from "@/lib/council"
import {
  dismiss,
  FACTOR_LABEL,
  KIND_INFO,
  kindIcon,
  NEXTUP_PATH,
  NEXTUP_POLL_MS,
  rank,
  REASONS,
  restore,
  saveSettings,
  SCHEDULES,
  snooze,
  useScheduledRank,
  type NextItem,
  type NextView,
  type Part,
  type Settings,
} from "@/lib/nextup"
import type { ProjectList } from "@/lib/projects"
import { relativeTime, useNow } from "@/lib/time"
import { cn, plural } from "@/lib/utils"
import { defaultHarness, formatCost, startSession, type WorkProvider } from "@/lib/work"
import type { ModulePageProps } from "@/modules/types"

const FACTOR_TONE: Record<Part["factor"], Tone> = {
  urgency: "warning",
  unblocking: "info",
  staleness: "neutral",
  focus: "success",
  effort: "neutral",
  blocked: "neutral",
  headroom: "neutral",
  set_aside: "neutral",
}

/** One chip per score term: the factor and its points; the reason in the title. */
function PartChips({ parts, max }: { parts: Part[]; max?: number }) {
  const shown = max ? parts.slice(0, max) : parts
  return (
    <span className="flex flex-wrap gap-1">
      {shown.map((p, i) => (
        <StatusPill key={i} tone={FACTOR_TONE[p.factor]} className="cursor-default">
          <span title={p.why}>
            {FACTOR_LABEL[p.factor]} {p.points > 0 ? `+${p.points}` : p.points}
          </span>
        </StatusPill>
      ))}
      {max && parts.length > max && <span className="text-2xs text-subtle-foreground">+{parts.length - max}</span>}
    </span>
  )
}

/** The why, in words: every term with its reason. */
function Breakdown({ item }: { item: NextItem }) {
  return (
    <ul className="space-y-1 text-xs" data-testid="nextup-breakdown">
      {item.parts.map((p, i) => (
        <li key={i} className="flex items-baseline gap-2">
          <span className={cn("w-9 shrink-0 text-right font-mono tabular-nums", p.points < 0 ? "text-warning-fg" : "text-foreground")}>
            {p.points > 0 ? `+${p.points}` : p.points}
          </span>
          <span className="text-muted-foreground">
            <span className="font-medium text-foreground">{FACTOR_LABEL[p.factor]}</span> · {p.why}
          </span>
        </li>
      ))}
      {item.parts.length === 0 && <li className="text-muted-foreground">No signal yet: no due date, nothing failing, nothing waiting on it.</li>}
    </ul>
  )
}

function SourceIcon({ item, className }: { item: NextItem; className?: string }) {
  const Icon = kindIcon(item.kind)
  return (
    <span title={KIND_INFO[item.kind]?.label} className={cn("flex size-7 shrink-0 items-center justify-center rounded-lg border bg-elevated text-subtle-foreground", className)}>
      <Icon className="size-3.5" />
    </span>
  )
}

/** Opens the item where it lives, in the app or on the web. */
function SourceLink({ item }: { item: NextItem }) {
  const { navigate } = useApp()
  if (item.link.url)
    return (
      <a href={item.link.url} target="_blank" rel="noreferrer" className="inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground hover:underline">
        {item.link.label} <ExternalLink className="size-3" />
      </a>
    )
  if (item.link.route)
    return (
      <button onClick={() => navigate(item.link.route!)} className="inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground hover:underline">
        {item.link.label} <ArrowRight className="size-3" />
      </button>
    )
  return null
}

function ItemMenus({ item, onDone }: { item: NextItem; onDone: () => void }) {
  const act = async (p: Promise<unknown>, msg: string) => {
    try {
      await p
      toast.success(msg, { description: item.title })
      onDone()
    } catch (e) {
      toast.error("Could not change it", { description: errorMessage(e) })
    }
  }
  return (
    <>
      <Menu
        label={`Snooze ${item.title}`}
        trigger={
          <span className="inline-flex" data-testid="nextup-snooze">
            <BellOff />
          </span>
        }
        items={[
          { label: "For a day", onSelect: () => void act(snooze(item.id, 1), "Snoozed for a day") },
          { label: "For a week", onSelect: () => void act(snooze(item.id, 7), "Snoozed for a week") },
        ]}
      />
      <Menu
        label={`Not now: ${item.title}`}
        trigger={
          <span className="inline-flex" data-testid="nextup-dismiss">
            <EyeOff />
          </span>
        }
        items={REASONS.map((r) => ({ label: r.label, onSelect: () => void act(dismiss(item.id, r.id), `Set aside: ${r.label.toLowerCase()}`) }))}
      />
    </>
  )
}

/** The prompt box shown in the confirm dialog; the page keeps its text. */
function PromptBox({ item, value, onChange }: { item: NextItem; value: string; onChange: (v: string) => void }) {
  const w = item.action.work
  if (!w) return null
  return (
    <div className="space-y-2 text-xs">
      <div className="flex flex-wrap gap-1.5">
        <Badge>{w.project}</Badge>
        <Badge>
          {w.provider}
          {w.model ? ` · ${w.model}` : ""}
        </Badge>
        {w.card && (
          <Badge>
            <KanbanSquare /> card {w.card}
          </Badge>
        )}
      </div>
      <label className="block">
        <span className="mb-1 block font-medium text-muted-foreground">{w.card ? "Extra instructions (the card's brief is the task)" : "Task"}</span>
        <textarea
          aria-label="Prompt"
          value={value}
          onChange={(e) => onChange(e.target.value)}
          rows={w.card ? 3 : 7}
          className="w-full resize-y rounded-md border bg-background/60 p-2 font-mono text-xs outline-none focus-visible:border-ring"
        />
      </label>
    </div>
  )
}

export default function NextUp({ subpath }: ModulePageProps) {
  const { navigate } = useApp()
  const now = useNow(30000)
  const poll = usePoll<NextView>(NEXTUP_PATH, NEXTUP_POLL_MS)
  const projects = usePoll<ProjectList>("/api/projects", 60000)
  useScheduledRank(poll.data, poll.refresh)
  const [project, setProject] = useState("")
  const [confirm, setConfirm] = useState<ConfirmRequest | null>(null)
  const [open, setOpen] = useState<string | null>(null)
  const [ranking, setRanking] = useState(false)
  const v = poll.data
  const all = v?.items ?? []
  const shown = project ? all.filter((i) => i.project === project) : all
  const [hero, ...rest] = shown
  const names = useMemo(() => {
    const m = new Map<string, string>()
    for (const i of all) if (i.project) m.set(i.project, i.project_name || i.project)
    return [...m.entries()].sort((a, b) => a[1].localeCompare(b[1]))
  }, [all])

  /** The one primary action: spending ones ask first, the rest just open. */
  const take = (item: NextItem) => {
    const a = item.action
    if (!a.spends) {
      if (a.url) window.open(a.url, "_blank", "noreferrer")
      else if (a.route) navigate(a.route)
      return
    }
    if (a.council) {
      const req = a.council
      setConfirm({
        title: "Run the council again?",
        description: a.explain,
        confirmLabel: "Run Council",
        body: <p className="line-clamp-4 rounded-md border bg-muted/40 p-2 text-xs text-muted-foreground">{req.input}</p>,
        run: async () => {
          try {
            const s = await startCouncil({ input: req.input, project: req.project })
            navigate(`/council/${s.id}`)
          } catch (e) {
            toast.error("Could not start the council", { description: errorMessage(e) })
          }
        },
      })
      return
    }
    if (!a.work) return
    const w = a.work
    let text = w.prompt ?? ""
    setConfirm({
      title: `${a.label}: ${item.title}?`,
      description: a.explain,
      confirmLabel: "Start session",
      body: <PromptField item={item} initial={text} onChange={(t) => (text = t)} />,
      run: async () => {
        try {
          const provider = (w.provider ?? "claude") as WorkProvider
          const s = await startSession({
            card: w.card,
            project: w.project,
            prompt: text.trim() || undefined,
            provider,
            profile: w.profile || undefined,
            harness: defaultHarness(provider),
            ...(w.model ? { model: w.model } : {}),
          } as Parameters<typeof startSession>[0])
          navigate(`/work/${s.id}`)
        } catch (e) {
          toast.error("Could not start the session", { description: errorMessage(e) })
        }
      },
    })
  }

  // /nextup/start/<id> (the Overview tile's Start) opens that item's confirm dialog.
  const startId = subpath[0] === "start" ? subpath.slice(1).join("/") : ""
  useEffect(() => {
    if (!startId || !v) return
    const it = v.items.find((i) => i.id === startId)
    navigate("/nextup")
    if (it) take(it)
  }, [startId, v?.generated_at])

  const askAgent = () => {
    const n = Math.min(all.length, 30)
    const conf = all.slice(0, 30).filter((i) => i.confidential).length
    const s = v?.settings
    setConfirm({
      title: `Ask an agent to rank ${plural(n, "item")}?`,
      description: `${s?.provider ? `${s.provider}${s.model ? ` (${s.model})` : ""}` : "Your team's scout, else the first installed CLI on its cheap model (claude uses haiku),"} reads a short list of ids, titles, scores and reasons, with no tools, and puts them in order. It runs on your own account; a ranking usually costs well under a cent.`,
      confirmLabel: "Rank",
      body:
        conf > 0 ? (
          <p className="flex items-start gap-2 rounded-md border bg-muted/40 p-2 text-xs text-muted-foreground">
            <Lock className="mt-0.5 size-3.5 shrink-0" />
            {plural(conf, "confidential item")} {conf === 1 ? "goes" : "go"} as an id and a score only: no title, project or context leaves this machine.
          </p>
        ) : undefined,
      run: async () => {
        setRanking(true)
        try {
          const r = await rank({})
          toast.success("Ranked by the agent", { description: `${r.usage.provider}${r.usage.model ? ` · ${r.usage.model}` : ""} · ${formatCost(r.usage.cost_usd)}` })
          poll.refresh()
        } catch (e) {
          toast.error("The agent could not rank the list", { description: errorMessage(e) })
        } finally {
          setRanking(false)
        }
      },
    })
  }

  return (
    <div className="space-y-6">
      <PageHeader
        icon={<Compass />}
        title="Next up"
        description="What to work on next, from your boards, briefs, sessions, pull requests and checks. Each item says why, and nothing starts until you confirm."
        actions={
          <>
            <RefreshButton refreshing={poll.refreshing} updatedAt={poll.updatedAt} />
            <Button size="sm" variant="secondary" onClick={askAgent} disabled={all.length === 0 || ranking} data-testid="nextup-rank">
              <Bot /> {ranking ? "Ranking" : "Ask an agent to rank"}
            </Button>
          </>
        }
      />

      {poll.error && !v && <ErrorState title="Could not work out what is next" message={poll.error.message} onRetry={poll.refresh} />}
      {v && v.errors.length > 0 && (
        <p className="flex items-start gap-2 rounded-lg border bg-muted/40 px-3 py-2 text-xs text-muted-foreground">
          <CircleAlert className="mt-px size-3.5 shrink-0 text-warning" />
          <span>
            Some sources could not be read, so the list may be missing items: {v.errors.map((e) => `${e.source} (${e.error})`).join(" · ")}
          </span>
        </p>
      )}

      {poll.loading && !v ? (
        <div className="space-y-3">
          <Skeleton className="h-44 rounded-xl" />
          <Skeleton className="h-64 rounded-xl" />
        </div>
      ) : v && all.length === 0 ? (
        <Card>
          <EmptyState
            icon={<Compass />}
            satellites={[
              <span key="k" className="flex size-6 items-center justify-center rounded-md border bg-elevated text-subtle-foreground">
                <KanbanSquare className="size-3.5" />
              </span>,
              <span key="v" className="flex size-6 items-center justify-center rounded-md border bg-elevated text-subtle-foreground">
                <Vote className="size-3.5" />
              </span>,
              <span key="g" className="flex size-6 items-center justify-center rounded-md border bg-elevated text-subtle-foreground">
                <GitPullRequest className="size-3.5" />
              </span>,
            ]}
            title="Nothing is waiting on you"
            description="Next up fills itself from what Lucidbench already knows: cards in Ready or In progress, briefs to approve, agents waiting for a reply, pull requests and failing checks, and project needs. Add a card or write a braindump to give it something to rank."
          >
            <Button onClick={() => navigate("/boards/work")}>
              <KanbanSquare /> Open the work board
            </Button>
            <Button variant="secondary" onClick={() => navigate("/council")}>
              <Vote /> Write a braindump
            </Button>
          </EmptyState>
        </Card>
      ) : v ? (
        <div className="grid items-start gap-4 @4xl:grid-cols-[minmax(0,1fr)_18rem]">
          <div className="min-w-0 space-y-4">
            {hero ? (
              <Hero item={hero} onTake={() => take(hero)} onDone={poll.refresh} ranked={!!v.ranked} />
            ) : (
              <Card className="px-5 py-8 text-center text-sm text-muted-foreground">No item on this project.</Card>
            )}

            <div className="flex flex-wrap items-center gap-2">
              <label className="relative flex items-center">
                <span className="sr-only">Project</span>
                <select
                  value={project}
                  onChange={(e) => setProject(e.target.value)}
                  aria-label="Filter by project"
                  className={cn("h-8 appearance-none rounded-md border bg-background/60 pl-2.5 pr-7 text-xs outline-none hover:border-border-strong focus-visible:border-ring", !project && "text-muted-foreground")}
                >
                  <option value="">All projects</option>
                  {names.map(([id, name]) => (
                    <option key={id} value={id}>
                      {name}
                    </option>
                  ))}
                </select>
                <ChevronDown className="pointer-events-none absolute right-2 size-3.5 text-subtle-foreground" />
              </label>
              {v.ranked && (
                <span className="inline-flex items-center gap-1.5 text-xs text-muted-foreground" data-testid="nextup-ranked">
                  <Sparkles className="size-3.5 text-brand" /> Ordered by {v.ranked.provider}
                  {v.ranked.model ? ` (${v.ranked.model})` : ""} {relativeTime(v.ranked.at, now)} · {formatCost(v.ranked.cost_usd)}
                </span>
              )}
              <span className="ml-auto text-xs tabular-nums text-subtle-foreground">
                {project ? `${shown.length} of ${plural(all.length, "item")}` : plural(all.length, "item")}
              </span>
            </div>

            {rest.length > 0 && (
              <Card className="overflow-hidden">
                <ol className="divide-y" aria-label="Ranked list">
                  {rest.map((it, i) => (
                    <li key={it.id} data-testid="nextup-item" data-id={it.id} className="px-4 py-3">
                      <div className="flex items-start gap-3">
                        <span className="mt-1 w-5 shrink-0 text-right font-mono text-xs tabular-nums text-subtle-foreground">{i + 2}</span>
                        <SourceIcon item={it} />
                        <div className="min-w-0 flex-1">
                          <div className="flex items-center gap-2">
                            <span className="truncate text-sm font-medium" data-testid="nextup-item-title">
                              {it.title}
                            </span>
                            {it.confidential && <Lock className="size-3 shrink-0 text-subtle-foreground" aria-label="Confidential" />}
                          </div>
                          <div className="mt-0.5 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-muted-foreground">
                            {it.project_name && <span>{it.project_name}</span>}
                            {it.context && <span className="truncate">{it.context}</span>}
                          </div>
                          {it.agent?.reason && <p className="mt-1 text-xs text-foreground/80">“{it.agent.reason}”</p>}
                          <div className="mt-1.5 flex flex-wrap items-center gap-2">
                            <button
                              onClick={() => setOpen(open === it.id ? null : it.id)}
                              aria-expanded={open === it.id}
                              className="inline-flex items-center gap-1 rounded-full border px-1.5 py-0.5 text-2xs font-semibold tabular-nums hover:bg-accent"
                              title="Why this score"
                              data-testid="nextup-score"
                            >
                              {it.score}
                              <ChevronDown className={cn("size-3 transition-transform", open === it.id && "rotate-180")} />
                            </button>
                            <PartChips parts={it.parts} max={3} />
                          </div>
                          {open === it.id && (
                            <div className="mt-2 rounded-md border bg-muted/30 p-2.5">
                              <Breakdown item={it} />
                            </div>
                          )}
                        </div>
                        <div className="flex shrink-0 flex-col items-end gap-1">
                          <Button size="sm" variant={it.action.spends ? "default" : "secondary"} onClick={() => take(it)} data-testid="nextup-item-action">
                            {it.action.spends ? <Play /> : <ArrowRight />} {it.action.label}
                          </Button>
                          <div className="flex items-center">
                            <ItemMenus item={it} onDone={poll.refresh} />
                          </div>
                        </div>
                      </div>
                    </li>
                  ))}
                </ol>
              </Card>
            )}
          </div>

          <SidePanel view={v} projects={projects.data} onChange={poll.refresh} />
        </div>
      ) : null}

      <ConfirmDialog request={confirm} onClose={() => setConfirm(null)} />
    </div>
  )
}

/** The prompt box inside the confirm dialog keeps its own state. */
function PromptField({ item, initial, onChange }: { item: NextItem; initial: string; onChange: (t: string) => void }) {
  const [text, setText] = useState(initial)
  return (
    <PromptBox
      item={item}
      value={text}
      onChange={(t) => {
        setText(t)
        onChange(t)
      }}
    />
  )
}

function Hero({ item, onTake, onDone, ranked }: { item: NextItem; onTake: () => void; onDone: () => void; ranked: boolean }) {
  const a = item.action
  return (
    <Card className="relative overflow-hidden" data-testid="nextup-hero" data-id={item.id}>
      <div aria-hidden className="pointer-events-none absolute inset-0 bg-gradient-to-br from-brand-soft/70 via-transparent to-transparent" />
      <div className="relative space-y-4 p-5">
        <div className="flex items-start gap-3">
          <SourceIcon item={item} className="size-9 text-brand" />
          <div className="min-w-0 flex-1">
            <p className="text-2xs font-medium uppercase tracking-[0.08em] text-subtle-foreground">
              Next up{ranked && item.agent ? " · the agent's pick" : ""} · {KIND_INFO[item.kind]?.label}
            </p>
            <h2 className="mt-0.5 text-lg font-semibold tracking-tight" data-testid="nextup-hero-title">
              {item.title}
            </h2>
            <div className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground">
              {item.project_name && <span>{item.project_name}</span>}
              {item.context && <span>{item.context}</span>}
              {item.confidential && (
                <span className="inline-flex items-center gap-1">
                  <Lock className="size-3" /> confidential: never sent to a provider
                </span>
              )}
              <SourceLink item={item} />
            </div>
          </div>
          <span className="flex flex-col items-end">
            <span className="text-2xl font-semibold tabular-nums tracking-tight">{item.score}</span>
            <span className="text-2xs text-subtle-foreground">score</span>
          </span>
        </div>

        <div className="grid gap-4 @2xl:grid-cols-2">
          <div>
            <h3 className="mb-1.5 text-xs font-semibold">Why it's next</h3>
            {item.agent?.reason && <p className="mb-2 text-sm">“{item.agent.reason}”</p>}
            <Breakdown item={item} />
          </div>
          <div>
            <h3 className="mb-1.5 text-xs font-semibold">If you start it</h3>
            <p className="text-sm text-muted-foreground">{a.explain}</p>
            {a.note && <p className="mt-1.5 text-xs text-warning-fg">{a.note}</p>}
            {a.spends && <p className="mt-1.5 text-xs text-subtle-foreground">You see the prompt and confirm before anything runs.</p>}
          </div>
        </div>

        <div className="flex flex-wrap items-center gap-2 border-t pt-4">
          <Button onClick={onTake} data-testid="nextup-start">
            {a.spends ? <Play /> : a.kind === "resume_work" ? <SquareTerminal /> : <ArrowRight />} {a.label}
          </Button>
          <ItemMenus item={item} onDone={onDone} />
        </div>
      </div>
    </Card>
  )
}

function SidePanel({ view, projects, onChange }: { view: NextView; projects: ProjectList | null; onChange: () => void }) {
  const s = view.settings
  const list = projects?.projects ?? []
  const save = async (next: Settings) => {
    try {
      await saveSettings(next)
      onChange()
    } catch (e) {
      toast.error("Could not save", { description: errorMessage(e) })
    }
  }
  const togglePin = (id: string) => save({ ...s, pinned: s.pinned.includes(id) ? s.pinned.filter((p) => p !== id) : [...s.pinned, id] })
  return (
    <div className="space-y-4">
      <Card className="space-y-4 p-4">
        <div>
          <h3 className="text-xs font-semibold">Focus</h3>
          <p className="mt-0.5 text-xs text-muted-foreground">Work on your focus project scores 25 more; pinned projects 12.</p>
          <select
            aria-label="Focus project"
            value={s.focus_project ?? ""}
            onChange={(e) => void save({ ...s, focus_project: e.target.value || undefined })}
            className="mt-2 h-8 w-full rounded-md border bg-background/60 px-2 text-xs outline-none focus-visible:border-ring"
          >
            <option value="">No focus project</option>
            {list.map((p) => (
              <option key={p.id} value={p.id}>
                {p.name}
              </option>
            ))}
          </select>
          {list.length > 0 && (
            <div className="mt-2 flex flex-wrap gap-1">
              {list.map((p) => {
                const on = s.pinned.includes(p.id)
                return (
                  <button
                    key={p.id}
                    onClick={() => void togglePin(p.id)}
                    aria-pressed={on}
                    className={cn("inline-flex h-6 items-center gap-1 rounded-full border px-2 text-2xs", on ? "border-brand/50 bg-brand-soft text-brand-fg" : "text-muted-foreground hover:bg-accent/50")}
                  >
                    <Pin className="size-3" /> {p.name}
                  </button>
                )
              })}
            </div>
          )}
        </div>
        <div>
          <h3 className="text-xs font-semibold">Agent ranking</h3>
          <p className="mt-0.5 text-xs text-muted-foreground">Re-rank on a schedule while Lucidbench is open. Off by default; each ranking runs on your account.</p>
          <select
            aria-label="Ranking schedule"
            value={s.schedule_hours}
            onChange={(e) => void save({ ...s, schedule_hours: Number(e.target.value) })}
            className="mt-2 h-8 w-full rounded-md border bg-background/60 px-2 text-xs outline-none focus-visible:border-ring"
          >
            {SCHEDULES.map((h) => (
              <option key={h} value={h}>
                {h === 0 ? "Only when I ask" : `Every ${plural(h, "hour")}, while open`}
              </option>
            ))}
          </select>
        </div>
      </Card>

      <Card className="p-4">
        <h3 className="text-xs font-semibold">How the score works</h3>
        <p className="mt-1 text-xs text-muted-foreground">
          Urgency (failing CI, failing checks, an agent waiting, due dates), then work that unblocks other projects, staleness, your focus, effort labels, and usage headroom: work for a
          provider near its limit drops. Set-aside items count against their project.
        </p>
      </Card>

      {view.hidden.length > 0 && (
        <Card className="p-4" data-testid="nextup-hidden">
          <h3 className="text-xs font-semibold">Snoozed and set aside</h3>
          <ul className="mt-2 space-y-1.5">
            {view.hidden.map((h) => (
              <li key={h.id} className="flex items-center gap-2 text-xs">
                <span className="min-w-0 flex-1 truncate text-muted-foreground" title={h.title}>
                  {h.title}
                </span>
                <span className="shrink-0 text-2xs text-subtle-foreground">{h.until ? `until ${new Date(h.until).toLocaleDateString()}` : h.reason}</span>
                <button
                  onClick={() =>
                    void restore(h.id)
                      .then(onChange)
                      .catch((e) => toast.error("Could not restore", { description: errorMessage(e) }))
                  }
                  aria-label={`Bring back ${h.title}`}
                  className="shrink-0 rounded p-1 text-subtle-foreground hover:bg-accent hover:text-foreground"
                >
                  <RotateCcw className="size-3" />
                </button>
              </li>
            ))}
          </ul>
        </Card>
      )}
    </div>
  )
}
