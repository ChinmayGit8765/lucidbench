import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import { FilePlus2, FileText, FolderPlus, Lock, Search, SquareKanban, X } from "lucide-react"
import { toast } from "sonner"

import { Button } from "@/components/ui/button"
import { ConfirmDialog, type ConfirmRequest } from "@/components/ui/confirm"
import { Dialog } from "@/components/ui/dialog"
import { ErrorState, Skeleton } from "@/components/ui/states"
import { ApiError, errorMessage } from "@/lib/api"
import { useApp } from "@/lib/app"
import {
  allPages,
  baseName,
  dirOf,
  FOLDER_FILE,
  joinPath,
  memoryApi,
  onMemoryIntent,
  pageRoute,
  safeName,
  takeMemoryIntent,
  type Entry,
  type Hit,
  type MemoryIntent,
  type VaultInfo,
} from "@/lib/memory"
import { cn } from "@/lib/utils"
import type { ModulePageProps } from "@/modules/types"
import type { LinkItem } from "@/pages/memory/Editor"
import { PageView } from "@/pages/memory/PageView"
import { Home, Onboarding } from "@/pages/memory/Start"
import { isBoardFile, Tree, type TreeActions } from "@/pages/memory/Tree"

const EXPANDED_KEY = "lucidbench.memory.expanded"

function loadExpanded(): Set<string> {
  try {
    return new Set(JSON.parse(localStorage.getItem(EXPANDED_KEY) ?? "[]") as string[])
  } catch {
    return new Set()
  }
}

/** The first free "<base>.md" (or folder) name in a listing. */
function freeName(ents: Entry[], base: string, ext: string) {
  const taken = new Set(ents.map((e) => e.name.toLowerCase()))
  for (let i = 1; ; i++) {
    const name = `${i === 1 ? base : `${base} ${i}`}${ext}`
    if (!taken.has(name.toLowerCase())) return name
  }
}

