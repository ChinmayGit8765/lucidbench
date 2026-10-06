import { useEffect, useState, type ReactNode } from "react"
import {
  CalendarDays,
  Check,
  Columns3,
  ExternalLink,
  FilePlus2,
  FileText,
  FolderKanban,
  GitPullRequest,
  Link2,
  Play,
  SquareTerminal,
  Tag,
  Vote,
  Waypoints,
  X,
  LayoutList,
} from "lucide-react"
import { toast } from "sonner"

import { providerInfo } from "@/components/ProviderMark"
import { StatusPill } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Sheet } from "@/components/ui/dialog"
import { errorMessage, getJSON } from "@/lib/api"
import { useApp } from "@/lib/app"
import { boardsApi, DEFAULT_BOARD, labelColor, type Board, type Card, type CardFields } from "@/lib/boards"
import { linearApi } from "@/lib/linear"
import { usePrefs } from "@/lib/prefs"
import { trelloApi } from "@/lib/trello"
import { LinkInput, PromoteDialog, RemoteLink } from "@/pages/boards/RemoteLinks"
import { baseName, memoryApi, pageRoute, safeName, type Hit } from "@/lib/memory"
import type { ProjectList } from "@/lib/projects"
import { cn } from "@/lib/utils"
import { sessionPath, sessionsPath, STATUS_INFO, type WorkSession } from "@/lib/work"

const field =
  "h-8 w-full rounded-md border bg-background/60 px-2.5 text-sm outline-none transition-colors placeholder:text-subtle-foreground hover:border-border-strong focus-visible:border-border-strong focus-visible:ring-2 focus-visible:ring-ring/30"

/** The card detail slide-over. Every field saves on change. */
export function CardSheet({ board, card, onClose, onSaved }: { board: Board; card: Card | null; onClose: () => void; onSaved: (c: Card) => void }) {
  return (
    <Sheet
      open={card !== null}
      onClose={onClose}
      className="max-w-xl"
      title={card ? <span className="flex items-center gap-2">{card.done && <Check className="size-4 text-success" />}Card</span> : ""}
      description={card ? <span className="font-mono">{card.id} · {board.title}</span> : undefined}
    >
      {card && <Body key={card.id} board={board} card={card} onSaved={onSaved} />}
    </Sheet>
  )
}

