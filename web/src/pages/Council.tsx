import { useEffect, useMemo, useRef, useState, type ReactNode } from "react"
import {
  ArrowLeft,
  ArrowRight,
  Check,
  CheckCircle2,
  ChevronDown,
  CircleDashed,
  FileClock,
  FileText,
  FlaskConical,
  KanbanSquare,
  Lightbulb,
  Loader2,
  MessagesSquare,
  NotebookPen,
  PenLine,
  Play,
  RotateCcw,
  ShieldAlert,
  Sparkles,
  TriangleAlert,
  Vote,
  WandSparkles,
  type LucideIcon,
} from "lucide-react"
import { toast } from "sonner"

import { ProviderMark, ProviderTile, providerInfo, tintVar } from "@/components/ProviderMark"
import { PageHeader, RefreshButton } from "@/components/Shell"
import { StateSprite } from "@/components/StateSprite"
import { Badge, StatusPill } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card } from "@/components/ui/card"
import { ConfirmDialog, type ConfirmRequest } from "@/components/ui/confirm"
import { Dialog } from "@/components/ui/dialog"
import { EmptyState, ErrorState, Skeleton } from "@/components/ui/states"
import { ApiError, errorMessage, getJSON, usePoll } from "@/lib/api"
import { useApp } from "@/lib/app"
import { DEFAULT_BOARD } from "@/lib/boards"
import {
  approveBrief,
  askAgain,
  COMPOSE_EVENT,
  COUNCIL_POLL_MS,
  COUNCIL_PROVIDERS,
  latestBlockers,
  maxCalls,
  roundsLabel,
  seconds,
  SESSIONS_PATH,
  SEVERITY,
  startCouncil,
  STATUS,
  tokens,
  totals,
  usd,
  VERDICT,
  type CouncilProvider,
  type CouncilRound,
  type CouncilSession,
  type CouncilStep,
  type CouncilSummary,
  type Severity,
  useCouncilSession,
} from "@/lib/council"
import { PROJECTS_POLL_MS, type ProjectList } from "@/lib/projects"
import { putHandoff, takeHandoff } from "@/lib/prompts"
import { absoluteTime, relativeTime, useNow } from "@/lib/time"
import { cn, isMac, plural } from "@/lib/utils"
import type { ModulePageProps } from "@/modules/types"
import type { Account } from "@/pages/Accounts"

export default function Council({ subpath }: ModulePageProps) {
  const id = subpath[0]
  return id ? <SessionPage key={id} id={id} /> : <Home />
}

const label = (p: string) => providerInfo(p)?.label ?? p

/** Provider ids in a log line read as their names. */
const named = (text: string) => text.replace(/\b(claude|codex|grok)\b/g, (p) => label(p))

function joinNames(ps: string[]): string {
  const ls = ps.map(label)
  if (ls.length <= 1) return ls.join("")
  return `${ls.slice(0, -1).join(", ")} and ${ls[ls.length - 1]}`
}

/* ---------- home: composer and sessions ---------- */

function Home() {
  // Poll faster while a council runs, so its row moves on by itself.
  const [live, setLive] = useState(false)
  const poll = usePoll<CouncilSummary[]>(SESSIONS_PATH, live ? 4000 : COUNCIL_POLL_MS)
  const shown = poll.data ?? []
  const anyRunning = shown.some((s) => s.status === "running")
  useEffect(() => setLive(anyRunning), [anyRunning])
  return (
    <div className="space-y-6">
      <PageHeader
        icon={<Vote />}
        title="Council"
        description="Turn a messy braindump into a clear brief. One model drafts it, two others look for what would go wrong, and you approve the result."
        actions={<RefreshButton refreshing={poll.refreshing} updatedAt={poll.updatedAt} />}
      />
      <Composer />
      {poll.loading && !poll.data ? (
        <Card className="space-y-2 p-5">
          <Skeleton className="h-10" />
          <Skeleton className="h-10" />
          <Skeleton className="h-10" />
        </Card>
      ) : poll.error && !poll.data ? (
        <ErrorState title="Could not load council sessions" message={poll.error.message} onRetry={poll.refresh} />
      ) : shown.length === 0 ? (
        <HowItWorks />
      ) : (
        <SessionsList list={shown} />
      )}
    </div>
  )
}

type SignIn = "signed_in" | "expired" | "missing"

function signInOf(accounts: Account[] | null, p: string): SignIn {
  const mine = (accounts ?? []).filter((a) => a.provider === p && a.location === "host")
  if (mine.some((a) => a.status === "logged_in")) return "signed_in"
  if (mine.some((a) => a.status === "expired")) return "expired"
  return "missing"
}

const SIGN_IN: Record<SignIn, { dot: string; label: string }> = {
  signed_in: { dot: "bg-success", label: "signed in" },
  expired: { dot: "bg-warning", label: "sign-in expired: the council will skip it" },
  missing: { dot: "bg-neutral", label: "not signed in: the council will skip it" },
}

const PLACEHOLDER = `Dump the idea as it is. For example:

the app leaves temp folders behind when something crashes, and they pile up. want them cleaned up on start, but never while a run is still using one…`

