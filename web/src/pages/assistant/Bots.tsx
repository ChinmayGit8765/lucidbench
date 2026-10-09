import { useEffect, useState } from "react"
import { Bot as BotIcon, Download, MessageCircle, Pencil, Plus, SquareTerminal, Trash2, Vote, Zap } from "lucide-react"
import { toast } from "sonner"

import { BotAvatar } from "@/components/assistant/BotAvatar"
import { ProviderMark } from "@/components/ProviderMark"
import { SPRITE_SLOTS } from "@/components/StateSprite"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card } from "@/components/ui/card"
import { ConfirmDialog, type ConfirmRequest } from "@/components/ui/confirm"
import { Dialog, Sheet } from "@/components/ui/dialog"
import { EmptyState, ErrorState, Skeleton } from "@/components/ui/states"
import { errorMessage, getJSON, sendJSON, usePoll } from "@/lib/api"
import { useApp } from "@/lib/app"
import {
  ACTION_LABEL,
  ALL_ACTIONS,
  ASSISTANT_PROVIDERS,
  BOTS_PATH,
  deleteBot,
  IMPORT_PATH,
  importBot,
  saveBot,
  type ActionKind,
  type Bot,
  type ImportList,
} from "@/lib/assistant"
import type { ProjectList } from "@/lib/projects"
import { cn } from "@/lib/utils"
import { startSession } from "@/lib/work"

const slug = (s: string) =>
  s
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, 36) || "bot"

const blank = (): Bot => ({ id: "", name: "", provider: "claude", model: "", persona: "", allowed_actions: [...ALL_ACTIONS], avatar: "" })

/** The Bots tab: a gallery of the user's bots, make and edit them, import agents the CLIs already have, and task one. */
export function Bots() {
  const { open } = useApp()
  const bots = usePoll<Bot[]>(BOTS_PATH, 30000)
  const [editing, setEditing] = useState<{ bot: Bot; isNew: boolean } | null>(null)
  const [tasking, setTasking] = useState<Bot | null>(null)
  const [importing, setImporting] = useState(false)
  const [confirm, setConfirm] = useState<ConfirmRequest | null>(null)
  const list = bots.data ?? []

  return (
    <div className="space-y-4" data-testid="assistant-bots">
      <div className="flex flex-wrap items-center gap-2">
        <p className="flex-1 text-sm text-muted-foreground">A bot is a saved agent: a provider and model, a persona, and the actions it may propose. Talk to it, or task it with a Work session or a council seat.</p>
        <Button variant="secondary" onClick={() => setImporting(true)} data-testid="bots-import">
          <Download /> Import from your CLIs
        </Button>
        <Button onClick={() => setEditing({ bot: blank(), isNew: true })} data-testid="bots-new">
          <Plus /> New bot
        </Button>
      </div>

      {bots.error && !bots.data && <ErrorState title="Could not load the bots" message={bots.error.message} onRetry={bots.refresh} />}
      {bots.loading && !bots.data ? (
        <Skeleton className="h-40 rounded-xl" />
      ) : list.length === 0 ? (
        <Card>
          <EmptyState
            icon={<BotIcon />}
            title="No bots yet"
            description="Make one here, or import the agents you already made in Claude Code, Codex or the Grok CLI."
          >
            <Button onClick={() => setEditing({ bot: blank(), isNew: true })}>
              <Plus /> New bot
            </Button>
          </EmptyState>
        </Card>
      ) : (
        <div className="grid gap-3 @2xl:grid-cols-2 @5xl:grid-cols-3">
          {list.map((b) => (
            <Card key={b.id} className="flex flex-col p-4" data-testid="bot-card">
              <div className="flex items-start gap-3">
                <BotAvatar bot={b} />
                <div className="min-w-0 flex-1">
                  <div className="truncate font-semibold">{b.name}</div>
                  <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
                    <ProviderMark provider={b.provider} className="size-3.5" /> {b.provider}
                    {b.model ? ` · ${b.model}` : ""}
                    {b.source && <Badge className="ml-1">{b.source}</Badge>}
                  </div>
                </div>
              </div>
              <p className="mt-2 line-clamp-3 flex-1 text-xs text-muted-foreground">{b.persona || "No persona"}</p>
              <div className="mt-2 text-2xs text-subtle-foreground">
                {b.allowed_actions.length === ALL_ACTIONS.length ? "May propose any action" : b.allowed_actions.length === 0 ? "Proposes nothing" : `May propose: ${b.allowed_actions.map((a) => ACTION_LABEL[a]).join(", ")}`}
              </div>
              <div className="mt-3 flex flex-wrap gap-1.5">
                <Button size="sm" onClick={() => setTasking(b)} data-testid="bot-task">
                  <Zap /> Task this bot
                </Button>
                <Button size="sm" variant="secondary" onClick={() => open("assistant", ["bot", b.id])}>
                  <MessageCircle /> Chat
                </Button>
                <Button size="sm" variant="ghost" aria-label={`Edit ${b.name}`} onClick={() => setEditing({ bot: { ...b }, isNew: false })}>
                  <Pencil />
                </Button>
                <Button
                  size="sm"
                  variant="ghost"
                  aria-label={`Delete ${b.name}`}
                  onClick={() =>
                    setConfirm({
                      title: `Delete ${b.name}?`,
                      description: "Its file is removed from the bots folder. Conversations with it are kept.",
                      confirmLabel: "Delete bot",
                      danger: true,
                      run: async () => {
                        try {
                          await deleteBot(b.id)
                          bots.refresh()
                        } catch (e) {
                          toast.error("Could not delete the bot", { description: errorMessage(e) })
                        }
                      },
                    })
                  }
                >
                  <Trash2 />
                </Button>
              </div>
            </Card>
          ))}
        </div>
      )}

      <BotEditor
        state={editing}
        taken={list.map((b) => b.id)}
        onClose={() => setEditing(null)}
        onSaved={() => {
          setEditing(null)
          bots.refresh()
        }}
      />
      <ImportSheet open={importing} onClose={() => setImporting(false)} onImported={() => bots.refresh()} />
      <TaskDialog bot={tasking} onClose={() => setTasking(null)} />
      <ConfirmDialog request={confirm} onClose={() => setConfirm(null)} />
    </div>
  )
}

