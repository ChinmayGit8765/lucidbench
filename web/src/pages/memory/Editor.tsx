/**
 * The Memory page editor: Milkdown (ProseMirror over remark), so pages stay
 * plain Markdown that Obsidian opens unchanged. Adds a slash menu, a "[["
 * page-link menu, block drag handles, task checkboxes and placeholders.
 *
 * Loaded on its own chunk; the tree and onboarding render without it.
 */
import { useEffect, useLayoutEffect, useRef, useState, type ReactNode } from "react"
import {
  CheckSquare,
  Code2,
  FileSymlink,
  FileText,
  Heading1,
  Heading2,
  Heading3,
  Info,
  List,
  ListOrdered,
  Minus,
  Plus,
  Quote,
  Table,
  Text,
  TriangleAlert,
  Lightbulb,
  type LucideIcon,
} from "lucide-react"

import { defaultValueCtx, Editor, editorViewCtx, editorViewOptionsCtx, rootCtx, serializerCtx } from "@milkdown/kit/core"
import type { Ctx } from "@milkdown/kit/ctx"
import { block, BlockProvider } from "@milkdown/kit/plugin/block"
import { clipboard } from "@milkdown/kit/plugin/clipboard"
import { cursor } from "@milkdown/kit/plugin/cursor"
import { history } from "@milkdown/kit/plugin/history"
import { listener, listenerCtx } from "@milkdown/kit/plugin/listener"
import { commonmark } from "@milkdown/kit/preset/commonmark"
import { gfm, insertTableCommand } from "@milkdown/kit/preset/gfm"
import { setBlockType, wrapIn } from "@milkdown/kit/prose/commands"
import { liftListItem, wrapInList } from "@milkdown/kit/prose/schema-list"
import { Plugin, PluginKey, TextSelection, type EditorState } from "@milkdown/kit/prose/state"
import { Decoration, DecorationSet, type EditorView } from "@milkdown/kit/prose/view"
import { $prose, callCommand } from "@milkdown/kit/utils"

import { cn } from "@/lib/utils"
import { configureObsidian, obsidian } from "@/pages/memory/obsidian"
import "@/pages/memory/editor.css"

/** A row of the "[[" menu. */
export interface LinkItem {
  /** What goes inside [[ ]]. */
  target: string
  label: string
  hint?: string
  icon?: string
  /** A page that does not exist yet: picking it creates the page. */
  create?: boolean
}

interface Props {
  /** The page body (no front matter). Read once; the editor owns it after that. */
  initial: string
  /** Every change, as Markdown. */
  onChange: (markdown: string) => void
  /** The body as this editor writes it, before any edit (for "is it dirty?"). */
  onReady?: (markdown: string) => void
  /** A wikilink was clicked. */
  onOpenLink: (target: string) => void
  /** Pages for the "[[" menu, best first. */
  findPages: (query: string) => Promise<LinkItem[]>
  /** A "[[" pick that should create its page first. */
  onCreatePage?: (item: LinkItem) => Promise<void>
  /** Put the cursor in the body once the editor is up. */
  autoFocus?: boolean
  className?: string
}

/* ---------- slash commands ---------- */

interface SlashItem {
  id: string
  label: string
  hint: string
  icon: LucideIcon
  keywords: string
  group: "Basic blocks" | "Callouts" | "Insert"
  run: (view: EditorView, ctx: Ctx) => void
}

function setChecked(view: EditorView) {
  const { $from } = view.state.selection
  for (let d = $from.depth; d > 0; d--) {
    if ($from.node(d).type.name === "list_item") {
      view.dispatch(view.state.tr.setNodeMarkup($from.before(d), undefined, { ...$from.node(d).attrs, checked: false }))
      return
    }
  }
}

/**
 * Wraps the current block. A list item cannot hold a quote or callout as
 * its first child, so inside a list the item is lifted out first, as
 * Notion turns a list item into the new block.
 */
