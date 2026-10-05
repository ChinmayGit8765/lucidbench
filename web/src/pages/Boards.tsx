import { useState } from "react"
import { ArrowRight, Plus, SquareKanban } from "lucide-react"
import { toast } from "sonner"

import { PageHeader, RefreshButton } from "@/components/Shell"
import { Button } from "@/components/ui/button"
import { Card } from "@/components/ui/card"
import { Dialog } from "@/components/ui/dialog"
import { EmptyState, ErrorState, Skeleton } from "@/components/ui/states"
import { errorMessage, refreshAll, usePoll } from "@/lib/api"
import { useApp } from "@/lib/app"
import { BOARDS_POLL_MS, createBoard, DEFAULT_BOARD, type BoardSummary } from "@/lib/boards"
import { relativeTime, useNow } from "@/lib/time"
import type { ModulePageProps } from "@/modules/types"
import { Kanban } from "@/pages/boards/Kanban"

export default function Boards({ subpath }: ModulePageProps) {
  if (subpath[0]) return <Kanban key={subpath[0]} id={subpath[0]} cardId={subpath[1] ?? null} />
  return <BoardList />
}

function BoardList() {
  const { open } = useApp()
  const now = useNow(30000)
  const poll = usePoll<BoardSummary[]>("/api/boards", BOARDS_POLL_MS)
  const [creating, setCreating] = useState(false)
  const list = poll.data ?? []
  const hasWork = list.some((b) => b.id === DEFAULT_BOARD)
  return (
    <div className="space-y-6">
      <PageHeader
        icon={<SquareKanban />}
        title="Boards"
        description="Task boards for your projects. Each board is a Kanban file in your Memory vault, so the Obsidian Kanban plugin opens it too."
        actions={
          <>
            <RefreshButton refreshing={poll.refreshing} updatedAt={poll.updatedAt} />
            <Button size="sm" onClick={() => setCreating(true)}>
              <Plus /> New board
            </Button>
          </>
        }
      />
      {poll.error && !poll.data ? (
        <ErrorState title="Could not read your boards" message={poll.error.message} onRetry={poll.refresh} />
      ) : poll.loading && !poll.data ? (
        <div className="grid gap-3 @3xl:grid-cols-3">
          {[0, 1, 2].map((i) => (
            <Skeleton key={i} className="h-32 rounded-xl" />
          ))}
        </div>
      ) : (
        <div className="grid gap-3 @2xl:grid-cols-2 @4xl:grid-cols-3">
          {!hasWork && (
            <button type="button" onClick={() => open("boards", [DEFAULT_BOARD])} className="text-left">
              <Card className="group flex h-full flex-col border-dashed p-4 transition-colors hover:border-border-strong">
                <div className="flex items-center gap-2 text-sm font-semibold">
                  <SquareKanban className="size-4 text-brand" /> Work
                </div>
                <p className="mt-1 text-sm text-muted-foreground">The default board: Inbox, Ready, In progress, Review and Done. Created when you open it.</p>
                <span className="mt-auto flex items-center gap-1 pt-3 text-xs text-brand-fg">
                  Open the work board <ArrowRight className="size-3.5" />
                </span>
              </Card>
            </button>
          )}
          {list.map((b) => (
            <button key={b.id} type="button" onClick={() => open("boards", [b.id])} className="text-left">
              <Card className="group relative flex h-full flex-col overflow-hidden p-4 transition-[border-color,box-shadow] hover:border-border-strong">
                <div aria-hidden className="tile-glow pointer-events-none absolute inset-0" />
                <div className="relative flex items-center gap-2">
                  <span className="flex size-7 items-center justify-center rounded-md border bg-background/60 text-subtle-foreground">
                    <SquareKanban className="size-3.5" />
                  </span>
                  <span className="truncate text-sm font-semibold">{b.title}</span>
                  <ArrowRight className="ml-auto size-3.5 text-subtle-foreground opacity-0 transition-opacity group-hover:opacity-100" />
                </div>
                <div className="relative mt-4 flex items-end gap-4">
                  <div>
                    <div className="text-2xl font-semibold tabular-nums tracking-tight">{b.cards}</div>
                    <div className="text-xs text-muted-foreground">{b.cards === 1 ? "card" : "cards"}</div>
                  </div>
                  <div>
                    <div className="text-2xl font-semibold tabular-nums tracking-tight text-muted-foreground">{b.columns}</div>
                    <div className="text-xs text-muted-foreground">columns</div>
                  </div>
                </div>
                <div className="relative mt-3 flex items-center gap-1.5 text-2xs text-subtle-foreground">
                  <span className="font-mono">Boards/{b.id}.md</span>
                  {b.modified && <span>· edited {relativeTime(b.modified, now)}</span>}
                </div>
              </Card>
            </button>
          ))}
          {list.length === 0 && hasWork === false && poll.data && (
            <Card className="border-dashed @2xl:col-span-1">
              <EmptyState icon={<SquareKanban />} title="No other boards yet" description="Make one per project or per kind of work." className="py-6">
                <Button size="sm" variant="secondary" onClick={() => setCreating(true)}>
                  <Plus /> New board
                </Button>
              </EmptyState>
            </Card>
          )}
        </div>
      )}
      <NewBoardDialog
        open={creating}
        onClose={() => setCreating(false)}
        onCreated={(id) => {
          setCreating(false)
          refreshAll()
          open("boards", [id])
        }}
      />
    </div>
  )
}

function NewBoardDialog({ open, onClose, onCreated }: { open: boolean; onClose: () => void; onCreated: (id: string) => void }) {
  const [title, setTitle] = useState("")
  const [columns, setColumns] = useState("To do, Doing, Done")
  const [busy, setBusy] = useState(false)
  const submit = async () => {
    const cols = columns.split(",").map((c) => c.trim()).filter(Boolean)
    if (!title.trim() || cols.length === 0) return
    setBusy(true)
    try {
      const id = await createBoard(title.trim(), cols)
      toast.success(`Board “${title.trim()}” created`)
      setTitle("")
      onCreated(id)
    } catch (e) {
      toast.error("Could not create the board", { description: errorMessage(e) })
    } finally {
      setBusy(false)
    }
  }
  return (
    <Dialog open={open} onClose={onClose} title="New board" description="Saved as a Kanban file in the Boards folder of your vault.">
      <form
        className="space-y-4"
        onSubmit={(e) => {
          e.preventDefault()
          void submit()
        }}
      >
        <div>
          <label htmlFor="board-title" className="text-xs font-medium text-muted-foreground">
            Name
          </label>
          <input
            id="board-title"
            autoFocus
            value={title}
            onChange={(e) => setTitle(e.target.value)}
            placeholder="Launch plan"
            className="mt-1.5 h-9 w-full rounded-lg border bg-background px-3 text-sm outline-none focus-visible:border-border-strong focus-visible:ring-2 focus-visible:ring-ring/40"
          />
        </div>
        <div>
          <label htmlFor="board-columns" className="text-xs font-medium text-muted-foreground">
            Columns, separated by commas
          </label>
          <input
            id="board-columns"
            value={columns}
            onChange={(e) => setColumns(e.target.value)}
            className="mt-1.5 h-9 w-full rounded-lg border bg-background px-3 text-sm outline-none focus-visible:border-border-strong focus-visible:ring-2 focus-visible:ring-ring/40"
          />
        </div>
        <div className="flex justify-end gap-2">
          <Button type="button" variant="secondary" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" disabled={busy || !title.trim()}>
            {busy ? "Creating" : "Create board"}
          </Button>
        </div>
      </form>
    </Dialog>
  )
}