function Composer() {
  const { open } = useApp()
  const accounts = usePoll<Account[]>("/api/accounts", 30000)
  const projects = usePoll<ProjectList>("/api/projects", PROJECTS_POLL_MS)
  const [input, setInput] = useState("")
  const [project, setProject] = useState("")
  const [proposer, setProposer] = useState<CouncilProvider>("claude")
  const [critics, setCritics] = useState<CouncilProvider[]>(["codex", "grok"])
  const [rounds, setRounds] = useState(2)
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState<ApiError | null>(null)
  const area = useRef<HTMLTextAreaElement>(null)

  useEffect(() => {
    // A braindump composed in Prompt Studio arrives here once.
    const h = takeHandoff("council")
    if (h) {
      setInput(h.text.slice(0, 20000))
      if (h.project) setProject(h.project)
    }
    area.current?.focus()
    const onCompose = () => area.current?.focus()
    window.addEventListener(COMPOSE_EVENT, onCompose)
    return () => window.removeEventListener(COMPOSE_EVENT, onCompose)
  }, [])

  const chooseProposer = (p: CouncilProvider) => {
    setProposer(p)
    setCritics((cs) => {
      const rest = cs.filter((c) => c !== p)
      // The old proposer becomes a critic, so the council stays three strong.
      return rest.length < 2 && !rest.includes(proposer) && proposer !== p ? [...rest, proposer] : rest
    })
  }
  const toggleCritic = (p: CouncilProvider) =>
    setCritics((cs) => (cs.includes(p) ? cs.filter((c) => c !== p) : COUNCIL_PROVIDERS.filter((x) => x === p || cs.includes(x))))

  const ps = projects.data?.projects ?? []
  const chosen = ps.find((p) => p.id === project)
  const confidential = chosen?.visibility === "confidential"
  const empty = input.trim() === ""
  const calls = maxCalls(critics.length, rounds)

  const submit = async () => {
    if (empty || busy || confidential) return
    setBusy(true)
    setErr(null)
    try {
      const s = await startCouncil({ input, project: project || undefined, proposer, critics, rounds })
      open("council", [s.id])
    } catch (e) {
      setErr(e instanceof ApiError ? e : new ApiError(0, errorMessage(e)))
      setBusy(false)
    }
  }

  return (
    <div className="space-y-3">
      <Card className="relative overflow-hidden focus-within:border-border-strong">
        <div aria-hidden className="pointer-events-none absolute inset-x-0 top-0 h-28 bg-gradient-to-b from-brand-soft to-transparent opacity-70" />
        <div className="relative px-5 pb-2 pt-4">
          <div className="flex items-center justify-between gap-3">
            <label htmlFor="braindump" className="flex items-center gap-2 text-sm font-semibold">
              <PenLine className="size-3.5 text-brand" />
              New braindump
            </label>
            <span className="text-xs text-subtle-foreground max-[720px]:hidden">Half sentences are fine. What the council cannot guess becomes an open question.</span>
          </div>
          <textarea
            id="braindump"
            ref={area}
            value={input}
            onChange={(e) => setInput(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
                e.preventDefault()
                void submit()
              }
            }}
            rows={7}
            maxLength={20000}
            placeholder={PLACEHOLDER}
            className="mt-2 block min-h-40 w-full resize-y bg-transparent text-[0.9375rem] leading-relaxed outline-none placeholder:text-subtle-foreground/70"
          />
          <div className="flex justify-end pb-1 text-2xs tabular-nums text-subtle-foreground">{input.length > 0 && `${input.length.toLocaleString()} characters`}</div>
        </div>
        <div className="relative flex flex-wrap items-end gap-x-5 gap-y-3 border-t bg-muted/30 px-5 py-3">
          <Field label="Project">
            <label className="relative flex items-center">
              <span className="sr-only">Project</span>
              <select
                value={project}
                onChange={(e) => setProject(e.target.value)}
                className={cn(
                  "h-8 max-w-52 appearance-none rounded-md border bg-background/60 pl-2.5 pr-7 text-xs outline-none transition-colors hover:border-border-strong focus-visible:border-ring",
                  project ? "text-foreground" : "text-muted-foreground",
                )}
              >
                <option value="">No project</option>
                {ps.map((p) => (
                  <option key={p.id} value={p.id} disabled={p.visibility === "confidential"}>
                    {p.name}
                    {p.visibility === "confidential" ? " (confidential)" : ""}
                  </option>
                ))}
              </select>
              <ChevronDown className="pointer-events-none absolute right-2 size-3.5 text-subtle-foreground" />
            </label>
          </Field>
          <Field label="Proposer">
            <div role="radiogroup" aria-label="Proposer" className="flex gap-1">
              {COUNCIL_PROVIDERS.map((p) => (
                <ProviderChip key={p} provider={p} on={proposer === p} signIn={signInOf(accounts.data, p)} role="radio" onClick={() => chooseProposer(p)} />
              ))}
            </div>
          </Field>
          <Field label="Critics">
            <div role="group" aria-label="Critics" className="flex gap-1">
              {COUNCIL_PROVIDERS.filter((p) => p !== proposer).map((p) => (
                <ProviderChip key={p} provider={p} on={critics.includes(p)} signIn={signInOf(accounts.data, p)} role="checkbox" onClick={() => toggleCritic(p)} />
              ))}
            </div>
          </Field>
          <Field label="Rounds">
            <div role="radiogroup" aria-label="Rounds" className="inline-flex h-8 rounded-md border bg-background/40 p-0.5">
              {[1, 2].map((n) => (
                <button
                  key={n}
                  role="radio"
                  aria-checked={rounds === n}
                  onClick={() => setRounds(n)}
                  title={n === 1 ? "One critique and one revision: quicker and cheaper" : "A second round runs only while a critic still sees a blocker"}
                  className={cn(
                    "rounded px-2.5 text-xs transition-colors",
                    rounds === n ? "bg-elevated font-medium text-foreground shadow-card" : "text-muted-foreground hover:text-foreground",
                  )}
                >
                  {n === 1 ? "1" : "up to 2"}
                </button>
              ))}
            </div>
          </Field>
          <div className="ml-auto flex items-center gap-3">
            <Button
              variant="ghost"
              size="sm"
              title="Shape this braindump in Prompt Studio, with context from the project and Memory"
              onClick={() => {
                putHandoff({ to: "studio", text: input, project: project || undefined, template: "braindump", from: "council" })
                open("studio", ["braindump"])
              }}
            >
              <WandSparkles /> Open in Studio
            </Button>
            <span className="text-right text-xs leading-tight text-subtle-foreground max-[720px]:hidden">
              {critics.length === 0 ? "Self-critique" : `${critics.length} ${critics.length === 1 ? "critic" : "critics"}`} · up to {calls} calls
              <br />
              on your own signed-in CLIs
            </span>
            <Button size="lg" onClick={() => void submit()} disabled={empty || busy || confidential} className="px-4">
              {busy ? <Loader2 className="animate-spin" /> : <Sparkles />}
              {busy ? "Convening" : "Convene council"}
              <kbd className="ml-1 rounded border border-primary-foreground/25 px-1 font-sans text-2xs opacity-80">{isMac() ? "⌘" : "Ctrl"}↵</kbd>
            </Button>
          </div>
        </div>
      </Card>
      {confidential && (
        <Callout tone="danger" icon={ShieldAlert} title={`${chosen?.name} is confidential`}>
          The council never sends a confidential project to a provider. Pick another project, or none.
        </Callout>
      )}
      {err && (
        <ErrorState
          title={err.status === 403 && /confidential/i.test(err.message) ? "Refused: confidential" : "Could not start the council"}
          message={err.message}
        />
      )}
    </div>
  )
}

function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="space-y-1.5">
      <div className="text-2xs font-medium uppercase tracking-wider text-subtle-foreground">{label}</div>
      {children}
    </div>
  )
}

function ProviderChip({
  provider,
  on,
  signIn,
  role,
  onClick,
}: {
  provider: CouncilProvider
  on: boolean
  signIn: SignIn
  role: "radio" | "checkbox"
  onClick: () => void
}) {
  const tint = tintVar(provider)
  return (
    <button
      role={role}
      aria-checked={on}
      onClick={onClick}
      title={`${label(provider)}: ${SIGN_IN[signIn].label}`}
      style={on ? { borderColor: `color-mix(in oklch, ${tint} 45%, transparent)`, backgroundColor: `color-mix(in oklch, ${tint} 12%, transparent)` } : undefined}
      className={cn(
        "relative flex h-8 items-center gap-1.5 rounded-md border px-2.5 text-xs transition-[background-color,border-color,color] duration-150",
        on ? "font-medium text-foreground" : "bg-background/40 text-muted-foreground hover:border-border-strong hover:text-foreground",
      )}
    >
      <span style={{ color: on ? tint : undefined }} className={cn(!on && "opacity-70")}>
        <ProviderMark provider={provider} className="size-3.5" />
      </span>
      {label(provider)}
      <span className={cn("size-1.5 rounded-full", SIGN_IN[signIn].dot, signIn === "missing" && "opacity-60")} />
    </button>
  )
}

