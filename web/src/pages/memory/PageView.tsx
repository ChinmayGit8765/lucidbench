import { lazy, Suspense, useCallback, useEffect, useRef, useState } from "react"
import {
  AlertTriangle,
  ArrowUpRight,
  Check,
  ChevronRight,
  Copy,
  Ellipsis,
  ExternalLink,
  FileText,
  ImagePlus,
  Link2,
  Loader2,
  Lock,

  SmilePlus,
  Trash2,
} from "lucide-react"


import { copyText } from "@/components/CopyCommand"
import { Menu } from "@/components/ui/menu"
import { ErrorState, Skeleton } from "@/components/ui/states"
import { ApiError, errorMessage } from "@/lib/api"
import { baseName, dirOf, memoryApi, type Page } from "@/lib/memory"
import { cn } from "@/lib/utils"
import type { LinkItem } from "@/pages/memory/Editor"
import { Properties } from "@/pages/memory/Properties"

const MemoryEditor = lazy(() => import("@/pages/memory/Editor"))

const SAVE_DELAY_MS = 700

/** Cover colours, by name, so the front matter stays readable. */
export const COVERS: Record<string, string> = {
  azure: "linear-gradient(120deg, oklch(0.62 0.15 245), oklch(0.6 0.17 285))",
  violet: "linear-gradient(120deg, oklch(0.58 0.18 295), oklch(0.62 0.17 330))",
  rose: "linear-gradient(120deg, oklch(0.66 0.16 10), oklch(0.7 0.14 50))",
  amber: "linear-gradient(120deg, oklch(0.78 0.14 75), oklch(0.7 0.16 45))",
  emerald: "linear-gradient(120deg, oklch(0.66 0.13 165), oklch(0.62 0.12 210))",
  slate: "linear-gradient(120deg, oklch(0.45 0.02 265), oklch(0.62 0.03 255))",
  dusk: "linear-gradient(120deg, oklch(0.32 0.06 280), oklch(0.5 0.13 320) 60%, oklch(0.7 0.14 40))",
}

const coverStyle = (cover: unknown) => (typeof cover === "string" ? COVERS[cover] ?? cover : undefined)

const EMOJI = [
  "📝", "📌", "💡", "🧠", "🎯", "🚀", "🛠️", "🧪", "📚", "🗂️", "🗒️", "📎",
  "✅", "⚡", "🔥", "🌱", "🌊", "🌙", "⭐", "🧩", "🎨", "🎧", "📈", "💬",
  "🔒", "🔑", "🐛", "🏗️", "📦", "🧭", "🗺️", "🏁", "❤️", "☕", "🍀", "🦊",
]

type SaveState = "saved" | "dirty" | "saving" | "error"

export interface PageViewProps {
  path: string
  vaultRoot: string | null
  /** A page created a moment ago: focus its title. */
  fresh: boolean
  onOpenLink: (target: string) => void
  findPages: (query: string, exclude: string) => Promise<LinkItem[]>
  onCreatePage: (item: LinkItem) => Promise<void>
  /** The title, icon or file name changed: refresh the tree. */
  onMeta: () => void
  onRename: (from: string, title: string) => Promise<string | null>
  onTrash: (path: string) => void
  onOpenFolder: (dir: string) => void
  openPath: (path: string) => void
}

/** One page: cover, icon, title, properties, the editor and its backlinks. */
export function PageView(props: PageViewProps) {
  const { path } = props
  const [page, setPage] = useState<Page | null>(null)
  const [error, setError] = useState<ApiError | null>(null)
  const [nonce, setNonce] = useState(0)

  useEffect(() => {
    let cancelled = false
    setPage(null)
    setError(null)
    memoryApi
      .page(path)
      .then((p) => !cancelled && setPage(p))
      .catch((e) => !cancelled && setError(e instanceof ApiError ? e : new ApiError(0, String(e))))
    return () => {
      cancelled = true
    }
  }, [path, nonce])

  if (error) {
    return (
      <div className="mx-auto max-w-3xl px-10 py-16">
        <ErrorState
          title={error.status === 404 ? "This page is not in the vault any more" : "Could not open this page"}
          message={error.status === 404 ? `${path} was moved or deleted, maybe from another app.` : error.message}
          onRetry={() => setNonce((n) => n + 1)}
        />
      </div>
    )
  }
  if (!page) {
    return (
      <div className="mx-auto max-w-3xl space-y-4 px-14 pt-20" aria-busy="true">
        <Skeleton className="h-10 w-2/3" />
        <Skeleton className="h-4 w-1/3" />
        <Skeleton className="mt-8 h-4 w-full" />
        <Skeleton className="h-4 w-5/6" />
        <Skeleton className="h-4 w-4/6" />
      </div>
    )
  }
  return <Loaded key={`${page.path}:${nonce}`} {...props} page={page} />
}