function BotEditor({ state, taken, onClose, onSaved }: { state: { bot: Bot; isNew: boolean } | null; taken: string[]; onClose: () => void; onSaved: () => void }) {
  const [b, setB] = useState<Bot>(blank())
  const [saving, setSaving] = useState(false)
  useEffect(() => {
    if (state) setB(state.bot)
  }, [state])
  if (!state) return null
  const set = (patch: Partial<Bot>) => setB((x) => ({ ...x, ...patch }))
  const id = state.isNew ? (() => {
    let base = slug(b.name)
    let n = 2
    const first = base
    while (taken.includes(base)) base = `${first}-${n++}`
    return base
  })() : b.id
  const sprite = b.avatar?.startsWith("sprite:") ? b.avatar.slice(7) : ""
  const save = async () => {
    setSaving(true)
    try {
      await saveBot({ ...b, id, model: b.model?.trim() || undefined, profile: b.profile?.trim() || undefined, avatar: b.avatar?.trim() || undefined })
      toast.success(`${b.name} saved`)
      onSaved()
    } catch (e) {
      toast.error("Could not save the bot", { description: errorMessage(e) })
    } finally {
      setSaving(false)
    }
  }
  const toggle = (a: ActionKind) => set({ allowed_actions: b.allowed_actions.includes(a) ? b.allowed_actions.filter((x) => x !== a) : [...b.allowed_actions, a] })
  return (
    <Sheet open onClose={onClose} title={state.isNew ? "New bot" : `Edit ${state.bot.name}`} description="Saved in your data dir's bots folder.">
      <div className="space-y-4 p-5">
        <label className="block space-y-1">
          <span className="text-sm font-medium">Name</span>
          <input aria-label="Bot name" value={b.name} onChange={(e) => set({ name: e.target.value })} className="h-9 w-full rounded-md border bg-background/50 px-2.5 text-sm" />
          <span className="text-2xs text-subtle-foreground">id: {id}</span>
        </label>
        <div className="grid grid-cols-2 gap-3">
          <label className="block space-y-1">
            <span className="text-sm font-medium">Provider</span>
            <select aria-label="Bot provider" value={b.provider} onChange={(e) => set({ provider: e.target.value as Bot["provider"] })} className="h-9 w-full rounded-md border bg-background/50 px-2 text-sm">
              {ASSISTANT_PROVIDERS.map((p) => (
                <option key={p} value={p}>
                  {p}
                </option>
              ))}
            </select>
          </label>
          <label className="block space-y-1">
            <span className="text-sm font-medium">Model</span>
            <input aria-label="Bot model" value={b.model ?? ""} onChange={(e) => set({ model: e.target.value })} placeholder="the CLI's default" className="h-9 w-full rounded-md border bg-background/50 px-2.5 text-sm" />
          </label>
        </div>
        <label className="block space-y-1">
          <span className="text-sm font-medium">Persona</span>
          <textarea aria-label="Bot persona" value={b.persona} onChange={(e) => set({ persona: e.target.value })} rows={6} placeholder="Who the bot is and how it answers. This becomes part of its system prompt." className="w-full rounded-md border bg-background/50 p-2.5 text-sm" />
        </label>
        <div className="space-y-1.5">
          <span className="text-sm font-medium">May propose</span>
          <div className="grid grid-cols-2 gap-1.5">
            {ALL_ACTIONS.map((a) => (
              <label key={a} className="flex items-center gap-2 text-sm">
                <input type="checkbox" checked={b.allowed_actions.includes(a)} onChange={() => toggle(a)} /> {ACTION_LABEL[a]}
              </label>
            ))}
          </div>
        </div>
        <div className="grid grid-cols-2 gap-3">
          <label className="block space-y-1">
            <span className="text-sm font-medium">Emoji</span>
            <input aria-label="Bot emoji" value={sprite ? "" : (b.avatar ?? "")} onChange={(e) => set({ avatar: e.target.value })} placeholder="e.g. an emoji" className="h-9 w-full rounded-md border bg-background/50 px-2.5 text-sm" />
          </label>
          <label className="block space-y-1">
            <span className="text-sm font-medium">or a sprite</span>
            <select aria-label="Bot sprite" value={sprite} onChange={(e) => set({ avatar: e.target.value ? `sprite:${e.target.value}` : "" })} className="h-9 w-full rounded-md border bg-background/50 px-2 text-sm">
              <option value="">None</option>
              {SPRITE_SLOTS.map((s) => (
                <option key={s.slot} value={s.slot}>
                  {s.label}
                </option>
              ))}
            </select>
          </label>
        </div>
        <div className="flex justify-end gap-2 pt-2">
          <Button variant="secondary" onClick={onClose}>
            Cancel
          </Button>
          <Button onClick={() => void save()} disabled={saving || !b.name.trim()} data-testid="bot-save">
            {saving ? "Saving" : "Save bot"}
          </Button>
        </div>
      </div>
    </Sheet>
  )
}