function Callout({ tone, icon: Icon, title, children }: { tone: "info" | "warning" | "danger"; icon: LucideIcon; title: string; children?: ReactNode }) {
  const cls = {
    info: "border-info/25 bg-info-soft [&_.ci]:text-info-fg",
    warning: "border-warning/30 bg-warning-soft [&_.ci]:text-warning-fg",
    danger: "border-danger/30 bg-danger-soft [&_.ci]:text-danger-fg",
  }[tone]
  return (
    <div role={tone === "info" ? "status" : "alert"} className={cn("flex items-start gap-3 rounded-lg border p-3.5", cls)}>
      <Icon className="ci mt-0.5 size-4 shrink-0" />
      <div className="min-w-0 text-sm">
        <div className="font-medium">{title}</div>
        {children && <div className="mt-0.5 text-muted-foreground">{children}</div>}
      </div>
    </div>
  )
}

/* ---------- empty state ---------- */

function HowItWorks() {
  const steps: { icon: ReactNode; title: string; text: string }[] = [
    { icon: <PenLine className="size-3.5" />, title: "You braindump", text: "Messy is fine. Name a project if it is for one." },
    {
      icon: <ProviderMark provider="claude" className="size-3.5" />,
      title: "A proposer drafts the brief",
      text: "Problem, outcome, done criteria each with a proof, risks, first steps, open questions.",
    },
    {
      icon: (
        <span className="flex -space-x-1">
          <ProviderMark provider="codex" className="size-3.5" />
          <ProviderMark provider="grok" className="size-3.5" />
        </span>
      ),
      title: "Two critics look for what would go wrong",
      text: "In parallel, each returns a verdict and points by severity.",
    },
    { icon: <RotateCcw className="size-3.5" />, title: "The draft is revised", text: "A second round runs only while a critic still sees a blocker." },
    { icon: <KanbanSquare className="size-3.5" />, title: "You approve", text: "The brief is saved in Memory and a card lands in Ready on your work board." },
  ]
  return (
    <Card className="overflow-hidden">
      <div className="grid @3xl:grid-cols-[1fr_1.25fr]">
        <EmptyState
          className="py-10"
          icon={<Vote />}
          satellites={(["claude", "codex", "grok"] as const).map((p) => (
            <ProviderTile key={p} provider={p} size="sm" />
          ))}
          title="No briefs yet"
          description="The council turns what is on your mind into a brief you could start from tomorrow. It runs on your own signed-in CLIs, and confidential projects never reach a provider."
        />
        <ol className="relative space-y-3.5 border-t bg-muted/25 p-5 @3xl:border-l @3xl:border-t-0">
          <span aria-hidden className="absolute bottom-8 left-[34px] top-8 w-px bg-border" />
          {steps.map((s, i) => (
            <li key={s.title} className="relative flex items-start gap-3">
              <span className="relative z-[1] flex size-7 shrink-0 items-center justify-center rounded-lg border bg-elevated text-muted-foreground shadow-card">
                {s.icon}
              </span>
              <div className="min-w-0 pt-0.5">
                <div className="text-sm font-medium">
                  <span className="mr-1.5 font-mono text-2xs text-subtle-foreground">{i + 1}</span>
                  {s.title}
                </div>
                <div className="text-xs text-muted-foreground">{s.text}</div>
              </div>
            </li>
          ))}
        </ol>
      </div>
    </Card>
  )
}

/* ---------- sessions list ---------- */

const STATUS_ICON: Record<CouncilSummary["status"], { icon: LucideIcon; cls: string }> = {
  running: { icon: Loader2, cls: "text-info [&_svg]:animate-spin" },
  draft: { icon: FileClock, cls: "text-warning" },
  approved: { icon: CheckCircle2, cls: "text-success" },
  failed: { icon: TriangleAlert, cls: "text-danger" },
}

function ProviderStack({ providers }: { providers: string[] }) {
  return (
    <span className="flex -space-x-1.5">
      {providers.map((p) => (
        <ProviderTile key={p} provider={p} size="sm" className="size-5 rounded-md ring-2 ring-card [&_svg]:size-3" />
      ))}
    </span>
  )
}

function SessionsList({ list }: { list: CouncilSummary[] }) {
  const { open } = useApp()
  const now = useNow(15000)
  const waiting = list.filter((s) => s.status === "draft").length
  return (
    <Card className="overflow-hidden">
      <div className="flex items-center justify-between px-5 pb-3 pt-4">
        <h2 className="flex items-center gap-2 text-sm font-semibold">
          Sessions
          <span className="rounded-full bg-muted px-1.5 text-2xs tabular-nums text-muted-foreground">{list.length}</span>
        </h2>
        {waiting > 0 && (
          <StatusPill tone="warning">
            {waiting} waiting for approval
          </StatusPill>
        )}
      </div>
      <ul className="divide-y border-t">
        {list.map((s) => {
          const si = STATUS_ICON[s.status]
          return (
            <li key={s.id}>
              <button
                onClick={() => open("council", [s.id])}
                className="flex w-full items-center gap-3.5 px-5 py-3 text-left outline-none transition-colors hover:bg-accent/30 focus-visible:bg-accent/40"
              >
                <span className={cn("flex size-8 shrink-0 items-center justify-center rounded-lg border bg-background/60 [&_svg]:size-4", si.cls)}>
                  <si.icon />
                </span>
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2">
                    <span className="truncate text-sm font-medium">{s.title || (s.status === "running" ? "Drafting a brief…" : "Untitled braindump")}</span>
                    {s.project && <Badge className="shrink-0 font-mono">{s.project}</Badge>}
                  </div>
                  <div className="truncate text-xs text-muted-foreground">{s.status === "failed" && s.error ? s.error : s.input}</div>
                </div>
                <div className="flex shrink-0 items-center gap-4 text-xs tabular-nums text-subtle-foreground">
                  <ProviderStack providers={[s.proposer, ...s.critics]} />
                  <span className="w-16 max-[900px]:hidden">
                    {plural(s.rounds, "round")}
                  </span>
                  <span className="w-12 text-right font-mono text-2xs max-[900px]:hidden" title={`${tokens(s.input_tokens)} in · ${tokens(s.output_tokens)} out`}>
                    {usd(s.cost_usd)}
                  </span>
                  <span className="w-40 max-[1100px]:hidden">
                    <StatusPill tone={STATUS[s.status].tone} pulse={s.status === "running"}>
                      {STATUS[s.status].label}
                    </StatusPill>
                  </span>
                  <time dateTime={s.created} title={absoluteTime(s.created)} className="w-16 text-right text-muted-foreground">
                    {relativeTime(s.created, now)}
                  </time>
                </div>
              </button>
            </li>
          )
        })}
      </ul>
    </Card>
  )
}