function Loaded({ page, vaultRoot, fresh, onOpenLink, findPages, onCreatePage, onMeta, onRename, onTrash, onOpenFolder, openPath }: PageViewProps & { page: Page }) {
  // The title is front matter "title", else the file name. A new page
  // ("Untitled.md") starts empty and takes its file name from its first title.
  const name = baseName(page.path)
  const untitled = /^Untitled( \d+)?$/.test(name)
  const hasFrontTitle = typeof page.front?.title === "string"
  const titleKept = useRef(hasFrontTitle || !untitled)
  const [front, setFront] = useState<Record<string, unknown>>(page.front ?? {})
  const [title, setTitle] = useState(hasFrontTitle ? String(page.front.title) : untitled ? "" : name)
  const [save, setSave] = useState<SaveState>("saved")
  const [saveError, setSaveError] = useState<string | null>(null)
  const [backlinks, setBacklinks] = useState<string[] | null>(null)
  const [picker, setPicker] = useState<"icon" | "cover" | null>(null)
  const titleRef = useRef<HTMLTextAreaElement>(null)

  // What is on disk vs what to write. The body is only sent once the editor
  // changed it, so a properties edit never reformats an Obsidian page.
  const body = useRef<string | null>(null)
  const baseline = useRef<string | null>(null)
  const latest = useRef({ front, title })
  latest.current = { front, title }
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null)
  const inflight = useRef<Promise<void> | null>(null)
  const metaDirty = useRef(false)

  const flush = useCallback(async () => {
    if (timer.current) {
      clearTimeout(timer.current)
      timer.current = null
    }
    if (inflight.current) await inflight.current
    const { front: f, title: t } = latest.current
    const out = { ...f }
    const tt = t.trim()
    // A title equal to the file name needs no front matter of its own.
    if (tt && titleKept.current && !(tt === name && !hasFrontTitle)) out.title = tt
    else delete out.title
    setSave("saving")
    const run = memoryApi
      .save(page.path, { front: out, body: body.current ?? page.body })
      .then(() => {
        setSave((s) => (s === "saving" ? "saved" : s))
        setSaveError(null)
        if (metaDirty.current) {
          metaDirty.current = false
          onMeta()
        }
      })
      .catch((e) => {
        setSave("error")
        setSaveError(errorMessage(e))
      })
      .finally(() => {
        inflight.current = null
      })
    inflight.current = run
    await run
  }, [page.path, page.body, onMeta, name, hasFrontTitle])

  const schedule = useCallback(() => {
    setSave("dirty")
    if (timer.current) clearTimeout(timer.current)
    timer.current = setTimeout(() => void flush(), SAVE_DELAY_MS)
  }, [flush])

  // Save anything pending when leaving the page.
  useEffect(
    () => () => {
      if (timer.current) void flush()
    },
    [flush],
  )

  useEffect(() => {
    let cancelled = false
    memoryApi
      .backlinks(page.path)
      .then((b) => !cancelled && setBacklinks(b))
      .catch(() => !cancelled && setBacklinks([]))
    return () => {
      cancelled = true
    }
  }, [page.path])

  useEffect(() => {
    if (fresh) titleRef.current?.focus()
  }, [fresh])

  // The title textarea grows with its text.
  useEffect(() => {
    const el = titleRef.current
    if (!el) return
    el.style.height = "0px"
    el.style.height = `${el.scrollHeight}px`
  }, [title])

  const updateFront = (next: Record<string, unknown>, meta = false) => {
    setFront(next)
    if (meta) metaDirty.current = true
    schedule()
  }

  const onBody = useCallback(
    (md: string) => {
      // Ignore the editor echoing the page back unchanged.
      if (baseline.current !== null && md === baseline.current && body.current === null) return
      body.current = md
      schedule()
    },
    [schedule],
  )

  const icon = typeof front.icon === "string" ? front.icon : ""
  const cover = coverStyle(front.cover)
  const display = title.trim() || name
  const crumbs = dirOf(page.path) ? dirOf(page.path).split("/") : []

  const commitTitle = async () => {
    if (titleKept.current || !title.trim()) return
    // A page made in Lucidbench takes its file name from its first title, so
    // [[links]] in Obsidian read well.
    await flush()
    const to = await onRename(page.path, title.trim())
    if (to) {
      openPath(to)
      return
    }
    // The name is taken: keep the file name and store the title instead.
    titleKept.current = true
    metaDirty.current = true
    schedule()
  }

  return (
    <div className="pb-16">
      {/* top bar */}
      <div className="sticky top-0 z-20 flex h-11 items-center gap-2 border-b border-transparent bg-card/85 px-4 backdrop-blur-md">
        <nav aria-label="Page path" className="flex min-w-0 flex-1 items-center gap-1 text-sm text-muted-foreground">
          <button type="button" onClick={() => onOpenFolder("")} className="shrink-0 rounded px-1 py-0.5 hover:bg-accent hover:text-foreground">
            Memory
          </button>
          {crumbs.map((c, i) => (
            <span key={i} className="flex min-w-0 items-center gap-1">
              <ChevronRight className="size-3.5 shrink-0 text-subtle-foreground" />
              <button type="button" onClick={() => onOpenFolder(crumbs.slice(0, i + 1).join("/"))} className="truncate rounded px-1 py-0.5 hover:bg-accent hover:text-foreground">
                {c}
              </button>
            </span>
          ))}
          <ChevronRight className="size-3.5 shrink-0 text-subtle-foreground" />
          <span className="flex min-w-0 items-center gap-1.5 truncate px-1 font-medium text-foreground">
            {icon && <span>{icon}</span>}
            <span className="truncate">{display}</span>
          </span>
        </nav>
        <SaveBadge state={save} error={saveError} onRetry={() => void flush()} />
        {vaultRoot && (
          <a
            href={`obsidian://open?path=${encodeURIComponent(`${vaultRoot}/${page.path}`)}`}
            className="hidden h-7 items-center gap-1.5 rounded-md px-2 text-xs text-muted-foreground transition-colors hover:bg-accent hover:text-foreground md:inline-flex"
            title="Open this file in Obsidian (if it is installed)"
          >
            <ExternalLink className="size-3.5" /> Obsidian
          </a>
        )}
        <Menu
          label="Page actions"
          trigger={<Ellipsis />}
          items={[
            { label: "Copy [[link]]", icon: Link2, onSelect: () => void copyText(`[[${baseName(page.path)}]]`, "Link copied") },
            { label: "Copy file path", icon: Copy, onSelect: () => void copyText(vaultRoot ? `${vaultRoot}/${page.path}` : page.path, "Path copied") },
            { label: "Move to trash", icon: Trash2, danger: true, onSelect: () => onTrash(page.path) },
          ]}
        />
      </div>

      {/* cover */}
      {cover ? (
        <div className="group relative h-40 w-full" style={{ background: cover }}>
          <div className="absolute bottom-2 right-4 flex gap-1 opacity-0 transition-opacity group-hover:opacity-100">
            <button type="button" onClick={() => setPicker("cover")} className="rounded-md bg-black/35 px-2 py-1 text-xs text-white backdrop-blur hover:bg-black/50">
              Change cover
            </button>
            <button
              type="button"
              onClick={() => {
                const next = { ...front }
                delete next.cover
                updateFront(next)
              }}
              className="rounded-md bg-black/35 px-2 py-1 text-xs text-white backdrop-blur hover:bg-black/50"
            >
              Remove
            </button>
          </div>
        </div>
      ) : (
        <div className="h-14" />
      )}

      <div className="relative mx-auto max-w-3xl px-14">
        {/* icon */}
        {icon && (
          <button
            type="button"
            onClick={() => setPicker("icon")}
            aria-label="Change icon"
            className={cn("relative z-10 -ml-1 block rounded-lg p-1 text-[64px] leading-none transition-colors hover:bg-accent/70", cover ? "-mt-10" : "mt-2")}
          >
            {icon}
          </button>
        )}
        {picker && (
          <Picker
            kind={picker}
            onClose={() => setPicker(null)}
            onPick={(v) => {
              setPicker(null)
              const next = { ...front }
              if (v === null) delete next[picker]
              else next[picker] = v
              updateFront(next, picker === "icon")
            }}
          />
        )}

        {/* hover controls */}
        <div className="group/page">
          <div className="flex h-8 items-center gap-1 pt-2 opacity-0 transition-opacity group-hover/page:opacity-100 focus-within:opacity-100">
            {!icon && (
              <button type="button" onClick={() => setPicker("icon")} className="flex h-7 items-center gap-1.5 rounded-md px-1.5 text-sm text-subtle-foreground hover:bg-accent hover:text-foreground">
                <SmilePlus className="size-4" /> Add icon
              </button>
            )}
            {!cover && (
              <button type="button" onClick={() => setPicker("cover")} className="flex h-7 items-center gap-1.5 rounded-md px-1.5 text-sm text-subtle-foreground hover:bg-accent hover:text-foreground">
                <ImagePlus className="size-4" /> Add cover
              </button>
            )}
          </div>

          <textarea
            ref={titleRef}
            rows={1}
            value={title}
            placeholder="Untitled"
            aria-label="Page title"
            onChange={(e) => {
              setTitle(e.target.value.replace(/\n/g, ""))
              metaDirty.current = true
              schedule()
            }}
            onBlur={() => void commitTitle()}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                e.preventDefault()
                ;(document.querySelector(".lb-prose") as HTMLElement | null)?.focus()
              }
            }}
            className="mt-1 block w-full resize-none overflow-hidden bg-transparent text-[2.5rem] font-bold leading-tight tracking-[-0.025em] outline-none placeholder:text-subtle-foreground/60"
          />
        </div>

        {page.confidential && (
          <div role="note" className="mt-4 flex items-center gap-2.5 rounded-lg border border-warning/35 bg-warning-soft px-3 py-2 text-sm text-warning-fg">
            <Lock className="size-4 shrink-0" />
            <span>
              <span className="font-medium">Confidential.</span> This page is never sent to AI providers: Council, Work and “ask memory” refuse it.
            </span>
          </div>
        )}

        <div className="mt-4 border-b pb-3">
          <Properties front={front} onChange={(next) => updateFront(next, true)} />
        </div>

        <Suspense
          fallback={
            <div className="space-y-3 pt-6" aria-busy="true">
              <Skeleton className="h-4 w-full" />
              <Skeleton className="h-4 w-5/6" />
              <Skeleton className="h-4 w-3/5" />
            </div>
          }
        >
          <MemoryEditor
            className="pt-5"
            initial={page.body}
            onChange={onBody}
            onReady={(md) => (baseline.current = md)}
            onOpenLink={onOpenLink}
            findPages={(q) => findPages(q, page.path)}
            onCreatePage={onCreatePage}
          />
        </Suspense>

        <Backlinks links={backlinks} openPath={openPath} />
      </div>
    </div>
  )
}