export default function Memory({ subpath }: ModulePageProps) {
  const { navigate, open } = useApp()
  const current = subpath.length > 0 ? subpath.join("/") : null
  const [info, setInfo] = useState<VaultInfo | null>(null)
  const [infoError, setInfoError] = useState<ApiError | null>(null)
  const [dirs, setDirs] = useState<Map<string, Entry[]>>(new Map())
  const [expanded, setExpanded] = useState<Set<string>>(loadExpanded)
  const [fresh, setFresh] = useState<string | null>(null)
  const [query, setQuery] = useState("")
  const [hits, setHits] = useState<Hit[] | null>(null)
  const [confirm, setConfirm] = useState<ConfirmRequest | null>(null)
  const [folderIn, setFolderIn] = useState<string | null>(null)
  const searchRef = useRef<HTMLInputElement>(null)
  const dirsRef = useRef(dirs)
  dirsRef.current = dirs

  useEffect(() => localStorage.setItem(EXPANDED_KEY, JSON.stringify([...expanded])), [expanded])

  const loadDir = useCallback(async (dir: string) => {
    try {
      const ents = await memoryApi.tree(dir)
      setDirs((m) => new Map(m).set(dir, ents))
      return ents
    } catch (e) {
      if (e instanceof ApiError && e.status === 404) {
        setDirs((m) => {
          const n = new Map(m)
          n.delete(dir)
          return n
        })
        setExpanded((s) => {
          const n = new Set(s)
          n.delete(dir)
          return n
        })
      }
      return []
    }
  }, [])

  const refresh = useCallback(async () => {
    try {
      setInfo(await memoryApi.info())
      setInfoError(null)
    } catch (e) {
      setInfoError(e instanceof ApiError ? e : new ApiError(0, String(e)))
      return
    }
    await Promise.all(["", ...[...dirsRef.current.keys()].filter((d) => d !== "")].map(loadDir))
  }, [loadDir])

  useEffect(() => {
    void refresh()
  }, [refresh])

  // Open the folders above the current page, and load every open folder.
  useEffect(() => {
    if (!current) return
    const parts = dirOf(current).split("/").filter(Boolean)
    if (parts.length === 0) return
    setExpanded((s) => {
      const n = new Set(s)
      parts.forEach((_, i) => n.add(parts.slice(0, i + 1).join("/")))
      return n
    })
  }, [current])
  useEffect(() => {
    for (const d of expanded) if (!dirs.has(d)) void loadDir(d)
  }, [expanded, dirs, loadDir])

  const openPath = useCallback((p: string) => navigate(pageRoute(p)), [navigate])

  /* ---------- actions ---------- */

  const newPage = useCallback(
    async (dir: string) => {
      try {
        const ents = dirsRef.current.get(dir) ?? (await memoryApi.tree(dir).catch(() => []))
        const path = joinPath(dir, freeName(ents, "Untitled", ".md"))
        await memoryApi.save(path, { front: {}, body: "" })
        if (dir) setExpanded((s) => new Set(s).add(dir))
        await refresh()
        setFresh(path)
        openPath(path)
      } catch (e) {
        toast.error("Could not create the page", { description: errorMessage(e) })
      }
    },
    [refresh, openPath],
  )

  const createFolder = async (dir: string, name: string) => {
    const clean = safeName(name)
    const folder = joinPath(dir, clean)
    try {
      await memoryApi.save(joinPath(folder, FOLDER_FILE), { front: {}, body: "" })
      setExpanded((s) => new Set(s).add(folder).add(dir))
      await refresh()
    } catch (e) {
      toast.error("Could not create the folder", { description: errorMessage(e) })
    }
  }

  /** Follows a moved page or folder: the URL and the open folders. */
  const followMove = (from: string, to: string) => {
    setExpanded((s) => new Set([...s].map((d) => (d === from || d.startsWith(`${from}/`) ? to + d.slice(from.length) : d))))
    if (current && (current === from || current.startsWith(`${from}/`))) openPath(to + current.slice(from.length))
  }

  const move = async (from: string, to: string) => {
    if (from === to) return true
    try {
      await memoryApi.move(from, to)
      followMove(from, to)
      await refresh()
      return true
    } catch (e) {
      toast.error(e instanceof ApiError && e.status === 409 ? "That name is already taken" : "Could not move it", { description: errorMessage(e) })
      return false
    }
  }

  const actions: TreeActions = {
    toggle: (dir) =>
      setExpanded((s) => {
        const n = new Set(s)
        if (n.has(dir)) n.delete(dir)
        else n.add(dir)
        return n
      }),
    open: (e) => {
      const m = /^Boards\/(.+)\.md$/.exec(e.path)
      if (m && isBoardFile(e.path)) open("boards", [m[1]])
      else openPath(e.path)
    },
    newPage: (dir) => void newPage(dir),
    newFolder: (dir) => setFolderIn(dir),
    rename: (e, name) => void move(e.path, joinPath(dirOf(e.path), e.dir ? safeName(name) : `${safeName(name)}.md`)),
    trash: (e) =>
      setConfirm({
        title: `Move “${e.title || (e.dir ? e.name : baseName(e.path))}” to the trash?`,
        description: `It moves to .trash in your vault${e.dir ? ", with everything inside it" : ""}. Restore it from there with any file manager.`,
        confirmLabel: "Move to trash",
        danger: true,
        run: async () => {
          try {
            await memoryApi.trash(e.path)
            toast.success("Moved to the trash", {
              description: e.dir ? e.name : baseName(e.path),
              action: {
                label: "Undo",
                onClick: () =>
                  void memoryApi
                    .restore(e.path)
                    .then(() => {
                      toast.success("Restored", { description: e.path })
                      return refresh()
                    })
                    .catch((err) => toast.error("Could not restore it", { description: errorMessage(err) })),
              },
            })
            if (current && (current === e.path || current.startsWith(`${e.path}/`))) navigate("/memory")
            await refresh()
          } catch (err) {
            toast.error("Could not move it to the trash", { description: errorMessage(err) })
          }
        },
      }),
    move: (from, toDir) => {
      const name = from.split("/").pop()!
      void move(from, joinPath(toDir, name))
    },
  }

  /* ---------- links ---------- */

  const findPages = useCallback(async (q: string, exclude: string): Promise<LinkItem[]> => {
    const item = (path: string, title?: string, icon?: string): LinkItem => ({
      target: baseName(path),
      label: title || baseName(path),
      hint: dirOf(path) || "Memory",
      icon,
    })
    let items: LinkItem[]
    if (!q.trim()) {
      const pages = await allPages(400)
      items = [...pages]
        .filter((p) => p.path !== exclude)
        .sort((a, b) => b.modified.localeCompare(a.modified))
        .slice(0, 8)
        .map((p) => item(p.path, p.title, p.icon))
    } else {
      const found = await memoryApi.search(q, 12)
      items = found.filter((h) => h.path !== exclude && !h.path.startsWith("Boards/")).slice(0, 8).map((h) => item(h.path, h.title))
      const exact = found.some((h) => baseName(h.path).toLowerCase() === q.trim().toLowerCase())
      if (!exact) items.push({ target: safeName(q), label: `Create “${safeName(q)}”`, hint: "New page in Memory", create: true })
    }
    return items
  }, [])

  const createLinked = useCallback(
    async (it: LinkItem) => {
      try {
        await memoryApi.save(`${safeName(it.target)}.md`, { front: {}, body: "" })
        await refresh()
        toast.success(`Created “${it.target}”`)
      } catch (e) {
        if (!(e instanceof ApiError && e.status === 409)) toast.error("Could not create the page", { description: errorMessage(e) })
      }
    },
    [refresh],
  )

  /** Finds the page a [[target]] names, the way Obsidian does: by path, else by file name. */
  const openLink = useCallback(
    async (target: string) => {
      const name = target.split("|")[0].split("#")[0].trim().replace(/\.md$/i, "")
      if (!name) return
      if (name.includes("/")) {
        try {
          await memoryApi.page(`${name}.md`)
          openPath(`${name}.md`)
          return
        } catch {
          /* fall through to a name search */
        }
      }
      const short = name.split("/").pop()!.toLowerCase()
      const hits = await memoryApi.search(short, 50).catch(() => [] as Hit[])
      const found = hits.find((h) => baseName(h.path).toLowerCase() === short) ?? (await allPages(800)).find((p) => baseName(p.path).toLowerCase() === short)
      if (found) {
        openPath(found.path)
        return
      }
      // Like Obsidian, following a link to a missing page creates it.
      const path = `${safeName(name.split("/").pop()!)}.md`
      try {
        await memoryApi.save(path, { front: {}, body: "" })
        await refresh()
        toast.success(`Created “${baseName(path)}”`)
        openPath(path)
      } catch (e) {
        toast.error("Could not open the link", { description: errorMessage(e) })
      }
    },
    [openPath, refresh],
  )

  const renameUntitled = useCallback(
    async (from: string, title: string) => {
      const to = joinPath(dirOf(from), `${safeName(title)}.md`)
      if (to === from) return null
      try {
        await memoryApi.move(from, to)
        await refresh()
        return to
      } catch {
        return null // the name is taken: keep the file name, the title still shows
      }
    },
    [refresh],
  )

  /* ---------- search ---------- */

  useEffect(() => {
    const q = query.trim()
    if (!q) {
      setHits(null)
      return
    }
    let cancelled = false
    const id = setTimeout(() => {
      memoryApi
        .search(q, 30)
        .then((h) => !cancelled && setHits(h))
        .catch(() => !cancelled && setHits([]))
    }, 180)
    return () => {
      cancelled = true
      clearTimeout(id)
    }
  }, [query])

  /* ---------- palette intents ---------- */

  const handleIntent = useCallback(
    (i: MemoryIntent | null) => {
      if (!i) return
      if (i.kind === "new-page") void newPage("")
      if (i.kind === "search") {
        navigate("/memory")
        setTimeout(() => searchRef.current?.focus(), 30)
      }
    },
    [newPage, navigate],
  )
  useEffect(() => {
    handleIntent(takeMemoryIntent())
    return onMemoryIntent(() => handleIntent(takeMemoryIntent()))
  }, [handleIntent])

  const onOpenFolder = (dir: string) => {
    if (!dir) {
      navigate("/memory")
      return
    }
    const parts = dir.split("/")
    setExpanded((s) => {
      const n = new Set(s)
      parts.forEach((_, i) => n.add(parts.slice(0, i + 1).join("/")))
      return n
    })
  }

  const root = dirs.get("")
  const empty = info !== null && info.pages === 0
  const main = useMemo(() => {
    if (current && isBoardFile(current)) return null
    return current
  }, [current])
  useEffect(() => {
    if (current && isBoardFile(current)) open("boards", [baseName(current)])
  }, [current, open])

  if (infoError && !info) {
    return (
      <ErrorState
        title="Could not open the Memory vault"
        message={infoError.message}
        onRetry={() => void refresh()}
      />
    )
  }

  return (
    <div className="-my-2 flex h-[calc(100vh-7.5rem)] min-h-[32rem] overflow-hidden rounded-xl border bg-card shadow-card">
      <aside className="flex w-64 shrink-0 flex-col border-r bg-sidebar/50">
        <div className="flex items-center gap-1 px-3 pb-2 pt-3">
          <span className="flex-1 truncate text-xs font-semibold uppercase tracking-[0.08em] text-subtle-foreground">Pages</span>
          <button
            type="button"
            title="New folder"
            aria-label="New folder"
            onClick={() => setFolderIn("")}
            className="inline-flex size-7 items-center justify-center rounded-md text-subtle-foreground hover:bg-accent hover:text-foreground"
          >
            <FolderPlus className="size-4" />
          </button>
          <button
            type="button"
            title="New page"
            aria-label="New page"
            onClick={() => void newPage("")}
            className="inline-flex size-7 items-center justify-center rounded-md text-subtle-foreground hover:bg-accent hover:text-foreground"
          >
            <FilePlus2 className="size-4" />
          </button>
        </div>
        <div className="px-3 pb-2">
          <label className="flex h-8 items-center gap-2 rounded-lg border bg-background/70 px-2.5 text-sm transition-colors focus-within:border-border-strong focus-within:ring-2 focus-within:ring-ring/30">
            <Search className="size-3.5 shrink-0 text-subtle-foreground" />
            <input
              ref={searchRef}
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Escape") setQuery("")
                if (e.key === "Enter" && hits?.[0]) openPath(hits[0].path)
              }}
              placeholder="Search memory…"
              aria-label="Search memory"
              className="min-w-0 flex-1 bg-transparent outline-none placeholder:text-subtle-foreground"
            />
            {query && (
              <button type="button" aria-label="Clear search" onClick={() => setQuery("")} className="text-subtle-foreground hover:text-foreground">
                <X className="size-3.5" />
              </button>
            )}
          </label>
        </div>
        <div className="min-h-0 flex-1 overflow-y-auto px-2">
          {query.trim() ? (
            <SearchResults hits={hits} query={query.trim()} current={current} openPath={openPath} />
          ) : !root ? (
            <div className="space-y-1.5 px-1 pt-1">
              {[0, 1, 2, 3].map((i) => (
                <Skeleton key={i} className="h-6" />
              ))}
            </div>
          ) : (
            <Tree dirs={dirs} expanded={expanded} current={current} actions={actions} />
          )}
        </div>
        {info && (
          <div className="border-t px-3 py-2 text-2xs text-subtle-foreground">
            <div className="truncate font-mono" title={info.root}>
              {info.root}
            </div>
            <div className="mt-0.5">
              {info.pages} {info.pages === 1 ? "page" : "pages"} · trash is .trash
            </div>
          </div>
        )}
      </aside>

      <section className="relative min-w-0 flex-1 overflow-y-auto" aria-label="Page">
        {main ? (
          <PageView
            key={main}
            path={main}
            vaultRoot={info?.root.replace(/\\/g, "/") ?? null}
            fresh={fresh === main}
            onOpenLink={(t) => void openLink(t)}
            findPages={findPages}
            onCreatePage={createLinked}
            onMeta={() => void refresh()}
            onRename={renameUntitled}
            onTrash={(p) => actions.trash({ name: p.split("/").pop()!, path: p, dir: false, size: 0, modified: "", confidential: false })}
            onOpenFolder={onOpenFolder}
            openPath={openPath}
          />
        ) : info === null ? (
          <div className="mx-auto max-w-2xl space-y-3 px-10 py-14">
            <Skeleton className="h-14 w-14 rounded-2xl" />
            <Skeleton className="h-9 w-80" />
            <Skeleton className="h-4 w-full" />
          </div>
        ) : empty ? (
          <Onboarding root={info.root} onNewPage={() => void newPage("")} onUseVault={() => navigate("/settings/general/vault")} />
        ) : (
          <Home pages={info.pages} onNewPage={() => void newPage("")} onSearch={() => searchRef.current?.focus()} openPath={openPath} />
        )}
      </section>

      <ConfirmDialog request={confirm} onClose={() => setConfirm(null)} />
      <NameDialog
        open={folderIn !== null}
        title="New folder"
        label="Folder name"
        initial={freeName(dirs.get(folderIn ?? "") ?? [], "New folder", "")}
        onClose={() => setFolderIn(null)}
        onSubmit={(name) => {
          const dir = folderIn ?? ""
          setFolderIn(null)
          void createFolder(dir, name)
        }}
      />
    </div>
  )
}