/* ---------- one session ---------- */

function SessionPage({ id }: { id: string }) {
  const { open } = useApp()
  const { session, error, update } = useCouncilSession(id)
  const back = (
    <button onClick={() => open("council")} className="inline-flex items-center gap-1.5 text-xs font-medium text-muted-foreground transition-colors hover:text-foreground">
      <ArrowLeft className="size-3.5" /> Council
    </button>
  )
  if (error)
    return (
      <div className="space-y-4">
        {back}
        <ErrorState title={error.status === 404 ? "No such council session" : "Could not load the session"} message={error.message} />
      </div>
    )
  if (!session)
    return (
      <div className="space-y-5" aria-busy="true">
        {back}
        <Skeleton className="h-8 w-80" />
        <Skeleton className="h-16 rounded-xl" />
        <div className="grid grid-cols-2 gap-3">
          <Skeleton className="h-40 rounded-xl" />
          <Skeleton className="h-40 rounded-xl" />
        </div>
      </div>
    )
  const s = session
  const done = s.status === "draft" || s.status === "approved"
  // Until the brief is written, the newest draft names the session.
  const latest = [...s.rounds].reverse().flatMap((r) => [r.synthesis, r.proposal]).find((st) => st?.text)
  const title = s.title || (latest?.text ? parseBrief(latest.text).title : "")
  const refetch = () => getJSON<CouncilSession>(`${SESSIONS_PATH}/${s.id}`).then(update)
  return (
    <div className="space-y-5">
      {back}
      <PageHeader
        eyebrow={s.status === "running" && title ? "Working title" : undefined}
        title={title || (s.status === "failed" ? "The council could not finish" : "Drafting a brief…")}
        description={<span className="line-clamp-2">{s.input}</span>}
        actions={
          <StatusPill tone={STATUS[s.status].tone} pulse={s.status === "running"}>
            {STATUS[s.status].label}
          </StatusPill>
        }
      />

      <div className="grid gap-5 @4xl:grid-cols-[minmax(0,1fr)_280px]">
        <div className="min-w-0 space-y-5">
          <StageStrip s={s} />
          {s.status === "failed" && s.error && <ErrorState title="The council stopped" message={s.error} />}
          {s.notes.length > 0 && (
            <Callout tone="info" icon={CircleDashed} title={s.mode === "self-critique" ? "Self-critique: one provider checked its own draft" : "Not every critic took part"}>
              <ul className="space-y-0.5">
                {s.notes.map((n) => (
                  <li key={n}>{n}</li>
                ))}
              </ul>
            </Callout>
          )}
          {done && s.brief && <BriefCard s={s} onChanged={update} refetch={refetch} />}
          <section className="space-y-3">
            {done && <h2 className="pt-2 text-sm font-semibold text-muted-foreground">How the council got here</h2>}
            <Timeline s={s} />
          </section>
        </div>
        <Aside s={s} />
      </div>
    </div>
  )
}

/* stage strip */

const STAGES: { id: string; label: string; icon: LucideIcon }[] = [
  { id: "propose", label: "Draft", icon: PenLine },
  { id: "critique", label: "Critique", icon: MessagesSquare },
  { id: "synthesise", label: "Revise", icon: RotateCcw },
  { id: "write", label: "Brief", icon: FileText },
]

function StageStrip({ s }: { s: CouncilSession }) {
  const at = s.status === "running" ? STAGES.findIndex((x) => x.id === s.stage) : s.status === "failed" ? -1 : STAGES.length
  const round = s.rounds.length
  const thinking = s.thinking
  return (
    <Card className="relative overflow-hidden px-5 py-4">
      {s.status === "running" && <div aria-hidden className="busy-shimmer absolute inset-x-0 top-0 h-px" />}
      <div className="flex flex-wrap items-center justify-between gap-3">
        <ol className="flex items-center gap-1.5">
          {STAGES.map((st, i) => {
            const state = i < at ? "done" : i === at ? "now" : "todo"
            return (
              <li key={st.id} className="flex items-center gap-1.5">
                <span
                  className={cn(
                    "flex h-7 items-center gap-1.5 rounded-full border px-2.5 text-xs transition-colors",
                    state === "done" && "border-success/30 bg-success-soft text-success-fg",
                    state === "now" && "border-brand/40 bg-brand-soft font-medium text-brand-fg",
                    state === "todo" && "text-subtle-foreground",
                  )}
                >
                  {state === "done" ? <Check className="size-3.5" /> : state === "now" ? <Loader2 className="size-3.5 animate-spin" /> : <st.icon className="size-3.5" />}
                  {st.label}
                </span>
                {i < STAGES.length - 1 && <span className={cn("h-px w-4", i < at ? "bg-success/40" : "bg-border")} />}
              </li>
            )
          })}
        </ol>
        <div className="flex items-center gap-2 text-xs text-muted-foreground">
          {s.status === "running" && <StateSprite state="thinking" className="-my-2 size-9" />}
          {s.status === "running" && thinking.length > 0 && (
            <span className="flex items-center gap-1.5">
              <ProviderStack providers={thinking} />
              {joinNames(thinking)} {thinking.length === 1 ? "is" : "are"} thinking…
            </span>
          )}
          {s.status !== "running" && (
            <span>
              {roundsLabel({ rounds: round, max_rounds: s.max_rounds })}
              {s.stopped_early ? " · stopped early, no blocker left" : round === s.max_rounds && s.status !== "failed" ? " · round cap reached" : ""}
            </span>
          )}
          {s.status === "running" && thinking.length === 0 && (
            <span>{Math.max(round, 1) > s.max_rounds ? `Round ${round} (Ask again)` : `Round ${Math.max(round, 1)} of ${s.max_rounds}`}</span>
          )}
        </div>
      </div>
    </Card>
  )
}

/* timeline */

function Timeline({ s }: { s: CouncilSession }) {
  return (
    <ol className="space-y-5">
      {s.rounds.map((r, i) => (
        <RoundView key={r.n} r={r} s={s} last={i === s.rounds.length - 1} />
      ))}
    </ol>
  )
}

