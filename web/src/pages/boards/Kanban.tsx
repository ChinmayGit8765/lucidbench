import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from "react"
import {
  closestCorners,
  DndContext,
  DragOverlay,
  KeyboardSensor,
  PointerSensor,
  useDroppable,
  useSensor,
  useSensors,
  type DragEndEvent,
  type DragOverEvent,
  type DragStartEvent,
} from "@dnd-kit/core"
import { SortableContext, sortableKeyboardCoordinates, useSortable, verticalListSortingStrategy } from "@dnd-kit/sortable"
import { CSS } from "@dnd-kit/utilities"
import { CalendarDays, Check, ChevronLeft, FileText, LayoutList, PenLine, Plus, SquareKanban, Vote, SquareTerminal, X } from "lucide-react"
import { toast } from "sonner"

import { PageHeader, RefreshButton } from "@/components/Shell"
import { Button } from "@/components/ui/button"
import { Card } from "@/components/ui/card"
import { celebrate } from "@/components/Celebrate"
import { StateSprite } from "@/components/StateSprite"
import { ErrorState, Skeleton } from "@/components/ui/states"
import { errorMessage, usePoll } from "@/lib/api"
import { useApp } from "@/lib/app"
import {
  boardsApi,
  DEFAULT_BOARD,
  dueState,
  formatDue,
  labelColor,
  onBoardsIntent,
  takeBoardsIntent,
  type Board,
  type Card as CardT,
} from "@/lib/boards"
import { newBraindump } from "@/lib/council"
import { cn } from "@/lib/utils"
import { CardSheet } from "@/pages/boards/CardSheet"

const COLUMN_TONE: Record<string, string> = {
  Inbox: "var(--neutral)",
  Ready: "var(--brand)",
  "In progress": "var(--info)",
  Review: "var(--warning)",
  Done: "var(--success)",
}
const toneOf = (col: string, i: number) => COLUMN_TONE[col] ?? ["var(--neutral)", "var(--brand)", "var(--info)", "var(--warning)", "var(--success)"][i % 5]

type Order = Record<string, string[]>

/** When the last drag ended: the click that follows a drop must not open the card. */
let lastDrop = 0

function orderOf(b: Board): Order {
  const o: Order = {}
  for (const c of b.columns) o[c] = []
  for (const card of b.cards) (o[card.column] ??= []).push(card.id)
  return o
}

