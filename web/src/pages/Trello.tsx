import { useEffect, useRef, useState } from "react"
import { ArrowRightLeft, CalendarDays, ExternalLink, LayoutList, Plus, X } from "lucide-react"
import { toast } from "sonner"

import { SetupCard } from "@/components/SetupCard"
import { PageHeader, RefreshButton } from "@/components/Shell"
import { Button } from "@/components/ui/button"
import { Card } from "@/components/ui/card"
import { ConfirmDialog, type ConfirmRequest } from "@/components/ui/confirm"
import { Menu } from "@/components/ui/menu"
import { EmptyState, ErrorState, Skeleton } from "@/components/ui/states"
import { errorMessage, refreshAll, usePoll } from "@/lib/api"
import { useApp } from "@/lib/app"
import {
  trelloApi,
  trelloLabelColor,
  TRELLO_POLL_MS,
  type TrelloBoard,
  type TrelloBoardView,
  type TrelloCard,
  type TrelloList,
  type TrelloStatus,
} from "@/lib/trello"
import { plural } from "@/lib/utils"

const select =
  "h-8 rounded-md border bg-background/60 px-2.5 text-sm outline-none transition-colors hover:border-border-strong focus-visible:border-border-strong focus-visible:ring-2 focus-visible:ring-ring/30"

export default function Trello({ subpath }: { subpath: string[] }) {
  const status = usePoll<TrelloStatus>("/api/trello/status", 30000)
  return (
    <div className="space-y-6">
      <PageHeader
        icon={<LayoutList />}
        title="Trello"
        description="Your Trello boards as lists of cards. Trello owns them: add a card or move one between lists here, and every card opens in Trello."
        actions={<RefreshButton refreshing={status.refreshing} updatedAt={status.updatedAt} />}
      />
      {status.error && !status.data ? (
        <ErrorState title="Could not check the Trello connector" message={status.error.message} onRetry={status.refresh} />
      ) : !status.data ? (
        <Skeleton className="h-64 rounded-xl" />
      ) : !status.data.configured ? (
        <SetupCard
          title="Connect Trello with a key and token"
          intro="Lucidbench reads Trello through its REST API with your API key and a token. Both stay in your environment; the config only names the variables."
          steps={[
            <>
              Open <code>trello.com/power-ups/admin</code>, create a Power-Up (or open one) and copy its <strong>API key</strong>. Then generate a <strong>token</strong> from the same page.
            </>,
            <>
              Set them as <code>{status.data.key_ref.replace(/^env:/, "")}</code> and <code>{status.data.token_ref.replace(/^env:/, "")}</code> where the daemon starts, then restart Lucidbench.
            </>,
            <>
              To use different variables, point <code>integrations.trello.key</code> and <code>integrations.trello.token</code> at them, as <code>env:NAME</code>, in your config.
            </>,
          ]}
          vars={[
            { name: status.data.key_ref.replace(/^env:/, ""), set: status.data.key_set },
            { name: status.data.token_ref.replace(/^env:/, ""), set: status.data.token_set },
          ]}
        />
      ) : (
        <Boards boardId={subpath[0]} />
      )}
    </div>
  )
}

function Boards({ boardId }: { boardId?: string }) {
  const { navigate } = useApp()
  const boards = usePoll<TrelloBoard[]>("/api/trello/boards", TRELLO_POLL_MS)
  const list = boards.data ?? []
  const id = boardId && list.some((b) => b.id === boardId) ? boardId : list[0]?.id
  if (boards.error && !boards.data) return <ErrorState title="Could not read Trello" message={boards.error.message} onRetry={boards.refresh} />
  if (!boards.data) return <Skeleton className="h-80 rounded-xl" />
  if (list.length === 0) {
    return (
      <Card className="border-dashed">
        <EmptyState icon={<LayoutList />} title="No open boards" description="This Trello account has no open boards yet." />
      </Card>
    )
  }
  return (
    <>
      <div className="flex items-center gap-2">
        <select aria-label="Board" value={id} onChange={(e) => navigate(`/trello/${encodeURIComponent(e.target.value)}`)} className={select}>
          {list.map((b) => (
            <option key={b.id} value={b.id}>
              {b.name}
            </option>
          ))}
        </select>
        {id && (
          <a
            href={list.find((b) => b.id === id)?.url}
            target="_blank"
            rel="noreferrer"
            className="inline-flex items-center gap-1 text-xs text-subtle-foreground hover:text-foreground"
          >
            Open in Trello <ExternalLink className="size-3" />
          </a>
        )}
      </div>
      {id && <BoardView key={id} id={id} />}
    </>
  )
}

