import { useEffect, useRef, useState, type DragEvent } from "react"
import {
  ChevronRight,
  Ellipsis,
  FilePlus2,
  FileText,
  Folder,
  FolderOpen,
  FolderPlus,
  Lock,
  Pencil,
  Plus,
  SquareKanban,
  Trash2,
} from "lucide-react"

import { Menu } from "@/components/ui/menu"
import { baseName, FOLDER_FILE, type Entry } from "@/lib/memory"
import { cn } from "@/lib/utils"

const DRAG_TYPE = "application/x-lucid-memory-path"

export interface TreeActions {
  toggle: (dir: string) => void
  open: (e: Entry) => void
  newPage: (dir: string) => void
  newFolder: (dir: string) => void
  rename: (e: Entry, name: string) => void
  trash: (e: Entry) => void
  move: (from: string, toDir: string) => void
}

/** Is this the board folder's file, opened in Boards rather than the editor? */
export const isBoardFile = (path: string) => /^Boards\/[a-z0-9][a-z0-9-]*\.md$/.test(path)

/** The page tree: folders first, lazy-loaded, drag a row onto a folder to move it. */
export function Tree({
  dirs,
  expanded,
  current,
  actions,
}: {
  dirs: Map<string, Entry[]>
  expanded: Set<string>
  current: string | null
  actions: TreeActions
}) {
  const [renaming, setRenaming] = useState<string | null>(null)
  const [over, setOver] = useState<string | null>(null)
  const root = dirs.get("")

  const onDropTo = (dir: string) => (e: DragEvent) => {
    e.preventDefault()
    e.stopPropagation()
    setOver(null)
    const from = e.dataTransfer.getData(DRAG_TYPE)
    if (from) actions.move(from, dir)
  }

  const renderDir = (dir: string, depth: number) => {
    const ents = (dirs.get(dir) ?? []).filter((e) => e.name !== FOLDER_FILE)
    if (ents.length === 0 && depth > 0) {
      return (
        <div className="py-1 text-xs text-subtle-foreground" style={{ paddingLeft: 30 + depth * 14 }}>
          Empty folder
        </div>
      )
    }
    return ents.map((e) => {
      const open = e.dir && expanded.has(e.path)
      const board = !e.dir && isBoardFile(e.path)
      const label = e.title || (e.dir ? e.name : baseName(e.path))
      const menu = [
        ...(e.dir
          ? [
              { label: "New page inside", icon: FilePlus2, onSelect: () => actions.newPage(e.path) },
              { label: "New folder inside", icon: FolderPlus, onSelect: () => actions.newFolder(e.path) },
            ]
          : []),
        { label: "Rename", icon: Pencil, onSelect: () => setRenaming(e.path) },
        { label: "Move to trash", icon: Trash2, danger: true, onSelect: () => actions.trash(e) },
      ]
      return (
        <div key={e.path}>
          <div
            draggable={renaming !== e.path}
            onDragStart={(ev) => {
              ev.dataTransfer.setData(DRAG_TYPE, e.path)
              ev.dataTransfer.effectAllowed = "move"
            }}
            onDragOver={
              e.dir
                ? (ev) => {
                    if (!ev.dataTransfer.types.includes(DRAG_TYPE)) return
                    ev.preventDefault()
                    ev.stopPropagation()
                    setOver(e.path)
                  }
                : undefined
            }
            onDragLeave={e.dir ? () => setOver((o) => (o === e.path ? null : o)) : undefined}
            onDrop={e.dir ? onDropTo(e.path) : undefined}
            className={cn(
              "group relative flex h-7 items-center gap-1 rounded-md pr-1 text-sm transition-colors",
              current === e.path ? "bg-accent font-medium text-foreground" : "text-muted-foreground hover:bg-accent/60 hover:text-foreground",
              over === e.path && "bg-brand-soft ring-1 ring-brand/50",
            )}
            style={{ paddingLeft: 4 + depth * 14 }}
          >
            {e.dir ? (
              <button
                type="button"
                aria-label={open ? `Collapse ${label}` : `Expand ${label}`}
                onClick={() => actions.toggle(e.path)}
                className="flex size-5 shrink-0 items-center justify-center rounded text-subtle-foreground hover:bg-accent hover:text-foreground"
              >
                <ChevronRight className={cn("size-3.5 transition-transform duration-150", open && "rotate-90")} />
              </button>
            ) : (
              <span className="w-5 shrink-0" />
            )}
            {renaming === e.path ? (
              <RenameInput
                initial={e.dir ? e.name : baseName(e.path)}
                onDone={(name) => {
                  setRenaming(null)
                  if (name && name !== (e.dir ? e.name : baseName(e.path))) actions.rename(e, name)
                }}
              />
            ) : (
              <button
                type="button"
                onClick={() => (e.dir ? actions.toggle(e.path) : actions.open(e))}
                onDoubleClick={() => setRenaming(e.path)}
                className="flex min-w-0 flex-1 items-center gap-1.5 text-left outline-none"
                title={e.path}
              >
                <span className="flex size-[18px] shrink-0 items-center justify-center text-[15px] leading-none">
                  {e.icon ? (
                    e.icon
                  ) : e.dir ? (
                    open ? (
                      <FolderOpen className="size-4 text-subtle-foreground" />
                    ) : (
                      <Folder className="size-4 text-subtle-foreground" />
                    )
                  ) : board ? (
                    <SquareKanban className="size-4 text-subtle-foreground" />
                  ) : (
                    <FileText className="size-4 text-subtle-foreground" />
                  )}
                </span>
                <span className="truncate">{label}</span>
                {e.confidential && <Lock className="size-3 shrink-0 text-warning-fg" aria-label="Confidential" />}
              </button>
            )}
            {renaming !== e.path && (
              <span className="flex shrink-0 items-center opacity-0 transition-opacity focus-within:opacity-100 group-hover:opacity-100">
                <Menu label={`${label} actions`} trigger={<Ellipsis />} items={menu} />
                {e.dir && (
                  <button
                    type="button"
                    aria-label={`New page in ${label}`}
                    title="New page inside"
                    onClick={() => actions.newPage(e.path)}
                    className="inline-flex size-7 items-center justify-center rounded-md text-subtle-foreground hover:bg-accent hover:text-foreground"
                  >
                    <Plus className="size-4" />
                  </button>
                )}
              </span>
            )}
          </div>
          {open && (dirs.has(e.path) ? renderDir(e.path, depth + 1) : <div className="h-7" style={{ marginLeft: 30 + depth * 14 }} />)}
        </div>
      )
    })
  }

  return (
    <div
      className={cn("min-h-full rounded-md pb-6", over === "" && "bg-brand-soft/50")}
      onDragOver={(ev) => {
        if (!ev.dataTransfer.types.includes(DRAG_TYPE)) return
        ev.preventDefault()
        setOver("")
      }}
      onDragLeave={(ev) => {
        if (ev.currentTarget === ev.target) setOver((o) => (o === "" ? null : o))
      }}
      onDrop={onDropTo("")}
    >
      {root ? renderDir("", 0) : null}
    </div>
  )
}

function RenameInput({ initial, onDone }: { initial: string; onDone: (name: string) => void }) {
  const [v, setV] = useState(initial)
  const ref = useRef<HTMLInputElement>(null)
  const done = useRef(false)
  const finish = (name: string) => {
    if (done.current) return
    done.current = true
    onDone(name)
  }
  useEffect(() => {
    ref.current?.focus()
    ref.current?.select()
  }, [])
  return (
    <input
      ref={ref}
      value={v}
      aria-label="New name"
      onChange={(e) => setV(e.target.value)}
      onBlur={() => finish(v.trim())}
      onKeyDown={(e) => {
        if (e.key === "Enter") finish(v.trim())
        if (e.key === "Escape") finish(initial)
      }}
      className="h-6 min-w-0 flex-1 rounded border border-brand/60 bg-background px-1.5 text-sm text-foreground outline-none"
    />
  )
}