function wrapBlock(view: EditorView, type: "blockquote" | "callout", attrs?: Record<string, unknown>) {
  const node = view.state.schema.nodes[type]
  if (wrapIn(node, attrs)(view.state, view.dispatch)) return
  const item = view.state.schema.nodes.list_item
  for (let i = 0; i < 8 && liftListItem(item)(view.state, view.dispatch); i++) {
    /* lift until the paragraph is out of every list */
  }
  wrapIn(node, attrs)(view.state, view.dispatch)
}

const SLASH: SlashItem[] = [
  { id: "text", label: "Text", hint: "Plain paragraph", icon: Text, keywords: "paragraph plain p", group: "Basic blocks", run: (v) => setBlockType(v.state.schema.nodes.paragraph)(v.state, v.dispatch) },
  { id: "h1", label: "Heading 1", hint: "# Big section heading", icon: Heading1, keywords: "title h1 #", group: "Basic blocks", run: (v) => setBlockType(v.state.schema.nodes.heading, { level: 1 })(v.state, v.dispatch) },
  { id: "h2", label: "Heading 2", hint: "## Medium heading", icon: Heading2, keywords: "subtitle h2 ##", group: "Basic blocks", run: (v) => setBlockType(v.state.schema.nodes.heading, { level: 2 })(v.state, v.dispatch) },
  { id: "h3", label: "Heading 3", hint: "### Small heading", icon: Heading3, keywords: "h3 ###", group: "Basic blocks", run: (v) => setBlockType(v.state.schema.nodes.heading, { level: 3 })(v.state, v.dispatch) },
  { id: "todo", label: "To-do list", hint: "- [ ] Track tasks", icon: CheckSquare, keywords: "task checkbox check todo", group: "Basic blocks", run: (v) => { wrapInList(v.state.schema.nodes.bullet_list)(v.state, v.dispatch); setChecked(v) } },
  { id: "bullet", label: "Bulleted list", hint: "- A simple list", icon: List, keywords: "ul unordered bullet", group: "Basic blocks", run: (v) => wrapInList(v.state.schema.nodes.bullet_list)(v.state, v.dispatch) },
  { id: "number", label: "Numbered list", hint: "1. An ordered list", icon: ListOrdered, keywords: "ol ordered number", group: "Basic blocks", run: (v) => wrapInList(v.state.schema.nodes.ordered_list)(v.state, v.dispatch) },
  { id: "quote", label: "Quote", hint: "> Capture a quote", icon: Quote, keywords: "blockquote citation", group: "Basic blocks", run: (v) => wrapBlock(v, "blockquote") },
  { id: "code", label: "Code", hint: "``` A code block", icon: Code2, keywords: "snippet pre fence", group: "Basic blocks", run: (v) => setBlockType(v.state.schema.nodes.code_block)(v.state, v.dispatch) },
  { id: "callout-note", label: "Note callout", hint: "> [!note]", icon: Info, keywords: "callout admonition note info", group: "Callouts", run: (v) => wrapBlock(v, "callout", { kind: "note" }) },
  { id: "callout-tip", label: "Tip callout", hint: "> [!tip]", icon: Lightbulb, keywords: "callout admonition tip hint", group: "Callouts", run: (v) => wrapBlock(v, "callout", { kind: "tip" }) },
  { id: "callout-warning", label: "Warning callout", hint: "> [!warning]", icon: TriangleAlert, keywords: "callout admonition warning caution danger", group: "Callouts", run: (v) => wrapBlock(v, "callout", { kind: "warning" }) },
  { id: "link", label: "Link to page", hint: "[[ Link another page", icon: FileSymlink, keywords: "wikilink mention page reference", group: "Insert", run: (v) => v.dispatch(v.state.tr.insertText("[[")) },
  { id: "table", label: "Table", hint: "A simple grid", icon: Table, keywords: "grid columns rows", group: "Insert", run: (_, ctx) => callCommand(insertTableCommand.key, { row: 3, col: 3 })(ctx) },
  {
    id: "divider",
    label: "Divider",
    hint: "--- A horizontal rule",
    icon: Minus,
    keywords: "hr rule line separator",
    group: "Insert",
    run: (v) => {
      const { hr, paragraph } = v.state.schema.nodes
      const { $from } = v.state.selection
      const at = $from.before($from.depth)
      const tr = v.state.tr.replaceWith(at, $from.after($from.depth), [hr.create(), paragraph.create()])
      v.dispatch(tr.setSelection(TextSelection.near(tr.doc.resolve(at + 2))))
    },
  },
]