function BoardView({ id }: { id: string }) {
  const poll = usePoll<TrelloBoardView>(`/api/trello/boards/${encodeURIComponent(id)}`, TRELLO_POLL_MS)
  const [confirm, setConfirm] = useState<ConfirmRequest | null>(null)
  const [composer, setComposer] = useState<string | null>(null)
  const view = poll.data
  if (poll.error && !view) return <ErrorState title="Could not read this board" message={poll.error.message} onRetry={poll.refresh} />
  if (!view) {
    return (
      <div className="flex gap-3">
        {[0, 1, 2].map((i) => (
          <Skeleton key={i} className="h-80 w-72 rounded-xl" />
        ))}
      </div>
    )
  }
  const byList = (l: TrelloList) => view.cards.filter((c) => c.list_id === l.id).sort((a, b) => a.pos - b.pos)

  const add = async (list: TrelloList, name: string) => {
    try {
      await trelloApi.add(list.id, name)
      toast.success(`Added to ${list.name}`)
      setComposer(null)
      refreshAll()
    } catch (e) {
      toast.error("Could not add the card", { description: errorMessage(e) })
    }
  }
  const askMove = (card: TrelloCard, to: TrelloList) => {
    const from = view.lists.find((l) => l.id === card.list_id)
    setConfirm({
      title: "Move this card in Trello?",
      description: `“${card.name}” moves from ${from?.name ?? "its list"} to ${to.name}. This changes the board in Trello.`,
      confirmLabel: `Move to ${to.name}`,
      run: async () => {
        try {
          await trelloApi.move(card.id, to.id)
          toast.success(`Moved to ${to.name}`)
          refreshAll()
        } catch (e) {
          toast.error("Could not move the card", { description: errorMessage(e) })
        }
      },
    })
  }

  return (
    <>
      <p className="-mt-2 text-xs text-subtle-foreground">
        {plural(view.lists.length, "list")} · {plural(view.cards.length, "card")}
      </p>
      <div className="-mx-5 overflow-x-auto px-5 pb-4 md:-mx-8 md:px-8">
        <div className="flex min-h-[26rem] items-start gap-3">
          {view.lists.map((l) => (
            <section key={l.id} aria-label={`${l.name} list`} className="flex max-h-[calc(100vh-17rem)] w-72 shrink-0 flex-col rounded-xl border bg-muted/40">
              <header className="flex items-center gap-2 px-3 pb-2 pt-3">
                <h2 className="truncate text-sm font-semibold">{l.name}</h2>
                <span className="rounded-full bg-background/80 px-1.5 text-2xs tabular-nums text-muted-foreground">{byList(l).length}</span>
                <button
                  type="button"
                  onClick={() => setComposer(l.id)}
                  aria-label={`Add a card to ${l.name}`}
                  title="Add a card"
                  className="ml-auto inline-flex size-6 items-center justify-center rounded-md text-subtle-foreground hover:bg-accent hover:text-foreground"
                >
                  <Plus className="size-3.5" />
                </button>
              </header>
              <div className="flex min-h-16 flex-1 flex-col gap-2 overflow-y-auto px-2 pb-2">
                {byList(l).map((c) => (
                  <TrelloCardView key={c.id} card={c} lists={view.lists} onMove={(to) => askMove(c, to)} />
                ))}
                {composer === l.id ? (
                  <Composer onAdd={(n) => void add(l, n)} onClose={() => setComposer(null)} />
                ) : (
                  <button
                    type="button"
                    onClick={() => setComposer(l.id)}
                    className="flex h-8 items-center gap-1.5 rounded-lg px-2 text-sm text-subtle-foreground transition-colors hover:bg-background/70 hover:text-foreground"
                  >
                    <Plus className="size-3.5" /> Add a card
                  </button>
                )}
              </div>
            </section>
          ))}
        </div>
      </div>
      <ConfirmDialog request={confirm} onClose={() => setConfirm(null)} />
    </>
  )
}

function TrelloCardView({ card, lists, onMove }: { card: TrelloCard; lists: TrelloList[]; onMove: (to: TrelloList) => void }) {
  return (
    <article className="group rounded-lg border bg-card p-3 shadow-card transition-[border-color] hover:border-border-strong">
      {card.labels.length > 0 && (
        <div className="mb-2 flex flex-wrap gap-1">
          {card.labels.map((l, i) => (
            <span key={i} title={l.name} className="h-1.5 w-8 rounded-full" style={{ background: trelloLabelColor(l.color) }} />
          ))}
        </div>
      )}
      <div className="flex items-start gap-2">
        <a href={card.url} target="_blank" rel="noreferrer" className="min-w-0 flex-1 text-sm leading-snug hover:text-brand-fg" aria-label={`${card.name}, open in Trello`}>
          {card.name}
        </a>
        <div className="-mr-1 -mt-1 opacity-0 transition-opacity focus-within:opacity-100 group-hover:opacity-100">
          <Menu
            label="Move card"
            trigger={<ArrowRightLeft />}
            items={lists.filter((l) => l.id !== card.list_id).map((l) => ({ label: `Move to ${l.name}`, onSelect: () => onMove(l) }))}
          />
        </div>
      </div>
      {card.due && (
        <div className="mt-2 flex items-center gap-1 text-2xs text-muted-foreground">
          <CalendarDays className="size-3" />
          {new Date(card.due).toLocaleDateString(undefined, { month: "short", day: "numeric" })}
        </div>
      )}
    </article>
  )
}

function Composer({ onAdd, onClose }: { onAdd: (name: string) => void; onClose: () => void }) {
  const [v, setV] = useState("")
  const ref = useRef<HTMLTextAreaElement>(null)
  useEffect(() => ref.current?.focus(), [])
  const submit = () => v.trim() && onAdd(v.trim())
  return (
    <div className="rounded-lg border border-brand/40 bg-card p-2 shadow-card">
      <textarea
        ref={ref}
        value={v}
        rows={2}
        onChange={(e) => setV(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter" && !e.shiftKey) {
            e.preventDefault()
            submit()
          }
          if (e.key === "Escape") onClose()
        }}
        placeholder="Card title. Enter adds it to Trello"
        aria-label="Card title"
        className="w-full resize-none bg-transparent text-sm outline-none placeholder:text-subtle-foreground"
      />
      <div className="mt-1 flex items-center gap-1.5">
        <Button size="sm" onClick={submit} disabled={!v.trim()}>
          Add to Trello
        </Button>
        <button type="button" onClick={onClose} aria-label="Close" className="inline-flex size-7 items-center justify-center rounded-md text-subtle-foreground hover:bg-accent hover:text-foreground">
          <X className="size-4" />
        </button>
      </div>
    </div>
  )
}