/** One board: columns of cards, drag between and within columns, a composer per column. */
export function Kanban({ id, cardId }: { id: string; cardId: string | null }) {
  const { navigate, open: openModule } = useApp()
  const poll = usePoll<Board>(`/api/boards/${encodeURIComponent(id)}`, 15000)
  const [board, setBoard] = useState<Board | null>(null)
  const [order, setOrder] = useState<Order>({})
  const [dragging, setDragging] = useState<string | null>(null)
  const [composer, setComposer] = useState<string | null>(null)
  const startCol = useRef<string | null>(null)
  const busy = useRef(false)

  // Server state wins, except while a drag or a move is in flight.
  useEffect(() => {
    if (!poll.data || dragging || busy.current) return
    setBoard(poll.data)
    setOrder(orderOf(poll.data))
  }, [poll.data, dragging])

  const cards = useMemo(() => new Map((board?.cards ?? []).map((c) => [c.id, c])), [board])
  const columnOf = useCallback((cid: string) => Object.keys(order).find((c) => order[c].includes(cid)), [order])

  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 5 } }),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }),
  )

  // "Add card…" from the palette opens the first column's composer.
  useEffect(() => {
    const take = () => {
      // Wait for the board: the composer opens in its first column.
      if (!board) return
      const i = takeBoardsIntent()
      if (i?.kind === "add-card" && i.board === id) setComposer(board.columns[0] ?? null)
    }
    take()
    return onBoardsIntent(take)
  }, [id, board])

  const reload = () => poll.refresh()

  const patchCard = (c: CardT) => setBoard((b) => (b ? { ...b, cards: b.cards.map((x) => (x.id === c.id ? c : x)) } : b))

  const onStart = (e: DragStartEvent) => {
    setDragging(String(e.active.id))
    startCol.current = columnOf(String(e.active.id)) ?? null
  }

  const onOver = ({ active, over }: DragOverEvent) => {
    if (!over) return
    const a = String(active.id)
    const o = String(over.id)
    const from = columnOf(a)
    const to = o.startsWith("col:") ? o.slice(4) : columnOf(o)
    if (!from || !to || from === to) return
    setOrder((prev) => {
      const src = prev[from].filter((x) => x !== a)
      const dst = [...prev[to]]
      const at = o.startsWith("col:") ? dst.length : Math.max(0, dst.indexOf(o))
      dst.splice(at, 0, a)
      return { ...prev, [from]: src, [to]: dst }
    })
  }

  const onEnd = async ({ active, over }: DragEndEvent) => {
    const a = String(active.id)
    setDragging(null)
    lastDrop = Date.now()
    if (!over) {
      if (poll.data) setOrder(orderOf(poll.data))
      return
    }
    const o = String(over.id)
    const col = columnOf(a)
    if (!col) return
    let list = order[col]
    if (!o.startsWith("col:") && o !== a && list.includes(o)) {
      const from = list.indexOf(a)
      const to = list.indexOf(o)
      list = [...list]
      list.splice(from, 1)
      list.splice(to, 0, a)
      setOrder((prev) => ({ ...prev, [col]: list }))
    }
    const index = list.indexOf(a)
    const before = poll.data ? orderOf(poll.data) : null
    if (before && startCol.current === col && before[col]?.indexOf(a) === index) return
    const from = startCol.current
    const fromIndex = from && before ? Math.max(0, before[from]?.indexOf(a) ?? 0) : 0
    const card = cards.get(a)
    busy.current = true
    try {
      // The new order is already on screen; the server catches up.
      const moved = await boardsApi.move(id, a, col, index)
      patchCard(moved)
      if (from && from !== col) {
        if (col === "Done") celebrate({ key: `card:${a}`, title: "Done!", detail: card?.title })
        toast(`Moved to ${col}`, {
          description: card?.title,
          action: {
            label: "Undo",
            onClick: () =>
              void boardsApi
                .move(id, a, from, fromIndex)
                .then(patchCard)
                .catch((e) => toast.error("Could not undo the move", { description: errorMessage(e) }))
                .finally(reload),
          },
        })
      }
    } catch (e) {
      // Put it back where it was.
      if (before) setOrder(before)
      toast.error("Could not move the card", { description: errorMessage(e) })
    } finally {
      busy.current = false
      reload()
    }
  }

  const add = async (column: string, title: string) => {
    try {
      const c = await boardsApi.add(id, { title, column })
      setBoard((b) => (b ? { ...b, cards: [...b.cards, c] } : b))
      setOrder((prev) => ({ ...prev, [column]: [...(prev[column] ?? []), c.id] }))
      reload()
    } catch (e) {
      toast.error("Could not add the card", { description: errorMessage(e) })
    }
  }

  const toggleDone = async (c: CardT) => {
    patchCard({ ...c, done: !c.done })
    try {
      patchCard(await boardsApi.update(id, c.id, { done: !c.done }))
      if (!c.done) celebrate({ key: `card:${c.id}`, title: "Done!", detail: c.title })
    } catch (e) {
      patchCard(c)
      toast.error("Could not update the card", { description: errorMessage(e) })
    }
  }

  if (poll.error && !board) {
    return (
      <div className="space-y-6">
        <BackLink />
        <ErrorState title={poll.error.status === 404 ? `There is no board “${id}”` : "Could not open this board"} message={poll.error.message} onRetry={poll.refresh} />
      </div>
    )
  }
  if (!board) {
    return (
      <div className="space-y-6">
        <BackLink />
        <Skeleton className="h-10 w-72" />
        <div className="flex gap-3">
          {[0, 1, 2, 3].map((i) => (
            <Skeleton key={i} className="h-80 w-72 rounded-xl" />
          ))}
        </div>
      </div>
    )
  }

  const open = cardId ? cards.get(cardId) ?? null : null
  const total = board.cards.length
  const done = board.cards.filter((c) => c.done || c.column === "Done").length

  return (
    <div className="space-y-5">
      <PageHeader
        icon={<SquareKanban />}
        eyebrow={<BackLink />}
        title={board.title}
        description={
          <>
            {total} {total === 1 ? "card" : "cards"} · {done} done · stored in <span className="font-mono text-xs">Boards/{board.id}.md</span>
          </>
        }
        actions={
          <>
            <RefreshButton refreshing={poll.refreshing} updatedAt={poll.updatedAt} />
            <Button size="sm" onClick={() => setComposer(board.columns[0] ?? null)}>
              <Plus /> Add card
            </Button>
          </>
        }
      />
      {total === 0 && board.id === DEFAULT_BOARD && (
        <Card className="flex flex-wrap items-center gap-3 border-dashed px-4 py-3">
          <StateSprite state="empty" className="size-12" />
          <div className="min-w-0 flex-1">
            <div className="text-sm font-medium">Approve a brief in Council to get your first card</div>
            <div className="text-xs text-muted-foreground">An approved brief lands here in Ready, linked to its page in Memory. From the card, Start work hands it to an agent.</div>
          </div>
          <Button size="sm" onClick={() => newBraindump(openModule)}>
            <PenLine /> New braindump
          </Button>
        </Card>
      )}
      <DndContext sensors={sensors} collisionDetection={closestCorners} onDragStart={onStart} onDragOver={onOver} onDragEnd={(e) => void onEnd(e)} onDragCancel={() => setDragging(null)}>
        <div className="-mx-5 overflow-x-auto px-5 pb-4 md:-mx-8 md:px-8">
          <div className="flex min-h-[28rem] items-start gap-3">
            {board.columns.map((col, i) => (
              <Column
                key={col}
                name={col}
                tone={toneOf(col, i)}
                ids={order[col] ?? []}
                composing={composer === col}
                onCompose={(on) => setComposer(on ? col : null)}
                onAdd={(t) => void add(col, t)}
              >
                {(order[col] ?? []).map((cid) => {
                  const c = cards.get(cid)
                  return c ? <SortableCard key={cid} card={c} onOpen={() => navigate(`/boards/${encodeURIComponent(id)}/${encodeURIComponent(cid)}`)} onToggle={() => void toggleDone(c)} /> : null
                })}
              </Column>
            ))}
          </div>
        </div>
        <DragOverlay dropAnimation={{ duration: 180, easing: "cubic-bezier(0.22, 1, 0.36, 1)" }}>
          {dragging && cards.get(dragging) ? <CardFace card={cards.get(dragging)!} overlay /> : null}
        </DragOverlay>
      </DndContext>
      <CardSheet
        board={board}
        card={open}
        onClose={() => navigate(`/boards/${encodeURIComponent(id)}`)}
        onSaved={(c) => {
          patchCard(c)
          if (c.column !== columnOf(c.id)) reload()
        }}
      />
    </div>
  )
}

