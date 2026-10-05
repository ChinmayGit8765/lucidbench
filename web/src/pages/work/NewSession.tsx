import { useEffect, useMemo, useState, type ReactNode } from "react"
import {
  ArrowLeft,
  Bot,
  CircleDashed,
  FileDiff,
  FolderGit2,
  GitBranch,
  KanbanSquare,
  Lock,
  Play,
  ShieldCheck,
  UserCog,
} from "lucide-react"
import { toast } from "sonner"

import { CopyCommand } from "@/components/CopyCommand"
import { ProviderTile, providerInfo } from "@/components/ProviderMark"
import { PageHeader } from "@/components/Shell"
import { StatusPill } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card } from "@/components/ui/card"
import { ConfirmDialog, type ConfirmRequest } from "@/components/ui/confirm"
import { Skeleton } from "@/components/ui/states"
import { errorMessage, usePoll } from "@/lib/api"
import { useApp } from "@/lib/app"
import { PROJECTS_POLL_MS, typeInfo, type ProjectList } from "@/lib/projects"
import { cn, isMac } from "@/lib/utils"
import {
  branchPreview,
  defaultHarness,
  homeHint,
  startSession,
  WORK_PROVIDERS,
  type Harness,
  type WorkProject,
  type WorkProvider,
} from "@/lib/work"
import type { Account } from "@/pages/Accounts"

interface BoardCard {
  id: string
  title: string
  column: string
  project?: string
  memory?: string
}

function Section({ n, title, hint, children }: { n: number; title: string; hint?: ReactNode; children: ReactNode }) {
  return (
    <section className="border-b px-5 py-5 last:border-b-0">
      <div className="mb-3 flex items-baseline gap-2.5">
        <span className="flex size-5 shrink-0 items-center justify-center rounded-full bg-brand-soft text-2xs font-semibold tabular-nums text-brand-fg">{n}</span>
        <h2 className="text-sm font-semibold">{title}</h2>
        {hint && <span className="text-xs text-subtle-foreground">{hint}</span>}
      </div>
      {children}
    </section>
  )
}

const HARNESS: Record<Harness, { label: string; icon: typeof UserCog; blurb: string }> = {
  mine: {
    label: "Your harness",
    icon: UserCog,
    blurb:
      "The CLI loads your own settings, hooks, skills, MCP servers and instruction files, exactly as it runs in your terminal. Your hooks still apply, so a hook that guards where files may be written can block the agent here.",
  },
  clean: {
    label: "Clean",
    icon: ShieldCheck,
    blurb:
      "No user settings, hooks or MCP servers: Claude runs with --safe-mode --strict-mcp-config, Codex with --ignore-user-config --ignore-rules. Grok has no such switch, so it runs the same either way.",
  },
}