function ImportSheet({ open, onClose, onImported }: { open: boolean; onClose: () => void; onImported: () => void }) {
  const [data, setData] = useState<ImportList | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [done, setDone] = useState<Record<string, boolean>>({})
  useEffect(() => {
    if (!open) return
    setError(null)
    getJSON<ImportList>(IMPORT_PATH)
      .then(setData)
      .catch((e) => setError(errorMessage(e)))
  }, [open])
  if (!open) return null
  const take = async (key: string, persona: boolean) => {
    try {
      const b = await importBot(key, persona)
      setDone((d) => ({ ...d, [key]: true }))
      toast.success(`Imported ${b.name}`)
      onImported()
    } catch (e) {
      toast.error("Could not import", { description: errorMessage(e) })
    }
  }
  return (
    <Sheet open onClose={onClose} title="Import from your CLIs" description="Read-only: the agents folders of Claude Code, Codex and the Grok CLI. Only each agent's name, description and model are read until you import one with its instructions. No CLI is run and no credentials are opened.">
      <div className="space-y-4 p-5" data-testid="bots-import-list">
        {error && <ErrorState title="Could not look for agents" message={error} />}
        {data?.notes.map((n) => (
          <p key={n.provider} className="flex items-start gap-2 text-sm text-muted-foreground" data-testid={`import-note-${n.provider}`}>
            <ProviderMark provider={n.provider} className="mt-0.5" /> {n.note}
          </p>
        ))}
        <ul className="space-y-2">
          {data?.candidates.map((c) => (
            <li key={c.key} className="rounded-lg border p-3">
              <div className="flex items-start gap-2.5">
                <ProviderMark provider={c.provider} className="mt-0.5" />
                <div className="min-w-0 flex-1">
                  <div className="text-sm font-medium">{c.name}</div>
                  <div className="text-2xs text-subtle-foreground">
                    {c.source} · {c.file}
                    {c.model ? ` · ${c.model}` : ""}
                  </div>
                  {c.description && <p className="mt-1 line-clamp-2 text-xs text-muted-foreground">{c.description}</p>}
                </div>
                {done[c.key] ? (
                  <Badge>imported</Badge>
                ) : (
                  <div className="flex shrink-0 flex-col gap-1">
                    <Button size="sm" onClick={() => void take(c.key, false)}>
                      Import
                    </Button>
                    {c.has_body && (
                      <Button size="sm" variant="ghost" onClick={() => void take(c.key, true)}>
                        with instructions
                      </Button>
                    )}
                  </div>
                )}
              </div>
            </li>
          ))}
        </ul>
      </div>
    </Sheet>
  )
}