function BackLink() {
  const { navigate } = useApp()
  return (
    <button type="button" onClick={() => navigate("/boards")} className="-ml-0.5 flex items-center gap-0.5 text-xs text-subtle-foreground transition-colors hover:text-foreground">
      <ChevronLeft className="size-3.5" /> All boards
    </button>
  )
}

function Column({
  name,
  tone,
  ids,
  composing,
  onCompose,
  onAdd,
  children,
}: {
  name: string
  tone: string
  ids: string[]
  composing: boolean
  onCompose: (on: boolean) => void
  onAdd: (title: string) => void
  children: ReactNode
}) {
  const { setNodeRef, isOver } = useDroppable({ id: `col:${name}` })
  return (
    <section
      aria-label={`${name} column`}
      className={cn("flex max-h-[calc(100vh-15rem)] w-72 shrink-0 flex-col rounded-xl border bg-muted/40 transition-colors", isOver && "border-brand/50 bg-brand-soft/40")}
    >
      <header className="flex items-center gap-2 px-3 pb-2 pt-3">
        <span className="size-2 rounded-full" style={{ background: tone }} />
        <h2 className="truncate text-sm font-semibold">{name}</h2>
        <span className="rounded-full bg-background/80 px-1.5 text-2xs tabular-nums text-muted-foreground">{ids.length}</span>
        <button
          type="button"
          onClick={() => onCompose(true)}
          aria-label={`Add a card to ${name}`}
          title="Add a card"
          className="ml-auto inline-flex size-6 items-center justify-center rounded-md text-subtle-foreground hover:bg-accent hover:text-foreground"
        >
          <Plus className="size-3.5" />
        </button>
      </header>
      <div ref={setNodeRef} className="flex min-h-16 flex-1 flex-col gap-2 overflow-y-auto px-2 pb-2">
        <SortableContext items={ids} strategy={verticalListSortingStrategy}>
          {children}
        </SortableContext>
        {composing ? (
          <Composer onAdd={onAdd} onClose={() => onCompose(false)} />
        ) : (
          <button
            type="button"
            onClick={() => onCompose(true)}
            className="flex h-8 items-center gap-1.5 rounded-lg px-2 text-sm text-subtle-foreground transition-colors hover:bg-background/70 hover:text-foreground"
          >
            <Plus className="size-3.5" /> Add a card
          </button>
        )}
      </div>
    </section>
  )
}