function SaveBadge({ state, error, onRetry }: { state: SaveState; error: string | null; onRetry: () => void }) {
  if (state === "error") {
    return (
      <button type="button" onClick={onRetry} title={error ?? undefined} className="flex h-7 items-center gap-1.5 rounded-md px-2 text-xs text-danger-fg hover:bg-danger-soft">
        <AlertTriangle className="size-3.5" /> Not saved · Retry
      </button>
    )
  }
  return (
    <span className="flex h-7 items-center gap-1.5 px-1 text-xs text-subtle-foreground" aria-live="polite">
      {state === "saving" ? (
        <>
          <Loader2 className="size-3.5 animate-spin" /> Saving…
        </>
      ) : state === "dirty" ? (
        <>
          <span className="size-1.5 rounded-full bg-warning" /> Edited
        </>
      ) : (
        <>
          <Check className="size-3.5 text-success" /> Saved
        </>
      )}
    </span>
  )
}

function Backlinks({ links, openPath }: { links: string[] | null; openPath: (p: string) => void }) {
  return (
    <section aria-label="Backlinks" className="mt-14 border-t pt-4">
      <h2 className="flex items-center gap-2 text-xs font-medium uppercase tracking-[0.08em] text-subtle-foreground">
        <ArrowUpRight className="size-3.5" />
        Linked mentions
        {links && <span className="rounded-full bg-muted px-1.5 text-2xs tabular-nums text-muted-foreground">{links.length}</span>}
      </h2>
      {links === null ? (
        <Skeleton className="mt-3 h-8 w-64" />
      ) : links.length === 0 ? (
        <p className="mt-2 text-sm text-subtle-foreground">No page links here yet. Type [[ in another page to link to this one.</p>
      ) : (
        <ul className="mt-2 grid gap-1.5 sm:grid-cols-2">
          {links.map((l) => (
            <li key={l}>
              <button
                type="button"
                onClick={() => openPath(l)}
                className="flex w-full items-center gap-2.5 rounded-lg border bg-background/50 px-3 py-2 text-left transition-colors hover:border-border-strong hover:bg-accent/40"
              >
                <FileText className="size-4 shrink-0 text-subtle-foreground" />
                <span className="min-w-0">
                  <span className="block truncate text-sm font-medium">{baseName(l)}</span>
                  <span className="block truncate text-2xs text-subtle-foreground">{dirOf(l) || "Memory"}</span>
                </span>
              </button>
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}

function Picker({ kind, onPick, onClose }: { kind: "icon" | "cover"; onPick: (v: string | null) => void; onClose: () => void }) {
  const ref = useRef<HTMLDivElement>(null)
  const [custom, setCustom] = useState("")
  useEffect(() => {
    const onDown = (e: MouseEvent) => !ref.current?.contains(e.target as Node) && onClose()
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && onClose()
    document.addEventListener("mousedown", onDown)
    window.addEventListener("keydown", onKey)
    return () => {
      document.removeEventListener("mousedown", onDown)
      window.removeEventListener("keydown", onKey)
    }
  }, [onClose])
  return (
    <div ref={ref} className="absolute z-30 mt-1 w-80 rounded-xl border border-border-strong bg-elevated p-3 shadow-pop animate-in fade-in-0 zoom-in-95 duration-150">
      <div className="mb-2 flex items-center justify-between">
        <span className="text-xs font-medium text-muted-foreground">{kind === "icon" ? "Page icon" : "Cover colour"}</span>
        <button type="button" onClick={() => onPick(null)} className="rounded px-1.5 py-0.5 text-xs text-subtle-foreground hover:bg-accent hover:text-foreground">
          Remove
        </button>
      </div>
      {kind === "icon" ? (
        <>
          <div className="grid grid-cols-9 gap-0.5">
            {EMOJI.map((e) => (
              <button key={e} type="button" onClick={() => onPick(e)} className="flex size-8 items-center justify-center rounded-md text-lg hover:bg-accent" aria-label={`Use ${e}`}>
                {e}
              </button>
            ))}
          </div>
          <input
            value={custom}
            onChange={(e) => setCustom(e.target.value)}
            onKeyDown={(e) => e.key === "Enter" && custom.trim() && onPick(custom.trim())}
            placeholder="Or type / paste any emoji, Enter to use"
            className="mt-2 h-8 w-full rounded-md border bg-background px-2 text-sm outline-none focus-visible:ring-2 focus-visible:ring-ring/40"
          />
        </>
      ) : (
        <div className="grid grid-cols-4 gap-2">
          {Object.entries(COVERS).map(([name, bg]) => (
            <button key={name} type="button" onClick={() => onPick(name)} className="group flex flex-col items-center gap-1" aria-label={`Use the ${name} cover`}>
              <span className="h-10 w-full rounded-md ring-offset-2 ring-offset-elevated transition-shadow group-hover:ring-2 group-hover:ring-ring" style={{ background: bg }} />
              <span className="text-2xs capitalize text-muted-foreground">{name}</span>
            </button>
          ))}
        </div>
      )}
    </div>
  )
}