function RoundView({ r, s, last }: { r: CouncilRound; s: CouncilSession; last: boolean }) {
  const blockers = r.critiques.filter((c) => c.verdict === "blocker").length
  const critiquesDone = r.critiques.length > 0 && r.critiques.every((c) => !c.running)
  let outcome = ""
  if (critiquesDone && (!last || s.status !== "running")) {
    if (blockers > 0) outcome = `${blockers} ${blockers === 1 ? "critic" : "critics"} reported a blocker${last ? "" : ", so another round ran"}`
    else outcome = r.synthesis ? "No blocker: the revision is the brief" : "Every critic was content: the draft stands"
  }
  return (
    <li className="space-y-3">
      <div className="flex items-center gap-3">
        <span className="flex h-6 items-center rounded-full border bg-elevated px-2.5 text-xs font-semibold tabular-nums shadow-card">Round {r.n}{r.n > s.max_rounds ? " · Ask again" : ""}</span>
        <span className="h-px flex-1 bg-border" />
      </div>
      {r.notes && (
        <div className="rounded-lg border border-brand/25 bg-brand-soft px-4 py-2.5 text-sm">
          <span className="text-xs font-medium text-brand-fg">You asked again: </span>
          {r.notes}
        </div>
      )}
      {r.proposal && <DraftStep step={r.proposal} label="First draft" />}
      {r.critiques.length > 0 && (
        <div className={cn("grid gap-3", r.critiques.length > 1 && "@2xl:grid-cols-2", r.critiques.length > 2 && "@5xl:grid-cols-3")}>
          {r.critiques.map((c) => (
            <CriticCard key={`${c.provider}-${c.role}`} step={c} />
          ))}
        </div>
      )}
      {r.synthesis && <DraftStep step={r.synthesis} label="Revision" />}
      {outcome && (
        <p className="flex items-center gap-2 pl-1 text-xs text-muted-foreground">
          <ArrowRight className="size-3.5 text-subtle-foreground" />
          {outcome}
        </p>
      )}
    </li>
  )
}

function StepMeta({ step }: { step: CouncilStep }) {
  const u = step.usage
  if (step.running) return <span className="text-xs text-info-fg">thinking…</span>
  if (!u) return null
  const parts = [seconds(u.duration_ms)]
  const tin = (u.input_tokens ?? 0) + (u.cache_read_tokens ?? 0) + (u.cache_write_tokens ?? 0)
  if (tin || u.output_tokens) parts.push(`${tokens(tin)} in · ${tokens(u.output_tokens ?? 0)} out`)
  if (u.cost_usd) parts.push(usd(u.cost_usd))
  return <span className="font-mono text-2xs tabular-nums text-subtle-foreground">{parts.join(" · ")}</span>
}

function Thinking({ lines = 3 }: { lines?: number }) {
  return (
    <div className="space-y-2 pt-1" aria-label="Thinking">
      {Array.from({ length: lines }, (_, i) => (
        <Skeleton key={i} className={cn("h-3", i === lines - 1 ? "w-2/5" : i % 2 ? "w-4/5" : "w-full")} />
      ))}
    </div>
  )
}

function DraftStep({ step, label: what }: { step: CouncilStep; label: string }) {
  const [openDraft, setOpenDraft] = useState(false)
  const body = step.text ? parseBrief(step.text) : null
  return (
    <Card className="relative overflow-hidden">
      {step.running && <div aria-hidden className="busy-shimmer absolute inset-x-0 top-0 h-px" />}
      <div className="flex items-center gap-3 px-4 py-3">
        <ProviderTile provider={step.provider} size="sm" />
        <div className="min-w-0 flex-1">
          <div className="truncate text-sm font-medium">
            {label(step.provider)} <span className="font-normal text-muted-foreground">· {what}</span>
          </div>
          {body?.title && <div className="truncate text-xs text-muted-foreground">{body.title}</div>}
        </div>
        <StepMeta step={step} />
        {step.text && (
          <Button variant="ghost" size="sm" onClick={() => setOpenDraft((o) => !o)} aria-expanded={openDraft}>
            {openDraft ? "Hide" : "Show"} <ChevronDown className={cn("transition-transform", openDraft && "rotate-180")} />
          </Button>
        )}
      </div>
      {step.running && (
        <div className="px-4 pb-4">
          <Thinking />
        </div>
      )}
      {step.error && <div className="border-t px-4 py-2.5 text-xs text-danger-fg">{step.error}</div>}
      {openDraft && body && (
        <div className="border-t bg-muted/20 px-5 py-4">
          <BriefBody b={body} compact />
        </div>
      )}
    </Card>
  )
}

const SEV_ORDER: Severity[] = ["blocker", "major", "minor"]

function CriticCard({ step }: { step: CouncilStep }) {
  const v = step.verdict ? VERDICT[step.verdict] : null
  const points = [...(step.points ?? [])].sort((a, b) => SEV_ORDER.indexOf(a.severity) - SEV_ORDER.indexOf(b.severity))
  return (
    <Card className={cn("relative flex flex-col overflow-hidden", step.skipped && "border-dashed bg-card/60")}>
      {step.running && <div aria-hidden className="busy-shimmer absolute inset-x-0 top-0 h-px" />}
      <div className="flex items-center gap-2.5 px-4 pb-2.5 pt-3">
        <ProviderTile provider={step.provider} size="sm" muted={step.skipped} />
        <div className="min-w-0 flex-1">
          <div className="truncate text-sm font-medium">{label(step.provider)}</div>
          <div className="text-2xs text-subtle-foreground">{step.role === "self-critique" ? "Self-critique" : "Critic"}</div>
        </div>
        {step.running ? (
          <StatusPill tone="info" pulse>
            Reading
          </StatusPill>
        ) : step.skipped ? (
          <StatusPill tone="neutral">Skipped</StatusPill>
        ) : v ? (
          <StatusPill tone={v.tone}>{v.label}</StatusPill>
        ) : step.error ? (
          <StatusPill tone="danger">Failed</StatusPill>
        ) : null}
      </div>
      <div className="flex-1 px-4 pb-3">
        {step.running && <Thinking lines={4} />}
        {step.skipped && <p className="text-xs text-muted-foreground">{step.error}</p>}
        {!step.skipped && step.error && <p className="break-words text-xs text-danger-fg">{step.error}</p>}
        {v && points.length === 0 && <p className="text-xs text-muted-foreground">Nothing to change.</p>}
        {points.length > 0 && (
          <ul className="space-y-2">
            {points.map((p, i) => (
              <li key={i} className="flex items-start gap-2 text-[0.8125rem] leading-snug">
                <span className={cn("mt-[0.4rem] size-1.5 shrink-0 rounded-full", SEVERITY[p.severity].dot)} />
                <span className="min-w-0">
                  <span className={cn("mr-1.5 text-2xs font-semibold uppercase tracking-wide", SEVERITY[p.severity].text)}>{SEVERITY[p.severity].label}</span>
                  <Inline text={p.text} />
                </span>
              </li>
            ))}
          </ul>
        )}
      </div>
      {!step.running && step.usage && (
        <div className="border-t px-4 py-2">
          <StepMeta step={step} />
        </div>
      )}
    </Card>
  )
}