function Composer({ onAdd, onClose }: { onAdd: (title: string) => void; onClose: () => void }) {
  const [v, setV] = useState("")
  const ref = useRef<HTMLTextAreaElement>(null)
  useEffect(() => ref.current?.focus(), [])
  const submit = () => {
    const t = v.trim()
    if (!t) return
    onAdd(t)
    setV("")
    ref.current?.focus()
  }
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
        placeholder="What needs doing? Enter to add"
        aria-label="Card title"
        className="w-full resize-none bg-transparent text-sm outline-none placeholder:text-subtle-foreground"
      />
      <div className="mt-1 flex items-center gap-1.5">
        <Button size="sm" onClick={submit} disabled={!v.trim()}>
          Add card
        </Button>
        <button type="button" onClick={onClose} aria-label="Close" className="inline-flex size-7 items-center justify-center rounded-md text-subtle-foreground hover:bg-accent hover:text-foreground">
          <X className="size-4" />
        </button>
      </div>
    </div>
  )
}

function SortableCard({ card, onOpen, onToggle }: { card: CardT; onOpen: () => void; onToggle: () => void }) {
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({ id: card.id })
  return (
    <div
      ref={setNodeRef}
      style={{ transform: CSS.Transform.toString(transform), transition }}
      className={cn(isDragging && "opacity-35")}
      {...attributes}
      {...listeners}
      onClick={() => Date.now() - lastDrop > 250 && onOpen()}
      onKeyDown={(e) => {
        listeners?.onKeyDown?.(e)
        if (e.key === "Enter" && !e.defaultPrevented) onOpen()
      }}
      aria-label={`Card: ${card.title}`}
    >
      <CardFace card={card} onToggle={onToggle} />
    </div>
  )
}

function CardFace({ card, onToggle, overlay }: { card: CardT; onToggle?: () => void; overlay?: boolean }) {
  const due = dueState(card.due)
  return (
    <article
      className={cn(
        "group cursor-grab rounded-lg border bg-card p-2.5 text-sm shadow-card transition-[border-color,box-shadow] hover:border-border-strong active:cursor-grabbing",
        overlay && "rotate-[1.5deg] cursor-grabbing border-border-strong shadow-pop",
      )}
    >
      {card.labels && card.labels.length > 0 && (
        <div className="mb-1.5 flex flex-wrap gap-1">
          {card.labels.map((l) => (
            <span key={l} className="rounded px-1.5 py-px text-2xs font-medium" style={{ background: `color-mix(in oklch, ${labelColor(l)} 16%, transparent)`, color: labelColor(l) }}>
              {l}
            </span>
          ))}
        </div>
      )}
      <div className="flex items-start gap-2">
        <button
          type="button"
          aria-label={card.done ? "Mark as not done" : "Mark as done"}
          onPointerDown={(e) => e.stopPropagation()}
          onClick={(e) => {
            e.stopPropagation()
            onToggle?.()
          }}
          className={cn(
            "mt-0.5 flex size-4 shrink-0 items-center justify-center rounded-full border transition-colors",
            card.done ? "border-success bg-success text-white" : "border-border-strong hover:border-success",
          )}
        >
          {card.done && <Check className="size-3" strokeWidth={3} />}
        </button>
        <span className={cn("min-w-0 flex-1 leading-snug", card.done && "text-subtle-foreground line-through")}>{card.title}</span>
      </div>
      {(card.project || card.due || card.memory || card.council || card.work || card.linear || card.trello) && (
        <div className="mt-2 flex flex-wrap items-center gap-1.5 pl-6 text-2xs text-muted-foreground">
          {card.project && <span className="rounded border bg-background/60 px-1.5 py-px font-mono">{card.project}</span>}
          {card.due && (
            <span
              className={cn(
                "flex items-center gap-1 rounded px-1 py-px",
                due === "overdue" && !card.done && "bg-danger-soft text-danger-fg",
                due === "today" && !card.done && "bg-warning-soft text-warning-fg",
              )}
            >
              <CalendarDays className="size-3" />
              {formatDue(card.due)}
            </span>
          )}
          {card.memory && (
            <span title={`Brief: ${card.memory}`} className="flex items-center">
              <FileText className="size-3" />
            </span>
          )}
          {card.council && (
            <span title="Council session" className="flex items-center">
              <Vote className="size-3" />
            </span>
          )}
          {card.work && (
            <span title="Work session" className="flex items-center">
              <SquareTerminal className="size-3" />
            </span>
          )}
          {card.linear && (
            <span title="Linked Linear issue" className="rounded border bg-background/60 px-1.5 py-px font-mono">
              {card.linear}
            </span>
          )}
          {card.trello && (
            <span title="Linked Trello card" className="flex items-center">
              <LayoutList className="size-3" />
            </span>
          )}
        </div>
      )}
    </article>
  )
}