type TaskMode = "work" | "council"

/** Task a bot: a Work session on its provider and model with its persona first, or a council with it in the proposer's seat. */
function TaskDialog({ bot, onClose }: { bot: Bot | null; onClose: () => void }) {
  const { open } = useApp()
  const projects = usePoll<ProjectList>(bot ? "/api/projects" : null, 30000)
  const [mode, setMode] = useState<TaskMode>("work")
  const [project, setProject] = useState("")
  const [task, setTask] = useState("")
  const [confirm, setConfirm] = useState<ConfirmRequest | null>(null)
  if (!bot) return <ConfirmDialog request={confirm} onClose={() => setConfirm(null)} />
  const ps = (projects.data?.projects ?? []).filter((p) => p.visibility !== "confidential" && (mode === "council" || p.local_path))
  const go = () => {
    if (mode === "work") {
      const prompt = `${bot.persona}\n\n${task.trim()}`
      setConfirm({
        title: `Start ${bot.name} on ${project}?`,
        description: `Lucidbench makes a new worktree next to the project's checkout and runs ${bot.provider}${bot.model ? ` (${bot.model})` : ""} there with your own account, so the run counts against your plan. The bot's persona goes first in the prompt. Nothing is pushed until you open a PR.`,
        body: <pre className="max-h-40 overflow-auto whitespace-pre-wrap rounded-md border bg-muted/40 p-2 text-xs">{prompt}</pre>,
        confirmLabel: "Start session",
        run: async () => {
          try {
            const s = await startSession({ provider: bot.provider, model: bot.model || undefined, profile: bot.profile || undefined, harness: bot.provider === "claude" ? "mine" : "clean", project, prompt } as Parameters<typeof startSession>[0])
            onClose()
            open("work", [s.id])
          } catch (e) {
            toast.error("Could not start the session", { description: errorMessage(e) })
          }
        },
      })
    } else {
      setConfirm({
        title: `Start a council with ${bot.name} proposing?`,
        description: `${bot.provider} takes the proposer's seat; the critics are the project's or the defaults. Each runs on your own account. The council keeps its own prompts, so the bot's persona is not sent.`,
        confirmLabel: "Start council",
        run: async () => {
          try {
            const s = await sendJSON<{ id: string }>("/api/council/sessions", "POST", { input: task.trim(), project: project || undefined, proposer: bot.provider })
            onClose()
            open("council", [s.id])
          } catch (e) {
            toast.error("Could not start the council", { description: errorMessage(e) })
          }
        },
      })
    }
  }
  return (
    <>
      <Dialog open onClose={onClose} title={`Task ${bot.name}`} description="Nothing starts until you confirm.">
        <div className="space-y-3" data-testid="bot-task-dialog">
          <div role="radiogroup" aria-label="Task as" className="grid grid-cols-2 gap-1.5">
            {(
              [
                { id: "work", label: "Work session", icon: SquareTerminal },
                { id: "council", label: "Council seat", icon: Vote },
              ] as const
            ).map((m) => (
              <button
                key={m.id}
                role="radio"
                aria-checked={mode === m.id}
                onClick={() => setMode(m.id)}
                className={cn("flex items-center justify-center gap-1.5 rounded-md border px-2 py-2 text-sm", mode === m.id ? "border-brand bg-brand-soft text-brand-fg" : "text-muted-foreground hover:bg-accent/50")}
              >
                <m.icon className="size-4" /> {m.label}
              </button>
            ))}
          </div>
          <label className="block space-y-1">
            <span className="text-sm font-medium">Project</span>
            <select aria-label="Task project" value={project} onChange={(e) => setProject(e.target.value)} className="h-9 w-full rounded-md border bg-background/50 px-2 text-sm">
              <option value="">{mode === "work" ? "Choose a project with a checkout" : "No project"}</option>
              {ps.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.name}
                </option>
              ))}
            </select>
          </label>
          <label className="block space-y-1">
            <span className="text-sm font-medium">{mode === "work" ? "The task" : "The braindump"}</span>
            <textarea aria-label="What to do" value={task} onChange={(e) => setTask(e.target.value)} rows={4} className="w-full rounded-md border bg-background/50 p-2.5 text-sm" />
          </label>
          <div className="flex justify-end gap-2">
            <Button variant="secondary" onClick={onClose}>
              Cancel
            </Button>
            <Button onClick={go} disabled={!task.trim() || (mode === "work" && !project)} data-testid="bot-task-go">
              Continue
            </Button>
          </div>
        </div>
      </Dialog>
      <ConfirmDialog request={confirm} onClose={() => setConfirm(null)} />
    </>
  )
}