function filterSlash(query: string): SlashItem[] {
  const q = query.toLowerCase()
  return SLASH.filter((s) => !q || `${s.label} ${s.keywords}`.toLowerCase().includes(q))
}

/* ---------- the suggestion bridge (ProseMirror ↔ React) ---------- */

interface Trigger {
  kind: "slash" | "link"
  query: string
  from: number
  to: number
  left: number
  top: number
}

interface Bridge {
  trigger: Trigger | null
  dismissedAt: number | null
  set: (t: Trigger | null) => void
  key: (e: KeyboardEvent) => boolean
  openLink: (target: string) => void
}

function detect(view: EditorView, dismissedAt: number | null): Trigger | null {
  const sel = view.state.selection
  if (!sel.empty || !view.hasFocus()) return null
  const $from = sel.$from
  if ($from.parent.type.spec.code) return null
  const before = $from.parent.textBetween(Math.max(0, $from.parentOffset - 80), $from.parentOffset, undefined, "￼")
  let kind: Trigger["kind"] | null = null
  let len = 0
  let query = ""
  const link = /\[\[([^[\]\n|#]*)$/.exec(before)
  const slash = /(?:^|\s)\/([\p{L}\d-]*)$/u.exec(before)
  if (link) {
    kind = "link"
    query = link[1]
    len = link[0].length
  } else if (slash) {
    kind = "slash"
    query = slash[1]
    len = query.length + 1
  }
  if (!kind) return null
  const from = $from.pos - len
  if (from === dismissedAt) return null
  const c = view.coordsAtPos(from)
  return { kind, query, from, to: $from.pos, left: c.left, top: c.bottom }
}

const placeholderKey = new PluginKey("lb-placeholder")

function editorPlugins(bridge: Bridge) {
  const suggest = new Plugin({
    view: () => ({
      update: (view) => {
        const t = detect(view, bridge.dismissedAt)
        if (!t && bridge.dismissedAt !== null && !view.state.selection.empty) bridge.dismissedAt = null
        const cur = bridge.trigger
        if (t?.kind !== cur?.kind || t?.query !== cur?.query || t?.from !== cur?.from) bridge.set(t)
      },
    }),
    props: {
      handleKeyDown: (_, e) => bridge.key(e),
      handleDOMEvents: {
        blur: () => {
          // Let a click on the menu land before it closes.
          setTimeout(() => bridge.trigger && !document.activeElement?.closest(".lb-suggest") && bridge.set(null), 120)
          return false
        },
        mousedown: (view, e) => {
          const el = e.target as HTMLElement
          // A wikilink opens its page.
          const link = el.closest<HTMLElement>("[data-wikilink]")
          if (link && e.button === 0) {
            e.preventDefault()
            bridge.openLink(link.dataset.wikilink ?? "")
            return true
          }
          // The task checkbox sits in the list item's left padding.
          const li = el.closest<HTMLElement>('li[data-item-type="task"]')
          if (li && el === li && e.clientX < li.getBoundingClientRect().left + 24) {
            const $pos = view.state.doc.resolve(view.posAtDOM(li, 0))
            for (let d = $pos.depth; d > 0; d--) {
              const node = $pos.node(d)
              if (node.type.name === "list_item") {
                e.preventDefault()
                view.dispatch(view.state.tr.setNodeMarkup($pos.before(d), undefined, { ...node.attrs, checked: !node.attrs.checked }))
                return true
              }
            }
          }
          // The callout header cycles its kind.
          const callout = el.closest<HTMLElement>(".lb-callout")
          if (callout && el === callout && e.clientY < callout.getBoundingClientRect().top + 30) {
            const pos = view.posAtDOM(callout, 0) - 1
            const node = view.state.doc.nodeAt(pos)
            if (node?.type.name === "callout") {
              e.preventDefault()
              const kinds = ["note", "tip", "info", "warning", "danger", "success", "question"]
              const next = kinds[(kinds.indexOf(node.attrs.kind) + 1) % kinds.length]
              view.dispatch(view.state.tr.setNodeMarkup(pos, undefined, { ...node.attrs, kind: next, title: node.attrs.title }))
              return true
            }
          }
          return false
        },
      },
    },
  })
  const placeholder = new Plugin({
    key: placeholderKey,
    props: {
      decorations: (state: EditorState) => {
        const { doc, selection } = state
        const only = doc.childCount === 1 ? doc.firstChild : null
        if (only && only.type.name === "paragraph" && only.content.size === 0) {
          return DecorationSet.create(doc, [Decoration.node(0, only.nodeSize, { class: "lb-empty", "data-placeholder": "Write something, or type “/” for blocks and “[[” to link a page…" })])
        }
        const $f = selection.$from
        if (!selection.empty || $f.parent.type.name !== "paragraph" || $f.parent.content.size > 0) return null
        const at = $f.before($f.depth)
        return DecorationSet.create(doc, [Decoration.node(at, at + $f.parent.nodeSize, { class: "lb-empty", "data-placeholder": "Type “/” for blocks" })])
      },
    },
  })
  return [$prose(() => suggest), $prose(() => placeholder)]
}

/* ---------- block handle ---------- */

const GRIP =
  '<svg viewBox="0 0 10 16" width="10" height="16" aria-hidden="true"><g fill="currentColor"><circle cx="2.5" cy="3" r="1.3"/><circle cx="7.5" cy="3" r="1.3"/><circle cx="2.5" cy="8" r="1.3"/><circle cx="7.5" cy="8" r="1.3"/><circle cx="2.5" cy="13" r="1.3"/><circle cx="7.5" cy="13" r="1.3"/></g></svg>'
const PLUS =
  '<svg viewBox="0 0 16 16" width="14" height="14" aria-hidden="true"><path d="M8 3v10M3 8h10" stroke="currentColor" stroke-width="1.6" stroke-linecap="round"/></svg>'

function blockHandle(ctx: Ctx): BlockProvider {
  const el = document.createElement("div")
  el.className = "lb-block-handle"
  el.innerHTML = `<button type="button" class="lb-bh-add" title="Add a block below" aria-label="Add a block below">${PLUS}</button><span class="lb-bh-grip" title="Drag to move">${GRIP}</span>`
  const provider = new BlockProvider({ ctx, content: el, getOffset: () => 6, getPlacement: () => "left" })
  el.querySelector(".lb-bh-add")!.addEventListener("mousedown", (e) => {
    e.preventDefault()
    e.stopPropagation()
    const active = provider.active
    if (!active) return
    const view = ctx.get(editorViewCtx)
    const at = active.$pos.pos + active.node.nodeSize
    const p = view.state.schema.nodes.paragraph.create(null, view.state.schema.text("/"))
    const tr = view.state.tr.insert(at, p)
    // Focus first, so the slash menu sees a focused editor and opens.
    view.focus()
    view.dispatch(tr.setSelection(TextSelection.create(tr.doc, at + 2)))
  })
  return provider
}

/* ---------- the component ---------- */

export default function MemoryEditor({ initial, onChange, onReady, onOpenLink, findPages, onCreatePage, autoFocus, className }: Props) {
  const root = useRef<HTMLDivElement>(null)
  const editorRef = useRef<Editor | null>(null)
  const [trigger, setTrigger] = useState<Trigger | null>(null)
  const [links, setLinks] = useState<LinkItem[]>([])
  const [active, setActive] = useState(0)
  const cb = useRef({ onChange, onReady, onOpenLink, findPages, onCreatePage })
  cb.current = { onChange, onReady, onOpenLink, findPages, onCreatePage }

  const slashItems = trigger?.kind === "slash" ? filterSlash(trigger.query) : []
  const count = trigger?.kind === "slash" ? slashItems.length : links.length
  const state = useRef({ trigger, count, active, choose: (_: number) => {} })

  const bridge = useRef<Bridge>({
    trigger: null,
    dismissedAt: null,
    set: (t) => {
      bridge.current.trigger = t
      setTrigger(t)
      setActive(0)
    },
    key: (e) => {
      const s = state.current
      if (!s.trigger || s.count === 0) {
        if (s.trigger && e.key === "Escape") {
          bridge.current.dismissedAt = s.trigger.from
          bridge.current.set(null)
          return true
        }
        return false
      }
      if (e.key === "ArrowDown" || e.key === "ArrowUp") {
        setActive((i) => (i + (e.key === "ArrowDown" ? 1 : -1) + s.count) % s.count)
        return true
      }
      if (e.key === "Enter" || e.key === "Tab") {
        s.choose(s.active)
        return true
      }
      if (e.key === "Escape") {
        bridge.current.dismissedAt = s.trigger.from
        bridge.current.set(null)
        return true
      }
      return false
    },
    openLink: (t) => cb.current.onOpenLink(t),
  })

  // Pages for the "[[" menu follow the query.
  const linkQuery = trigger?.kind === "link" ? trigger.query : null
  useEffect(() => {
    if (linkQuery === null) return
    let cancelled = false
    const id = setTimeout(() => {
      cb.current
        .findPages(linkQuery)
        .then((items) => {
          if (!cancelled) {
            setLinks(items)
            setActive(0)
          }
        })
        .catch(() => undefined)
    }, 120)
    return () => {
      cancelled = true
      clearTimeout(id)
    }
  }, [linkQuery])

  const choose = (i: number) => {
    const ed = editorRef.current
    const t = bridge.current.trigger
    if (!ed || !t) return
    // Close first: a pick may open the next menu ("Link to page" types "[[").
    bridge.current.set(null)
    ed.action((ctx) => {
      const view = ctx.get(editorViewCtx)
      if (t.kind === "slash") {
        const item = slashItems[i]
        if (!item) return
        view.dispatch(view.state.tr.delete(t.from, t.to))
        item.run(view, ctx)
      } else {
        const item = links[i]
        if (!item) return
        const node = view.state.schema.nodes.wikilink.create({ target: item.target })
        const tr = view.state.tr.replaceWith(t.from, t.to, [node, view.state.schema.text(" ")])
        view.dispatch(tr)
        if (item.create) void cb.current.onCreatePage?.(item)
      }
      view.focus()
    })
  }
  state.current = { trigger, count, active, choose }

  useEffect(() => {
    const el = root.current
    if (!el) return
    let provider: BlockProvider | null = null
    let destroyed = false
    const editor = Editor.make()
      .config((ctx) => {
        ctx.set(rootCtx, el)
        ctx.set(defaultValueCtx, initial)
        ctx.update(editorViewOptionsCtx, (o) => ({ ...o, attributes: { class: "lb-prose", spellcheck: "true" } }))
        configureObsidian(ctx)
        ctx.get(listenerCtx).markdownUpdated((_, md, prev) => {
          if (md !== prev) cb.current.onChange(md)
        })
      })
      .use(commonmark)
      .use(gfm)
      .use(obsidian)
      .use(history)
      .use(listener)
      .use(clipboard)
      .use(cursor)
      .use(block)
      .use(editorPlugins(bridge.current))
    editor
      .create()
      .then(() => {
        if (destroyed) {
          void editor.destroy()
          return
        }
        editorRef.current = editor
        editor.action((ctx) => {
          provider = blockHandle(ctx)
          provider.update()
          const view = ctx.get(editorViewCtx)
          cb.current.onReady?.(ctx.get(serializerCtx)(view.state.doc))
          if (autoFocus) {
            view.dispatch(view.state.tr.setSelection(TextSelection.atEnd(view.state.doc)))
            view.focus()
          }
        })
      })
      .catch((e) => console.error("editor failed to start", e))
    return () => {
      destroyed = true
      provider?.destroy()
      editorRef.current = null
      void editor.destroy()
    }
    // The editor is created once per mount; the parent re-keys it per page.
  }, [])

  return (
    <div className={cn("relative", className)}>
      <div ref={root} className="lb-editor" />
      {trigger && (
        <SuggestMenu trigger={trigger}>
          {trigger.kind === "slash" ? (
            slashItems.length === 0 ? (
              <div className="px-3 py-2 text-xs text-muted-foreground">No blocks match “{trigger.query}”</div>
            ) : (
              (["Basic blocks", "Callouts", "Insert"] as const).map((g) => {
                const items = slashItems.filter((s) => s.group === g)
                if (items.length === 0) return null
                return (
                  <div key={g} className="py-1">
                    <div className="px-2.5 pb-1 pt-1.5 text-2xs font-medium uppercase tracking-[0.08em] text-subtle-foreground">{g}</div>
                    {items.map((s) => {
                      const i = slashItems.indexOf(s)
                      const Icon = s.icon
                      return (
                        <MenuRow key={s.id} on={i === active} onHover={() => setActive(i)} onPick={() => choose(i)}>
                          <span className="flex size-8 shrink-0 items-center justify-center rounded-md border bg-background text-muted-foreground">
                            <Icon className="size-4" />
                          </span>
                          <span className="min-w-0">
                            <span className="block text-sm font-medium">{s.label}</span>
                            <span className="block truncate text-2xs text-subtle-foreground">{s.hint}</span>
                          </span>
                        </MenuRow>
                      )
                    })}
                  </div>
                )
              })
            )
          ) : links.length === 0 ? (
            <div className="px-3 py-2 text-xs text-muted-foreground">Type to find a page…</div>
          ) : (
            <div className="py-1">
              <div className="px-2.5 pb-1 pt-1.5 text-2xs font-medium uppercase tracking-[0.08em] text-subtle-foreground">Link to page</div>
              {links.map((l, i) => (
                <MenuRow key={`${l.target}-${i}`} on={i === active} onHover={() => setActive(i)} onPick={() => choose(i)}>
                  <span className="flex size-6 shrink-0 items-center justify-center text-sm">
                    {l.create ? <Plus className="size-4 text-brand" /> : l.icon ? l.icon : <FileText className="size-4 text-subtle-foreground" />}
                  </span>
                  <span className="min-w-0 flex-1">
                    <span className="block truncate text-sm">{l.label}</span>
                    {l.hint && <span className="block truncate text-2xs text-subtle-foreground">{l.hint}</span>}
                  </span>
                </MenuRow>
              ))}
            </div>
          )}
          <div className="border-t px-2.5 py-1.5 text-2xs text-subtle-foreground">↑↓ to move · Enter to pick · Esc to close</div>
        </SuggestMenu>
      )}
    </div>
  )
}

function SuggestMenu({ trigger, children }: { trigger: Trigger; children: ReactNode }) {
  const ref = useRef<HTMLDivElement>(null)
  const [pos, setPos] = useState({ left: trigger.left, top: trigger.top + 6 })
  // Keep the menu on screen: flip above the line when there is no room below.
  useLayoutEffect(() => {
    const el = ref.current
    if (!el) return
    const h = el.offsetHeight
    const w = el.offsetWidth
    const top = trigger.top + 6 + h > window.innerHeight - 8 ? Math.max(8, trigger.top - h - 28) : trigger.top + 6
    const left = Math.min(trigger.left, window.innerWidth - w - 8)
    setPos({ left, top })
  }, [trigger])
  return (
    <div
      ref={ref}
      className="lb-suggest fixed z-50 max-h-80 w-72 overflow-y-auto rounded-lg border border-border-strong bg-elevated shadow-pop animate-in fade-in-0 zoom-in-95 duration-100"
      style={pos}
      onMouseDown={(e) => e.preventDefault()}
      role="listbox"
    >
      {children}
    </div>
  )
}

function MenuRow({ on, onHover, onPick, children }: { on: boolean; onHover: () => void; onPick: () => void; children: ReactNode }) {
  const ref = useRef<HTMLButtonElement>(null)
  useEffect(() => {
    if (on) ref.current?.scrollIntoView({ block: "nearest" })
  }, [on])
  return (
    <button
      ref={ref}
      type="button"
      role="option"
      aria-selected={on}
      onMouseEnter={onHover}
      onClick={onPick}
      className={cn("mx-1 flex w-[calc(100%-0.5rem)] items-center gap-2.5 rounded-md px-1.5 py-1 text-left transition-colors", on ? "bg-accent" : "hover:bg-accent/60")}
    >
      {children}
    </button>
  )
}