function Body({ board, card, onSaved }: { board: Board; card: Card; onSaved: (c: Card) => void }) {
  const { navigate, open } = useApp()
  const [title, setTitle] = useState(card.title)
  const [projects, setProjects] = useState<string[]>([])
  const [session, setSession] = useState<WorkSession | null>(null)
  const [linking, setLinking] = useState(false)
  const { prefs } = usePrefs()
  const linearOn = prefs.extensions.linear?.added ?? false
  const trelloOn = prefs.extensions.trello?.added ?? false
  const [remote, setRemote] = useState<"promote" | "link-linear" | "link-trello" | null>(null)

  useEffect(() => setTitle(card.title), [card.title])

  useEffect(() => {
    let cancelled = false
    getJSON<ProjectList>("/api/projects")
      .then((l) => !cancelled && setProjects(l.projects.map((p) => p.id)))
      .catch(() => undefined)
    return () => {
      cancelled = true
    }
  }, [])

  // The card's Work session: its status, and the PR once there is one. Work
  // writes the session id on the card's work:: line, then the PR URL once a
  // PR is open; for a URL, the session is the one that opened that PR.
  const prURL = card.work && /^https?:\/\//.test(card.work) ? card.work : null
  useEffect(() => {
    setSession(null)
    if (!card.work) return
    let cancelled = false
    const found = prURL
      ? getJSON<WorkSession[]>(sessionsPath).then((l) => l.find((s) => s.card === card.id && s.pr_url === prURL) ?? null)
      : getJSON<WorkSession>(sessionPath(card.work))
    found.then((s) => !cancelled && setSession(s)).catch(() => undefined)
    return () => {
      cancelled = true
    }
  }, [card.work, card.id, prURL])
  const pr = session?.pr_url ?? prURL
  const workID = session?.id ?? (prURL ? null : card.work)

  const save = async (fields: CardFields) => {
    try {
      onSaved(await boardsApi.update(board.id, card.id, fields))
    } catch (e) {
      toast.error("Could not save the card", { description: errorMessage(e) })
    }
  }

  const createBrief = async () => {
    const path = `Inbox/${safeName(card.title)}.md`
    const body = "## Problem\n\n## Outcome\n\n## Done criteria\n\n- [ ] \n\n## Risks\n\n## First steps\n\n## Open questions\n"
    try {
      await memoryApi.save(path, { title: card.title, front: { type: "brief", status: "draft", ...(card.project ? { project: card.project } : {}) }, body })
      await save({ memory: path })
      toast.success("Brief created", { description: path })
    } catch (e) {
      toast.error("Could not create the brief", { description: errorMessage(e) })
    }
  }

  // A run spends tokens on an account the user picks, so "Start work" opens
  // Work's new-session page with this card chosen; Work starts the session
  // (POST /api/work/sessions) once a provider is picked and confirmed.
  const onWorkBoard = board.id === DEFAULT_BOARD
  const startWork = () => open("work", ["new", card.id])

  return (
    <div className="space-y-6 px-5 py-5">
      <div className="flex items-start gap-3">
        <button
          type="button"
          aria-label={card.done ? "Mark as not done" : "Mark as done"}
          onClick={() => void save({ done: !card.done })}
          className={cn(
            "mt-1.5 flex size-5 shrink-0 items-center justify-center rounded-full border-2 transition-colors",
            card.done ? "border-success bg-success text-white" : "border-border-strong hover:border-success",
          )}
        >
          {card.done && <Check className="size-3.5" strokeWidth={3} />}
        </button>
        <textarea
          value={title}
          rows={1}
          aria-label="Card title"
          onChange={(e) => setTitle(e.target.value.replace(/\n/g, " "))}
          onBlur={() => title.trim() && title.trim() !== card.title && void save({ title: title.trim() })}
          onKeyDown={(e) => e.key === "Enter" && (e.preventDefault(), (e.currentTarget as HTMLTextAreaElement).blur())}
          className={cn("min-h-9 w-full resize-none bg-transparent text-xl font-semibold leading-snug tracking-tight outline-none [field-sizing:content]", card.done && "text-muted-foreground line-through")}
        />
      </div>

      <div className="flex flex-wrap gap-2">
        <Button
          size="sm"
          variant="secondary"
          disabled={!card.memory}
          title={card.memory ? `Open ${card.memory}` : "Link or create a brief first"}
          onClick={() => card.memory && navigate(pageRoute(card.memory))}
        >
          <FileText /> Open brief
        </Button>
        {pr ? (
          <Button size="sm" asChild>
            <a href={pr} target="_blank" rel="noreferrer">
              <GitPullRequest /> Open PR
            </a>
          </Button>
        ) : workID ? (
          <>
            <Button size="sm" onClick={() => open("work", [workID])}>
              <SquareTerminal /> Open session
            </Button>
            {session && session.status !== "running" && onWorkBoard && (
              <Button size="sm" variant="ghost" onClick={startWork} title="Start another session on this card">
                <Play /> Start again
              </Button>
            )}
          </>
        ) : (
          <Button
            size="sm"
            disabled={!onWorkBoard}
            onClick={startWork}
            title={onWorkBoard ? "Pick a provider and start an agent on this card" : "Work starts from cards on the work board"}
          >
            <Play /> Start work
          </Button>
        )}
      </div>

      <dl className="space-y-1">
        <Prop icon={Columns3} label="Column">
          <select value={card.column} onChange={(e) => void save({ column: e.target.value })} className={field} aria-label="Column">
            {board.columns.map((c) => (
              <option key={c} value={c}>
                {c}
              </option>
            ))}
          </select>
        </Prop>
        <Prop icon={FolderKanban} label="Project">
          <select value={card.project ?? ""} onChange={(e) => void save({ project: e.target.value })} className={field} aria-label="Project">
            <option value="">No project</option>
            {[...new Set([...(card.project ? [card.project] : []), ...projects])].map((p) => (
              <option key={p} value={p}>
                {p}
              </option>
            ))}
          </select>
        </Prop>
        <Prop icon={CalendarDays} label="Due">
          <input type="date" value={card.due ?? ""} onChange={(e) => void save({ due: e.target.value })} className={field} aria-label="Due date" />
        </Prop>
        <Prop icon={Tag} label="Labels">
          <Labels labels={card.labels ?? []} onChange={(labels) => void save({ labels })} />
        </Prop>
        <Prop icon={FileText} label="Brief">
          {card.memory ? (
            <div className="flex items-center gap-1.5">
              <button
                type="button"
                onClick={() => navigate(pageRoute(card.memory!))}
                className="flex min-w-0 flex-1 items-center gap-2 rounded-md border bg-background/60 px-2.5 py-1.5 text-left text-sm hover:border-border-strong"
              >
                <FileText className="size-3.5 shrink-0 text-brand" />
                <span className="truncate">{baseName(card.memory)}</span>
                <span className="ml-auto truncate font-mono text-2xs text-subtle-foreground">{card.memory}</span>
              </button>
              <button type="button" aria-label="Unlink the brief" title="Unlink" onClick={() => void save({ memory: "" })} className="inline-flex size-7 items-center justify-center rounded-md text-subtle-foreground hover:bg-accent hover:text-foreground">
                <X className="size-3.5" />
              </button>
            </div>
          ) : linking ? (
            <PagePicker
              onPick={(p) => {
                setLinking(false)
                void save({ memory: p })
              }}
              onClose={() => setLinking(false)}
            />
          ) : (
            <div className="flex gap-1.5">
              <Button size="sm" variant="ghost" onClick={() => setLinking(true)}>
                <Link2 /> Link a page
              </Button>
              <Button size="sm" variant="ghost" onClick={() => void createBrief()}>
                <FilePlus2 /> Create a brief
              </Button>
            </div>
          )}
        </Prop>
        <Prop icon={Vote} label="Council">
          {card.council ? (
            <LinkRow icon={Vote} onClick={() => open("council", [card.council!])} label="Council session" detail={card.council} />
          ) : (
            <span className="text-sm text-subtle-foreground">No council session</span>
          )}
        </Prop>
        <Prop icon={SquareTerminal} label="Work">
          {workID ? (
            <LinkRow
              icon={SquareTerminal}
              onClick={() => open("work", [workID])}
              label={session ? `${providerInfo(session.provider)?.label ?? session.provider} session` : "Work session"}
              detail={session?.branch ?? workID}
              pill={session && <StatusPill tone={STATUS_INFO[session.status].tone}>{STATUS_INFO[session.status].label}</StatusPill>}
            />
          ) : (
            <span className="text-sm text-subtle-foreground">{pr ? "The session that opened the PR is gone" : "Not started"}</span>
          )}
        </Prop>
        {(card.linear || linearOn) && (
          <Prop icon={Waypoints} label="Linear">
            {card.linear ? (
              <RemoteLink label="Linear" id={card.linear} url={card.linear_url} onUnlink={() => void save({ linear: "", linear_url: "" })} />
            ) : remote === "link-linear" ? (
              <LinkInput
                placeholder="ENG-12 or an issue URL"
                link={async (text) => (await linearApi.link(board.id, card.id, text)).card}
                onLinked={(c) => (setRemote(null), onSaved(c), toast.success(`Linked ${c.linear}`))}
                onClose={() => setRemote(null)}
              />
            ) : (
              <div className="flex gap-1.5">
                <Button size="sm" variant="ghost" onClick={() => setRemote("promote")}>
                  <Waypoints /> Promote to Linear
                </Button>
                <Button size="sm" variant="ghost" onClick={() => setRemote("link-linear")}>
                  <Link2 /> Link issue
                </Button>
              </div>
            )}
          </Prop>
        )}
        {(card.trello || trelloOn) && (
          <Prop icon={LayoutList} label="Trello">
            {card.trello ? (
              <RemoteLink label="Trello" id={card.trello} url={card.trello_url} onUnlink={() => void save({ trello: "", trello_url: "" })} />
            ) : remote === "link-trello" ? (
              <LinkInput
                placeholder="A Trello card URL or id"
                link={async (text) => (await trelloApi.link(board.id, card.id, text)).card}
                onLinked={(c) => (setRemote(null), onSaved(c), toast.success("Linked the Trello card"))}
                onClose={() => setRemote(null)}
              />
            ) : (
              <Button size="sm" variant="ghost" onClick={() => setRemote("link-trello")}>
                <Link2 /> Link Trello card
              </Button>
            )}
          </Prop>
        )}
        {pr && (
          <Prop icon={GitPullRequest} label="Pull request">
            <a
              href={pr}
              target="_blank"
              rel="noreferrer"
              className="flex min-w-0 items-center gap-2 rounded-md border bg-background/60 px-2.5 py-1.5 text-sm hover:border-border-strong"
            >
              <GitPullRequest className="size-3.5 shrink-0 text-success" />
              <span className="truncate">Draft PR</span>
              <span className="ml-auto truncate font-mono text-2xs text-subtle-foreground">{pr.replace(/^https?:\/\/(www\.)?github\.com\//, "")}</span>
              <ExternalLink className="size-3 shrink-0 text-subtle-foreground" />
            </a>
          </Prop>
        )}
      </dl>

      <PromoteDialog board={board.id} card={card} open={remote === "promote"} onClose={() => setRemote(null)} onDone={onSaved} />

      <p className="border-t pt-4 text-xs text-subtle-foreground">
        Saved to <span className="font-mono">Boards/{board.id}.md</span> as an Obsidian Kanban card, with its fields as <span className="font-mono">key:: value</span> lines.
      </p>
    </div>
  )
}

/** A linked record (council or work session) shown as a row that opens it. */
function LinkRow({ icon: Icon, label, detail, pill, onClick }: { icon: typeof Tag; label: string; detail: string; pill?: ReactNode; onClick: () => void }) {
  return (
    <button
      type="button"
      onClick={onClick}
      className="flex w-full min-w-0 items-center gap-2 rounded-md border bg-background/60 px-2.5 py-1.5 text-left text-sm hover:border-border-strong"
    >
      <Icon className="size-3.5 shrink-0 text-brand" />
      <span className="truncate">{label}</span>
      {pill}
      <span className="ml-auto truncate font-mono text-2xs text-subtle-foreground">{detail}</span>
    </button>
  )
}

function Prop({ icon: Icon, label, children }: { icon: typeof Tag; label: string; children: ReactNode }) {
  return (
    <div className="grid grid-cols-[7.5rem_minmax(0,1fr)] items-center gap-3 py-1">
      <dt className="flex items-center gap-2 text-sm text-muted-foreground">
        <Icon className="size-3.5 text-subtle-foreground" />
        {label}
      </dt>
      <dd className="min-w-0">{children}</dd>
    </div>
  )
}

function Labels({ labels, onChange }: { labels: string[]; onChange: (l: string[]) => void }) {
  const [v, setV] = useState("")
  const add = () => {
    const l = v.trim().replace(/,/g, " ")
    if (l && !labels.includes(l)) onChange([...labels, l])
    setV("")
  }
  return (
    <div className={cn(field, "flex h-auto min-h-8 flex-wrap items-center gap-1 py-1")}>
      {labels.map((l) => (
        <span key={l} className="flex items-center gap-0.5 rounded px-1.5 py-px text-xs font-medium" style={{ background: `color-mix(in oklch, ${labelColor(l)} 16%, transparent)`, color: labelColor(l) }}>
          {l}
          <button type="button" aria-label={`Remove ${l}`} onClick={() => onChange(labels.filter((x) => x !== l))} className="opacity-70 hover:opacity-100">
            <X className="size-3" />
          </button>
        </span>
      ))}
      <input
        value={v}
        onChange={(e) => setV(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter" || e.key === ",") {
            e.preventDefault()
            add()
          }
          if (e.key === "Backspace" && !v && labels.length) onChange(labels.slice(0, -1))
        }}
        onBlur={() => v.trim() && add()}
        placeholder={labels.length ? "" : "Add a label"}
        aria-label="Add a label"
        className="min-w-16 flex-1 bg-transparent text-sm outline-none placeholder:text-subtle-foreground"
      />
    </div>
  )
}

function PagePicker({ onPick, onClose }: { onPick: (path: string) => void; onClose: () => void }) {
  const [q, setQ] = useState("")
  const [hits, setHits] = useState<Hit[]>([])
  useEffect(() => {
    if (!q.trim()) {
      setHits([])
      return
    }
    let cancelled = false
    const id = setTimeout(() => {
      memoryApi
        .search(q, 8)
        .then((h) => !cancelled && setHits(h.filter((x) => !x.path.startsWith("Boards/"))))
        .catch(() => undefined)
    }, 150)
    return () => {
      cancelled = true
      clearTimeout(id)
    }
  }, [q])
  return (
    <div className="rounded-md border bg-background/60">
      <input
        autoFocus
        value={q}
        onChange={(e) => setQ(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Escape") onClose()
          if (e.key === "Enter" && hits[0]) onPick(hits[0].path)
        }}
        placeholder="Search Memory for a page…"
        aria-label="Search Memory for a page"
        className="h-8 w-full bg-transparent px-2.5 text-sm outline-none"
      />
      {hits.length > 0 && (
        <ul className="border-t py-1">
          {hits.map((h) => (
            <li key={h.path}>
              <button type="button" onClick={() => onPick(h.path)} className="flex w-full items-center gap-2 px-2.5 py-1.5 text-left text-sm hover:bg-accent">
                <FileText className="size-3.5 shrink-0 text-subtle-foreground" />
                <span className="truncate">{h.title}</span>
                <span className="ml-auto truncate font-mono text-2xs text-subtle-foreground">{h.path}</span>
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