/* brief */

export interface ParsedBrief {
  title: string
  sections: { heading: string; lines: string[] }[]
}

export function parseBrief(md: string): ParsedBrief {
  const out: ParsedBrief = { title: "", sections: [] }
  let cur: { heading: string; lines: string[] } | null = null
  for (const raw of md.replace(/\r\n/g, "\n").split("\n")) {
    const line = raw.trimEnd()
    if (line.startsWith("# ") && !out.title) {
      out.title = line.slice(2).trim()
      continue
    }
    const h = /^#{2,3} +(.+)$/.exec(line)
    if (h) {
      cur = { heading: h[1].trim(), lines: [] }
      out.sections.push(cur)
      continue
    }
    if (!cur) {
      if (line.trim()) {
        cur = { heading: "", lines: [] }
        out.sections.push(cur)
      } else continue
    }
    cur.lines.push(line)
  }
  return out
}

/** **bold**, `code` and [[links]] inside a line. */
function Inline({ text }: { text: string }) {
  const parts = text.split(/(\*\*[^*]+\*\*|`[^`]+`|\[\[[^\]]+\]\])/g)
  return (
    <>
      {parts.map((p, i) => {
        if (p.startsWith("**") && p.endsWith("**") && p.length > 4) return <strong key={i} className="font-semibold">{p.slice(2, -2)}</strong>
        if (p.startsWith("`") && p.endsWith("`") && p.length > 2)
          return (
            <code key={i} className="rounded bg-muted px-1 py-px font-mono text-[0.85em]">
              {p.slice(1, -1)}
            </code>
          )
        if (p.startsWith("[[") && p.endsWith("]]")) return <span key={i} className="text-brand-fg">{p.slice(2, -2).split("|").pop()}</span>
        return <span key={i}>{p}</span>
      })}
    </>
  )
}

const CRITERION_RE = /^[-*] \[( |x|X)\] *(.*)$/
const BULLET_RE = /^[-*] +(.*)$/
const ORDERED_RE = /^(\d+)[.)] +(.*)$/

function Criterion({ text, done }: { text: string; done: boolean }) {
  // "D1 — outcome — proof: how"
  const m = /^(D\d+)\s*[—–-]+\s*(.*)$/.exec(text)
  const tag = m?.[1]
  let rest = m ? m[2] : text
  let proof = ""
  const pi = rest.toLowerCase().lastIndexOf("proof:")
  if (pi >= 0) {
    proof = rest.slice(pi + 6).trim()
    rest = rest.slice(0, pi).replace(/\s*[—–-]+\s*$/, "").trim()
  }
  return (
    <li className="flex items-start gap-3 py-2.5">
      <span
        className={cn(
          "mt-0.5 flex size-4 shrink-0 items-center justify-center rounded border",
          done ? "border-success/50 bg-success-soft text-success" : "border-border-strong bg-background",
        )}
      >
        {done && <Check className="size-3" />}
      </span>
      <div className="min-w-0 flex-1">
        <div className="text-sm leading-snug">
          {tag && <span className="mr-2 font-mono text-2xs font-semibold text-brand-fg">{tag}</span>}
          <Inline text={rest} />
        </div>
        {proof ? (
          <div className="mt-1 flex items-start gap-1.5 text-xs text-muted-foreground">
            <FlaskConical className="mt-px size-3.5 shrink-0 text-subtle-foreground" />
            <span>
              <span className="text-subtle-foreground">Proof: </span>
              <Inline text={proof} />
            </span>
          </div>
        ) : (
          <div className="mt-1 text-xs text-warning-fg">No proof given</div>
        )}
      </div>
    </li>
  )
}

function Lines({ lines }: { lines: string[] }) {
  const blocks: ReactNode[] = []
  let list: { kind: "ul" | "ol" | "crit"; items: ReactNode[] } | null = null
  let para: string[] = []
  const flushPara = () => {
    if (para.length) blocks.push(<p key={`p${blocks.length}`} className="text-sm leading-relaxed"><Inline text={para.join(" ")} /></p>)
    para = []
  }
  const flushList = () => {
    if (!list) return
    const k = `l${blocks.length}`
    if (list.kind === "crit") blocks.push(<ul key={k} className="divide-y">{list.items}</ul>)
    else if (list.kind === "ol") blocks.push(<ol key={k} className="list-decimal space-y-1 pl-5 text-sm leading-relaxed marker:text-subtle-foreground">{list.items}</ol>)
    else blocks.push(<ul key={k} className="list-disc space-y-1 pl-5 text-sm leading-relaxed marker:text-subtle-foreground">{list.items}</ul>)
    list = null
  }
  const push = (kind: "ul" | "ol" | "crit", node: ReactNode) => {
    flushPara()
    if (list && list.kind !== kind) flushList()
    list ??= { kind, items: [] }
    list.items.push(node)
  }
  lines.forEach((raw, i) => {
    const line = raw.trim()
    let m: RegExpExecArray | null
    if (!line) {
      flushPara()
      flushList()
    } else if ((m = CRITERION_RE.exec(line))) push("crit", <Criterion key={i} text={m[2]} done={m[1] !== " "} />)
    else if ((m = BULLET_RE.exec(line))) push("ul", <li key={i}><Inline text={m[1]} /></li>)
    else if ((m = ORDERED_RE.exec(line))) push("ol", <li key={i}><Inline text={m[2]} /></li>)
    else if (list && /^\s{2,}/.test(raw)) {
      // a wrapped list item: append to the previous one
      list.items.push(<div key={`c${i}`} className="text-sm text-muted-foreground"><Inline text={line} /></div>)
    } else {
      flushList()
      para.push(line)
    }
  })
  flushPara()
  flushList()
  return <div className="space-y-2">{blocks}</div>
}

const PAIRS: [string, string][] = [
  ["problem", "outcome"],
  ["risks", "first steps"],
]

function Section({ heading, lines, compact }: { heading: string; lines: string[]; compact?: boolean }) {
  return (
    <section className="min-w-0">
      {heading && <h3 className={cn("mb-1.5 font-semibold uppercase tracking-wider text-subtle-foreground", compact ? "text-2xs" : "text-2xs")}>{heading}</h3>}
      <Lines lines={lines} />
    </section>
  )
}

export function BriefBody({ b, compact }: { b: ParsedBrief; compact?: boolean }) {
  const byName = new Map(b.sections.map((s) => [s.heading.toLowerCase(), s]))
  const used = new Set<string>()
  const blocks: ReactNode[] = []
  for (const s of b.sections) {
    const key = s.heading.toLowerCase()
    if (used.has(key)) continue
    const pair = PAIRS.find((p) => p[0] === key)
    const partner = pair ? byName.get(pair[1]) : undefined
    if (pair && partner) {
      used.add(key)
      used.add(pair[1])
      blocks.push(
        <div key={key} className={cn("grid gap-x-8 gap-y-5", !compact && "@2xl:grid-cols-2")}>
          <Section heading={s.heading} lines={s.lines} compact={compact} />
          <Section heading={partner.heading} lines={partner.lines} compact={compact} />
        </div>,
      )
      continue
    }
    used.add(key)
    blocks.push(
      key === "done criteria" && !compact ? (
        <div key={key} className="rounded-lg border bg-background/40 px-4 pb-1 pt-3">
          <Section heading={s.heading} lines={s.lines} />
        </div>
      ) : (
        <Section key={key || `s${blocks.length}`} heading={s.heading} lines={s.lines} compact={compact} />
      ),
    )
  }
  return <div className={cn(compact ? "space-y-4" : "space-y-6")}>{blocks}</div>
}

function CostLine({ s }: { s: CouncilSession }) {
  const t = totals(s.usage)
  return (
    <span className="font-mono text-2xs tabular-nums text-subtle-foreground" title={t.uncosted.length ? `${joinNames(t.uncosted)} did not report a cost` : undefined}>
      {plural(t.calls, "call")} · {tokens(t.input)} in · {tokens(t.output)} out · {usd(t.cost)}
      {t.uncosted.length > 0 && "+"} · {seconds(t.ms)} model time
    </span>
  )
}

function BriefCard({ s, onChanged, refetch }: { s: CouncilSession; onChanged: (s: CouncilSession) => void; refetch: () => Promise<void> }) {
  const { open } = useApp()
  const [confirm, setConfirm] = useState<ConfirmRequest | null>(null)
  const [asking, setAsking] = useState(false)
  const b = useMemo(() => parseBrief(s.brief ?? ""), [s.brief])
  const approved = s.status === "approved"

  // A blocker in the newest critiques is still open: say so, and ask twice.
  const blockers = latestBlockers(s)
  const approve = () =>
    setConfirm({
      title: blockers.length > 0 ? "A critic still sees a blocker" : "Approve this brief?",
      description:
        blockers.length > 0
          ? `The latest critiques list ${plural(blockers.length, "blocker")} that the brief may not have fixed. Ask again with your notes, or approve anyway: the page in Memory is marked approved and a card is added to Ready${s.project ? `, on ${s.project}` : ""}.`
          : `The page in Memory is marked approved and a card is added to Ready on the work board${s.project ? `, on ${s.project}` : ""}.`,
      body:
        blockers.length > 0 ? (
          <ul aria-label="Open blockers" className="max-h-56 space-y-1.5 overflow-auto rounded-lg border border-danger/30 bg-danger-soft/60 p-3 text-xs">
            {blockers.map((b, i) => (
              <li key={i} className="flex items-start gap-2">
                <span className="mt-[0.4rem] size-1.5 shrink-0 rounded-full bg-danger" />
                <span className="min-w-0">
                  <span className="font-medium text-danger-fg">{label(b.provider)}: </span>
                  {b.text}
                </span>
              </li>
            ))}
          </ul>
        ) : undefined,
      confirmLabel: blockers.length > 0 ? "Approve anyway" : "Approve and add card",
      danger: blockers.length > 0,
      run: async () => {
        try {
          const card = await approveBrief(s.id, undefined, blockers.length > 0)
          toast.success("Card added to Ready", {
            description: card.title,
            action: { label: "Open card", onClick: () => open("boards", [DEFAULT_BOARD, card.id]) },
          })
          await refetch()
        } catch (e) {
          toast.error("Could not approve the brief", { description: errorMessage(e) })
        }
      },
    })

  return (
    <Card className="overflow-hidden">
      <div className="relative border-b px-6 pb-4 pt-5">
        <div aria-hidden className="pointer-events-none absolute inset-0 bg-gradient-to-b from-brand-soft to-transparent opacity-60" />
        <div className="relative flex items-start justify-between gap-4">
          <div className="min-w-0">
            <p className="flex items-center gap-1.5 text-xs font-medium text-subtle-foreground">
              <FileText className="size-3.5" />
              Brief
              {s.brief_path && <span className="truncate font-mono font-normal">· {s.brief_path}</span>}
            </p>
            <h2 className="mt-1 text-xl font-semibold tracking-[-0.015em]">{b.title || s.title}</h2>
          </div>
          {approved ? (
            <StatusPill tone={s.approved_with_blockers ? "warning" : "success"}>{s.approved_with_blockers ? "Approved with blockers" : "Approved"}</StatusPill>
          ) : (
            <StatusPill tone="warning">Draft · waiting for you</StatusPill>
          )}
        </div>
      </div>
      {s.warnings.length > 0 && (
        <div className="border-b bg-warning-soft/60 px-6 py-2.5 text-xs text-warning-fg">
          <span className="font-medium">Format check: </span>
          {s.warnings.join("; ")}
        </div>
      )}
      <div className="px-6 py-5">
        <BriefBody b={b} />
      </div>
      <div className="flex flex-wrap items-center gap-2 border-t bg-muted/25 px-6 py-3">
        <CostLine s={s} />
        <div className="ml-auto flex flex-wrap items-center gap-2">
          {s.brief_path && (
            <Button variant="ghost" size="sm" onClick={() => open("memory", s.brief_path!.split("/"))}>
              <NotebookPen /> Open in Memory
            </Button>
          )}
          {approved ? (
            <>
              <span className="flex items-center gap-1.5 text-xs text-success-fg">
                <CheckCircle2 className="size-3.5" /> On the work board · {s.card?.column ?? "Ready"}
              </span>
              <Button variant="secondary" size="sm" onClick={() => open("boards", s.card ? [DEFAULT_BOARD, s.card.id] : [DEFAULT_BOARD])}>
                <KanbanSquare /> Open card
              </Button>
              {s.card && (
                <Button size="sm" onClick={() => open("work", ["new", s.card!.id])}>
                  <Play /> Start work
                </Button>
              )}
            </>
          ) : (
            <>
              <Button variant="secondary" size="sm" onClick={() => setAsking(true)}>
                <RotateCcw /> Ask again
              </Button>
              <Button size="sm" onClick={approve}>
                <Check /> Approve
              </Button>
            </>
          )}
        </div>
      </div>
      <ConfirmDialog request={confirm} onClose={() => setConfirm(null)} />
      <AskAgainDialog key={String(asking)} open={asking} onClose={() => setAsking(false)} s={s} onStarted={onChanged} />
    </Card>
  )
}

function AskAgainDialog({ open, onClose, s, onStarted }: { open: boolean; onClose: () => void; s: CouncilSession; onStarted: (s: CouncilSession) => void }) {
  const [notes, setNotes] = useState("")
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState("")
  const go = async () => {
    setBusy(true)
    setErr("")
    try {
      onStarted(await askAgain(s.id, notes))
      onClose()
    } catch (e) {
      setErr(errorMessage(e))
      setBusy(false)
    }
  }
  return (
    <Dialog
      open={open}
      onClose={busy ? () => {} : onClose}
      title="Ask the council again"
      description={`The critics read the brief with your notes, then ${label(s.proposer)} revises it. Your notes win over the critics.`}
    >
      <textarea
        autoFocus
        value={notes}
        onChange={(e) => setNotes(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter" && (e.metaKey || e.ctrlKey) && notes.trim()) void go()
        }}
        rows={5}
        placeholder="What should change? e.g. keep it to the daemon only; the desktop app is out of scope."
        className="w-full resize-y rounded-lg border bg-background/60 px-3 py-2 text-sm leading-relaxed outline-none placeholder:text-subtle-foreground focus-visible:border-ring"
      />
      {err && <p className="mt-2 text-xs text-danger-fg">{err}</p>}
      <div className="mt-4 flex items-center justify-between gap-2">
        <span className="text-xs text-subtle-foreground">up to {Math.max(s.critics.length, 1) + 1} calls</span>
        <div className="flex gap-2">
          <Button variant="secondary" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button onClick={() => void go()} disabled={busy || !notes.trim()}>
            {busy ? <Loader2 className="animate-spin" /> : <RotateCcw />} Run another round
          </Button>
        </div>
      </div>
    </Dialog>
  )
}

/* aside */

function Aside({ s }: { s: CouncilSession }) {
  const { open } = useApp()
  const now = useNow(5000)
  const t = totals(s.usage)
  const byProvider = new Map<string, { calls: number; input: number; output: number; cost: number }>()
  for (const u of s.usage) {
    const r = byProvider.get(u.provider) ?? { calls: 0, input: 0, output: 0, cost: 0 }
    r.calls++
    r.input += (u.input_tokens ?? 0) + (u.cache_read_tokens ?? 0) + (u.cache_write_tokens ?? 0)
    r.output += u.output_tokens ?? 0
    r.cost += u.cost_usd ?? 0
    byProvider.set(u.provider, r)
  }
  const log = [...s.log].reverse().slice(0, 10)
  return (
    <aside className="space-y-4 @4xl:sticky @4xl:top-0 @4xl:self-start">
      <Card className="p-4">
        <h2 className="mb-2.5 text-xs font-semibold uppercase tracking-wider text-subtle-foreground">Council</h2>
        <dl className="space-y-2 text-sm">
          <Row k="Proposer">
            <span className="flex items-center gap-1.5">
              <ProviderMark provider={s.proposer} className="size-3.5" />
              {label(s.proposer)}
            </span>
          </Row>
          <Row k={s.mode === "self-critique" ? "Critic" : "Critics"}>
            {s.mode === "self-critique" && s.critics.length === 0 ? (
              <span className="text-muted-foreground">self-critique</span>
            ) : (
              <span className="flex flex-wrap items-center justify-end gap-x-2.5 gap-y-1">
                {s.critics.map((c) => (
                  <span key={c} className="flex items-center gap-1.5">
                    <ProviderMark provider={c} className="size-3.5" />
                    {label(c)}
                  </span>
                ))}
              </span>
            )}
          </Row>
          <Row k="Project">{s.project ? <span className="font-mono text-xs">{s.project}</span> : <span className="text-muted-foreground">none</span>}</Row>
          <Row k="Rounds">
            {s.rounds.length > s.max_rounds ? (
              <span title={`The automatic loop stops at ${s.max_rounds}; each Ask again adds a round.`}>{roundsLabel({ rounds: s.rounds.length, max_rounds: s.max_rounds })}</span>
            ) : (
              `${s.rounds.length} of ${s.max_rounds}`
            )}
          </Row>
          <Row k="Started">
            <span title={absoluteTime(s.created)}>{relativeTime(s.created, now)}</span>
          </Row>
          {s.brief_path && (
            <Row k="Idea">
              <button onClick={() => open("ideas", [s.id])} className="inline-flex items-center gap-1 text-brand-fg hover:underline" title="The brief, its card, the agent sessions and the PRs in one place">
                <Lightbulb className="size-3.5" /> Follow it
              </button>
            </Row>
          )}
        </dl>
      </Card>

      <Card className="p-4">
        <h2 className="mb-2.5 flex items-baseline justify-between text-xs font-semibold uppercase tracking-wider text-subtle-foreground">
          Usage
          <span className="font-mono text-sm normal-case tracking-normal text-foreground">
            {usd(t.cost)}
            {t.uncosted.length > 0 && <span className="text-subtle-foreground">+</span>}
          </span>
        </h2>
        {byProvider.size === 0 ? (
          <p className="text-xs text-muted-foreground">No calls finished yet.</p>
        ) : (
          <ul className="space-y-1.5">
            {[...byProvider].map(([p, r]) => (
              <li key={p} className="flex items-center gap-2 text-xs">
                <ProviderMark provider={p} className="size-3.5 text-muted-foreground" />
                <span className="flex-1">
                  {label(p)} <span className="text-subtle-foreground">× {r.calls}</span>
                </span>
                {r.input + r.output === 0 ? (
                  <span className="text-2xs text-subtle-foreground">no usage reported</span>
                ) : (
                  <>
                    <span className="font-mono text-2xs tabular-nums text-subtle-foreground">
                      {tokens(r.input)}/{tokens(r.output)}
                    </span>
                    <span className="w-12 text-right font-mono text-2xs tabular-nums">{r.cost ? usd(r.cost) : "n/a"}</span>
                  </>
                )}
              </li>
            ))}
          </ul>
        )}
        {t.uncosted.length > 0 && <p className="mt-2 text-2xs text-subtle-foreground">n/a: {joinNames(t.uncosted)} did not report a cost.</p>}
      </Card>

      <Card className="p-4">
        <h2 className="mb-2.5 text-xs font-semibold uppercase tracking-wider text-subtle-foreground">Activity</h2>
        <ol className="space-y-2">
          {log.map((e, i) => (
            <li key={`${e.time}-${i}`} className="flex gap-2 text-xs">
              <span className={cn("mt-1.5 size-1.5 shrink-0 rounded-full", e.kind === "error" ? "bg-danger" : e.kind === "skipped" ? "bg-warning" : e.kind === "done" ? "bg-success" : e.kind === "critique" ? "bg-brand" : "bg-border-strong")} />
              <span className="min-w-0 flex-1 text-muted-foreground">{named(e.text)}</span>
              <time className="shrink-0 tabular-nums text-subtle-foreground" title={absoluteTime(e.time)}>
                {relativeTime(e.time, now)}
              </time>
            </li>
          ))}
        </ol>
      </Card>
    </aside>
  )
}

function Row({ k, children }: { k: string; children: ReactNode }) {
  return (
    <div className="flex items-start justify-between gap-3">
      <dt className="text-xs text-muted-foreground">{k}</dt>
      <dd className="text-right text-xs">{children}</dd>
    </div>
  )
}