/** A search snippet as text: links, headings, emphasis and task boxes without their Markdown. */
function plainSnippet(s: string): string {
  return s
    .replace(/!?\[\[([^\]|]+)\|([^\]]+)\]\]/g, "$2")
    .replace(/!?\[\[([^\]]+)\]\]/g, "$1")
    .replace(/(^|\s)#{1,6}\s/g, "$1")
    .replace(/(^|\s)[-*] \[[ xX]\]\s/g, "$1")
    .replace(/\*\*|__|`/g, "")
    .replace(/\s+/g, " ")
}

function SearchResults({ hits, query, current, openPath }: { hits: Hit[] | null; query: string; current: string | null; openPath: (p: string) => void }) {
  if (hits === null) {
    return (
      <div className="space-y-2 px-1 pt-1">
        <Skeleton className="h-10" />
        <Skeleton className="h-10" />
      </div>
    )
  }
  if (hits.length === 0) return <p className="px-2 py-3 text-sm text-muted-foreground">Nothing matches “{query}”.</p>
  const mark = (s: string) => {
    const i = s.toLowerCase().indexOf(query.toLowerCase())
    if (i < 0) return s
    return (
      <>
        {s.slice(0, i)}
        <mark className="rounded-sm bg-brand-soft px-px text-foreground">{s.slice(i, i + query.length)}</mark>
        {s.slice(i + query.length)}
      </>
    )
  }
  return (
    <ul className="space-y-0.5 pb-4">
      <li className="px-2 pb-1 pt-1 text-2xs font-medium uppercase tracking-[0.08em] text-subtle-foreground">
        {hits.length} {hits.length === 1 ? "result" : "results"}
      </li>
      {hits.map((h) => {
        const board = isBoardFile(h.path)
        const Icon = board ? SquareKanban : FileText
        return (
          <li key={h.path}>
            <button
              type="button"
              onClick={() => openPath(h.path)}
              className={cn("w-full rounded-md px-2 py-1.5 text-left transition-colors hover:bg-accent/60", current === h.path && "bg-accent")}
            >
              <span className="flex items-center gap-1.5 text-sm font-medium">
                <Icon className="size-3.5 shrink-0 text-subtle-foreground" />
                <span className="truncate">{mark(h.title)}</span>
                {h.confidential && <Lock className="size-3 shrink-0 text-warning-fg" />}
              </span>
              {h.snippet && !board && <span className="mt-0.5 line-clamp-2 block text-xs text-muted-foreground">{mark(plainSnippet(h.snippet))}</span>}
              <span className="mt-0.5 block truncate text-2xs text-subtle-foreground">{board ? "Board" : dirOf(h.path) || "Memory"}</span>
            </button>
          </li>
        )
      })}
    </ul>
  )
}

function NameDialog({
  open,
  title,
  label,
  initial,
  onClose,
  onSubmit,
}: {
  open: boolean
  title: string
  label: string
  initial: string
  onClose: () => void
  onSubmit: (name: string) => void
}) {
  const [v, setV] = useState(initial)
  useEffect(() => {
    if (open) setV(initial)
  }, [open, initial])
  return (
    <Dialog open={open} onClose={onClose} title={title}>
      <form
        onSubmit={(e) => {
          e.preventDefault()
          if (v.trim()) onSubmit(v.trim())
        }}
      >
        <label className="text-xs font-medium text-muted-foreground" htmlFor="lb-name">
          {label}
        </label>
        <input
          id="lb-name"
          autoFocus
          value={v}
          onChange={(e) => setV(e.target.value)}
          className="mt-1.5 h-9 w-full rounded-lg border bg-background px-3 text-sm outline-none focus-visible:border-border-strong focus-visible:ring-2 focus-visible:ring-ring/40"
        />
        <div className="mt-5 flex justify-end gap-2">
          <Button type="button" variant="secondary" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" disabled={!v.trim()}>
            Create
          </Button>
        </div>
      </form>
    </Dialog>
  )
}
