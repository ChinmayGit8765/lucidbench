import { useEffect, useState } from "react"
import { ArrowRight, BookOpenText, FilePlus2, FileText, FolderCog, Link2, Lock, NotebookPen, Search } from "lucide-react"

import { copyText } from "@/components/CopyCommand"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/states"
import { allPages, baseName, dirOf, type Entry } from "@/lib/memory"
import { relativeTime, useNow } from "@/lib/time"

/** The empty vault: what Memory is, where the files live, how to start. */
export function Onboarding({ root, onNewPage, onUseVault }: { root: string | null; onNewPage: () => void; onUseVault: () => void }) {
  const points = [
    { icon: FileText, title: "Plain Markdown files", text: "Every page is a .md file with YAML properties. Nothing is locked in a database." },
    { icon: BookOpenText, title: "Opens in Obsidian", text: "Point Obsidian at the same folder: [[links]], callouts and Kanban boards all carry over." },
    { icon: Link2, title: "Linked thinking", text: "Type [[ to link a page, / for blocks. Each page lists the pages that link to it." },
    { icon: Lock, title: "Confidential stays local", text: "Mark a page or folder confidential: true and no AI provider ever receives it." },
  ]
  return (
    <div className="mx-auto max-w-2xl px-10 py-14">
      <div className="flex size-14 items-center justify-center rounded-2xl border border-border-strong bg-elevated text-brand shadow-pop">
        <NotebookPen className="size-6" />
      </div>
      <h1 className="mt-6 text-[2.25rem] font-bold leading-tight tracking-[-0.025em]">Your memory starts here</h1>
      <p className="mt-2 max-w-xl text-base text-muted-foreground">
        Memory is where briefs, notes and decisions live, so Council, Boards and Work can build on them. It is a folder of Markdown pages you
        own, with a Notion-style editor on top.
      </p>

      <div className="mt-8 grid gap-3 sm:grid-cols-2">
        {points.map(({ icon: Icon, title, text }) => (
          <div key={title} className="rounded-xl border bg-background/50 p-4">
            <Icon className="size-4 text-brand" />
            <div className="mt-2 text-sm font-semibold">{title}</div>
            <p className="mt-0.5 text-sm text-muted-foreground">{text}</p>
          </div>
        ))}
      </div>

      <div className="mt-6 rounded-xl border bg-background/50 p-4">
        <div className="text-2xs font-medium uppercase tracking-[0.08em] text-subtle-foreground">Vault location</div>
        <div className="mt-1.5 flex items-center gap-2">
          <code className="min-w-0 flex-1 truncate font-mono text-xs" title={root ?? undefined}>
            {root ?? "…"}
          </code>
          {root && (
            <button type="button" onClick={() => void copyText(root, "Path copied")} className="text-2xs text-subtle-foreground hover:text-foreground">
              Copy
            </button>
          )}
        </div>
        <p className="mt-1.5 text-xs text-muted-foreground">Lucidbench never picks an existing vault by itself. Already keep notes in Obsidian? Point Memory at that folder instead.</p>
      </div>

      <div className="mt-8 flex flex-wrap gap-2">
        <Button onClick={onNewPage}>
          <FilePlus2 /> Create your first page
        </Button>
        <Button variant="secondary" onClick={onUseVault}>
          <FolderCog /> Use a different vault
        </Button>
      </div>
    </div>
  )
}

/** A vault with pages, none open: recent pages and the quick actions. */
export function Home({ onNewPage, onSearch, openPath, pages }: { onNewPage: () => void; onSearch: () => void; openPath: (p: string) => void; pages: number }) {
  const now = useNow(30000)
  const [recent, setRecent] = useState<Entry[] | null>(null)
  useEffect(() => {
    let cancelled = false
    allPages(400)
      .then((p) => !cancelled && setRecent([...p].sort((a, b) => b.modified.localeCompare(a.modified)).slice(0, 9)))
      .catch(() => !cancelled && setRecent([]))
    return () => {
      cancelled = true
    }
  }, [])
  return (
    <div className="mx-auto max-w-3xl px-10 py-12">
      <h1 className="text-[2rem] font-bold tracking-[-0.025em]">Memory</h1>
      <p className="mt-1 text-sm text-muted-foreground">
        {pages} {pages === 1 ? "page" : "pages"} in your vault. Pick one on the left, or start something new.
      </p>
      <div className="mt-6 flex flex-wrap gap-2">
        <Button onClick={onNewPage}>
          <FilePlus2 /> New page
        </Button>
        <Button variant="secondary" onClick={onSearch}>
          <Search /> Search memory
        </Button>
      </div>
      <h2 className="mt-10 text-xs font-medium uppercase tracking-[0.08em] text-subtle-foreground">Recently edited</h2>
      {recent === null ? (
        <div className="mt-3 grid gap-3 sm:grid-cols-3">
          {[0, 1, 2].map((i) => (
            <Skeleton key={i} className="h-24 rounded-xl" />
          ))}
        </div>
      ) : (
        <ul className="mt-3 grid gap-3 sm:grid-cols-3">
          {recent.map((p) => (
            <li key={p.path}>
              <button
                type="button"
                onClick={() => openPath(p.path)}
                className="group flex h-full w-full flex-col rounded-xl border bg-background/50 p-3.5 text-left transition-[border-color,box-shadow] hover:border-border-strong hover:shadow-card"
              >
                <span className="text-2xl leading-none">{p.icon || <FileText className="size-5 text-subtle-foreground" />}</span>
                <span className="mt-3 line-clamp-2 text-sm font-semibold">{p.title || baseName(p.path)}</span>
                <span className="mt-auto flex items-center gap-1 pt-2 text-2xs text-subtle-foreground">
                  <span className="truncate">{dirOf(p.path) || "Memory"}</span>
                  <span>·</span>
                  <span className="shrink-0">{relativeTime(p.modified, now)}</span>
                  <ArrowRight className="ml-auto size-3 opacity-0 transition-opacity group-hover:opacity-100" />
                </span>
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