export function NewSession({ card: initialCard }: { card?: string }) {
  const { open, navigate } = useApp()
  const projects = usePoll<ProjectList>("/api/projects", PROJECTS_POLL_MS)
  const accounts = usePoll<Account[]>("/api/accounts", 30000)
  const tools = usePoll<{ clis: Record<string, boolean> }>("/api/host/tools", 60000)
  const board = usePoll<{ cards: BoardCard[] }>("/api/boards/work", 30000)

  const [provider, setProvider] = useState<WorkProvider>("claude")
  const [profile, setProfile] = useState("")
  const [harness, setHarness] = useState<Harness>("mine")
  const [harnessTouched, setHarnessTouched] = useState(false)
  const [project, setProject] = useState("")
  const [cardId, setCardId] = useState(initialCard ?? "")
  const [prompt, setPrompt] = useState("")
  const [confirm, setConfirm] = useState<ConfirmRequest | null>(null)

  const all = (projects.data?.projects ?? []) as WorkProject[]
  const usable = all.filter((p) => p.local_path && p.visibility !== "confidential")
  const unusable = all.filter((p) => !p.local_path || p.visibility === "confidential")
  const cards = (board.data?.cards ?? []).filter((c) => c.column !== "Done")
  const card = cards.find((c) => c.id === cardId)
  const chosen = usable.find((p) => p.id === project)

  // A card brings its project along.
  useEffect(() => {
    if (card?.project && usable.some((p) => p.id === card.project)) setProject(card.project)
  }, [card?.id, projects.data]) // not on every edit of usable, so a later pick sticks
  // One usable project: pick it.
  useEffect(() => {
    if (!project && usable.length === 1) setProject(usable[0].id)
  }, [project, usable])

  const hostAccounts = (p: string) => (accounts.data ?? []).filter((a) => a.provider === p && a.location === "host")
  const signedIn = (p: string) => hostAccounts(p).some((a) => a.status === "logged_in")
  const installed = (p: string) => tools.data?.clis?.[p] ?? true
  const profiles = hostAccounts(provider)

  const pickProvider = (p: WorkProvider) => {
    setProvider(p)
    setProfile("")
    if (!harnessTouched) setHarness(defaultHarness(p))
  }

  const title = card?.title ?? prompt.split("\n").find((l) => l.trim())?.trim() ?? ""
  const missing = !project ? "Pick a project" : !card && !prompt.trim() ? "Write a prompt or pick a card" : !installed(provider) ? `${providerInfo(provider)?.label} is not installed` : null
  const label = providerInfo(provider)?.label ?? provider

  const start = () => {
    if (missing || !chosen) return
    setConfirm({
      title: `Start ${label} on ${chosen.name}?`,
      description: `Lucidbench makes a new worktree next to ${homeHint(chosen.local_path ?? "")} and runs ${provider} there with your own account, so the run counts against your plan. Nothing is pushed until you open a PR.`,
      confirmLabel: "Start session",
      run: async () => {
        try {
          const s = await startSession({
            provider,
            profile: profile || undefined,
            harness,
            project,
            card: card?.id,
            prompt: prompt.trim() || undefined,
          })
          navigate(`/work/${s.id}`)
        } catch (e) {
          toast.error("Could not start the session", { description: errorMessage(e) })
        }
      },
    })
  }

  const loading = projects.loading && !projects.data
  const repoName = useMemo(() => (chosen?.local_path ?? "").split(/[\\/]/).filter(Boolean).pop() ?? "repo", [chosen])

  return (
    <div className="space-y-6">
      <PageHeader
        icon={<Play />}
        eyebrow={
          <button onClick={() => open("work")} className="inline-flex items-center gap-1 hover:text-foreground">
            <ArrowLeft className="size-3" /> Work
          </button>
        }
        title="New session"
        description="Pick a project and an agent, then say what to do. The agent gets its own worktree and branch."
      />

      <div className="grid items-start gap-4 @4xl:grid-cols-[minmax(0,1fr)_20rem]">
        <Card className="overflow-hidden">
          <Section n={1} title="Task" hint={cards.length > 0 ? "a card's brief, your own words, or both" : undefined}>
            {cards.length > 0 && (
              <label className="mb-3 flex items-center gap-2 rounded-lg border bg-background/50 px-3 py-2 focus-within:ring-2 focus-within:ring-ring">
                <KanbanSquare className="size-4 shrink-0 text-subtle-foreground" />
                <select
                  aria-label="Card"
                  value={cardId}
                  onChange={(e) => setCardId(e.target.value)}
                  className="min-w-0 flex-1 bg-transparent text-sm outline-none"
                >
                  <option value="">No card: just a prompt</option>
                  {["Ready", "Inbox", "In progress", "Review"].map((col) => {
                    const cs = cards.filter((c) => c.column === col)
                    return cs.length ? (
                      <optgroup key={col} label={col}>
                        {cs.map((c) => (
                          <option key={c.id} value={c.id}>
                            {c.title}
                            {c.project ? ` · ${c.project}` : ""}
                          </option>
                        ))}
                      </optgroup>
                    ) : null
                  })}
                </select>
              </label>
            )}
            {card && (
              <p className="mb-3 text-xs text-muted-foreground">
                The agent gets {card.memory ? <span className="font-mono">{card.memory}</span> : "the card title"} as its brief. The card moves to In progress, then Review.
              </p>
            )}
            <textarea
              aria-label="Prompt"
              value={prompt}
              onChange={(e) => setPrompt(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
                  e.preventDefault()
                  start()
                }
              }}
              rows={card ? 3 : 6}
              placeholder={card ? "Anything to add to the brief (optional)" : "What should the agent do? For example: add a README line saying hello."}
              className="w-full resize-y rounded-lg border bg-background/50 px-3 py-2.5 text-sm leading-6 outline-none placeholder:text-subtle-foreground focus-visible:ring-2 focus-visible:ring-ring"
            />
            <p className="mt-1.5 text-2xs text-subtle-foreground">
              <kbd className="rounded border bg-muted px-1 font-sans">{isMac() ? "⌘" : "Ctrl"}</kbd> + <kbd className="rounded border bg-muted px-1 font-sans">Enter</kbd> to start
            </p>
          </Section>

          <Section n={2} title="Project" hint="only projects with a local checkout">
            {loading ? (
              <div className="grid gap-2 @2xl:grid-cols-2">
                <Skeleton className="h-16" />
                <Skeleton className="h-16" />
              </div>
            ) : usable.length === 0 ? (
              <div className="rounded-lg border border-dashed p-4 text-sm text-muted-foreground">
                No project has a <span className="font-mono">local_path</span> yet. Add one to a project in{" "}
                <span className="font-mono">{projects.data?.path_hint ?? "projects.yaml"}</span>:
                <CopyCommand className="mt-2" command="local_path: <the project's git checkout>" />
              </div>
            ) : (
              <div role="radiogroup" aria-label="Project" className="grid gap-2 @2xl:grid-cols-2">
                {usable.map((p) => {
                  const on = p.id === project
                  const T = typeInfo(p.type).icon
                  return (
                    <button
                      key={p.id}
                      role="radio"
                      aria-checked={on}
                      onClick={() => setProject(p.id)}
                      className={cn(
                        "flex items-start gap-3 rounded-lg border px-3 py-2.5 text-left outline-none transition-[border-color,background-color] focus-visible:ring-2 focus-visible:ring-ring",
                        on ? "border-brand/60 bg-brand-soft/60" : "bg-background/40 hover:border-border-strong hover:bg-accent/40",
                      )}
                    >
                      <span className={cn("mt-0.5 flex size-7 shrink-0 items-center justify-center rounded-md border", on ? "border-brand/40 text-brand-fg" : "text-subtle-foreground")}>
                        <T className="size-3.5" />
                      </span>
                      <span className="min-w-0 flex-1">
                        <span className="block truncate text-sm font-medium">{p.name}</span>
                        <span className="block truncate font-mono text-2xs text-muted-foreground" title={homeHint(p.local_path ?? "")}>
                          {homeHint(p.local_path ?? "")}
                        </span>
                      </span>
                    </button>
                  )
                })}
              </div>
            )}
            {unusable.length > 0 && (
              <p className="mt-2.5 flex flex-wrap items-center gap-x-3 gap-y-1 text-2xs text-subtle-foreground">
                <span>Not available:</span>
                {unusable.map((p) => (
                  <span key={p.id} className="inline-flex items-center gap-1" title={p.visibility === "confidential" ? "Confidential projects never reach a provider" : "Add local_path to projects.yaml"}>
                    {p.visibility === "confidential" ? <Lock className="size-3" /> : <CircleDashed className="size-3" />}
                    {p.name}
                    <span className="opacity-70">({p.visibility === "confidential" ? "confidential" : "no local_path"})</span>
                  </span>
                ))}
              </p>
            )}
          </Section>

          <Section n={3} title="Agent" hint="your own signed-in CLI">
            <div role="radiogroup" aria-label="Provider" className="grid grid-cols-3 gap-2">
              {WORK_PROVIDERS.map((p) => {
                const on = p === provider
                const inst = installed(p)
                const ok = signedIn(p)
                return (
                  <button
                    key={p}
                    role="radio"
                    aria-checked={on}
                    disabled={!inst}
                    onClick={() => pickProvider(p)}
                    className={cn(
                      "flex items-center gap-2.5 rounded-lg border px-3 py-2.5 text-left outline-none transition-[border-color,background-color] focus-visible:ring-2 focus-visible:ring-ring disabled:cursor-not-allowed disabled:opacity-50",
                      on ? "border-brand/60 bg-brand-soft/60" : "bg-background/40 hover:border-border-strong hover:bg-accent/40",
                    )}
                  >
                    <ProviderTile provider={p} size="md" muted={!inst} />
                    <span className="min-w-0">
                      <span className="block text-sm font-medium">{providerInfo(p)?.label}</span>
                      <span className={cn("block truncate text-2xs", !inst ? "text-subtle-foreground" : ok ? "text-success-fg" : "text-warning-fg")}>
                        {!inst ? "not installed" : accounts.data ? (ok ? "signed in" : "sign-in unknown") : "checking…"}
                      </span>
                    </span>
                  </button>
                )
              })}
            </div>
            {profiles.length > 1 && (
              <div className="mt-3 flex flex-wrap items-center gap-1.5">
                <span className="mr-1 text-xs text-muted-foreground">Account</span>
                {profiles.map((a) => {
                  const on = profile === a.name || (!profile && a === profiles[0])
                  return (
                    <button
                      key={a.name}
                      onClick={() => setProfile(a === profiles[0] ? "" : a.name)}
                      className={cn(
                        "inline-flex h-6 items-center gap-1.5 rounded-full border px-2.5 font-mono text-2xs transition-colors",
                        on ? "border-brand/60 bg-brand-soft text-brand-fg" : "text-muted-foreground hover:bg-accent",
                      )}
                    >
                      <span className={cn("size-1.5 rounded-full", a.status === "logged_in" ? "bg-success" : "bg-warning")} />
                      {a.name}
                    </button>
                  )
                })}
              </div>
            )}

            <div className="mt-4">
              <div className="mb-2 flex items-center gap-2">
                <span className="text-xs font-medium">Harness</span>
                <span className="text-2xs text-subtle-foreground">default for {label}: {defaultHarness(provider) === "mine" ? "your harness" : "clean"}</span>
              </div>
              <div role="radiogroup" aria-label="Harness" className="inline-flex rounded-lg border bg-muted/50 p-0.5">
                {(Object.keys(HARNESS) as Harness[]).map((h) => {
                  const I = HARNESS[h].icon
                  const on = h === harness
                  return (
                    <button
                      key={h}
                      role="radio"
                      aria-checked={on}
                      onClick={() => {
                        setHarness(h)
                        setHarnessTouched(true)
                      }}
                      className={cn(
                        "inline-flex h-7 items-center gap-1.5 rounded-md px-3 text-xs font-medium transition-colors",
                        on ? "bg-elevated text-foreground shadow-card" : "text-muted-foreground hover:text-foreground",
                      )}
                    >
                      <I className="size-3.5" />
                      {HARNESS[h].label}
                    </button>
                  )
                })}
              </div>
              <p className="mt-2 max-w-xl text-xs leading-5 text-muted-foreground">{HARNESS[harness].blurb}</p>
            </div>
          </Section>
        </Card>

        <div className="space-y-3 @4xl:sticky @4xl:top-4">
          <Card className="p-4">
            <h2 className="mb-3 text-sm font-semibold">What happens</h2>
            <ol className="space-y-3 text-xs">
              <Happens icon={FolderGit2} title="A fresh worktree">
                <span className="font-mono">{repoName}-lucid-&lt;id&gt;</span> next to your checkout, on{" "}
                <span className="break-all font-mono">{branchPreview(title)}</span> from the default branch.
              </Happens>
              <Happens icon={Bot} title={`${label} works there`}>
                with {harness === "mine" ? "your own harness" : "a clean harness"}, editing files and committing. Your checkout is not touched.
              </Happens>
              <Happens icon={FileDiff} title="You review the diff">
                Open PR pushes the branch and opens a draft. Nothing merges by itself.
              </Happens>
            </ol>
            <Button className="mt-4 w-full" size="lg" disabled={!!missing} onClick={start}>
              <Play /> Start session
            </Button>
            <p className="mt-2 min-h-4 text-center text-2xs text-subtle-foreground">
              {missing ?? (
                <>
                  <StatusPill tone="neutral" className="mr-1 h-4">
                    {harness === "mine" ? "your harness" : "clean"}
                  </StatusPill>
                  {chosen?.name}
                </>
              )}
            </p>
          </Card>
          <p className="flex items-start gap-1.5 px-1 text-2xs leading-4 text-subtle-foreground">
            <GitBranch className="mt-px size-3 shrink-0" />
            The agent is told to work only in its worktree, commit as it goes, and never push.
          </p>
        </div>
      </div>

      <ConfirmDialog request={confirm} onClose={() => setConfirm(null)} />
    </div>
  )
}

function Happens({ icon: Icon, title, children }: { icon: typeof Bot; title: string; children: ReactNode }) {
  return (
    <li className="flex gap-2.5">
      <span className="mt-0.5 flex size-6 shrink-0 items-center justify-center rounded-md border bg-background/60 text-subtle-foreground">
        <Icon className="size-3.5" />
      </span>
      <span className="min-w-0">
        <span className="block font-medium text-foreground">{title}</span>
        <span className="text-muted-foreground">{children}</span>
      </span>
    </li>
  )
}
