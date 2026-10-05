/**
 * Obsidian syntax for the Milkdown editor, so a page round-trips byte for
 * byte wherever it can:
 *
 *   [[Page]], [[Folder/Page|alias]], [[Page#Heading]], ![[embed.png]]
 *     an inline "wikilink" node, written back exactly as it was read
 *     (plain remark would escape it to \[\[Page]]).
 *   > [!note] Title
 *   > body
 *     a "callout" block, written back with its marker line.
 *
 * It also writes "-" bullets and "---" rules, as Obsidian does.
 */
import type { Ctx } from "@milkdown/kit/ctx"
import { remarkStringifyOptionsCtx } from "@milkdown/kit/core"
import type { Node as ProseNode } from "@milkdown/kit/prose/model"
import { $nodeSchema, $remark } from "@milkdown/kit/utils"
import { visit } from "unist-util-visit"

/* Minimal mdast shapes; the parser hands us plain objects. */
interface MdNode {
  type: string
  value?: string
  children?: MdNode[]
  [k: string]: unknown
}

const WIKI = /(!?)\[\[([^[\]\n]+?)\]\]/g
const CALLOUT = /^\[!([A-Za-z][\w-]*)\]([+-]?)(?:[ \t]+([^\n]*))?(?:\n|$)/

function remarkObsidian() {
  return (root: unknown) => {
    const tree = root as MdNode
    visit(tree as never, "text", (node: MdNode, index: number | undefined, parent: MdNode | undefined) => {
      const text = node.value ?? ""
      if (!parent?.children || index === undefined || !text.includes("[[")) return
      const out: MdNode[] = []
      let last = 0
      for (const m of text.matchAll(WIKI)) {
        const at = m.index ?? 0
        if (at > last) out.push({ type: "text", value: text.slice(last, at) })
        out.push({ type: "wikilink", value: m[2], embed: m[1] === "!" })
        last = at + m[0].length
      }
      if (out.length === 0) return
      if (last < text.length) out.push({ type: "text", value: text.slice(last) })
      parent.children.splice(index, 1, ...out)
      return index + out.length
    })
    visit(tree as never, "blockquote", (node: MdNode) => {
      const kids = node.children ?? []
      const p = kids[0]
      const t = p?.type === "paragraph" ? p.children?.[0] : undefined
      if (!p?.children || !t || t.type !== "text") return
      const m = CALLOUT.exec(t.value ?? "")
      if (!m) return
      node.type = "callout"
      node.kind = m[1].toLowerCase()
      node.fold = m[2]
      node.title = m[3] ?? ""
      t.value = (t.value ?? "").slice(m[0].length)
      if (t.value === "") p.children.shift()
      // "[!note]" and the next line can arrive split by a line break node.
      const n = p.children[0]
      if (n?.type === "break") p.children.shift()
      else if (n?.type === "text" && n.value?.startsWith("\n")) {
        n.value = n.value.slice(1)
        if (n.value === "") p.children.shift()
      }
      if (p.children.length === 0) kids.shift()
    })
  }
}

export const obsidianRemark = $remark("obsidian", () => remarkObsidian)

/** The text a wikilink shows: its alias, else the page name without folders. */
export function wikilinkLabel(target: string): string {
  const [page, alias] = target.split("|")
  if (alias?.trim()) return alias.trim()
  const name = page.split("#")[0].split("/").pop() || page
  const heading = page.includes("#") ? ` › ${page.split("#").slice(1).join("#")}` : ""
  return name.replace(/\.md$/i, "") + heading
}

export const wikilinkSchema = $nodeSchema("wikilink", () => ({
  group: "inline",
  inline: true,
  atom: true,
  selectable: true,
  attrs: { target: { default: "" }, embed: { default: false } },
  parseDOM: [
    {
      tag: "span[data-wikilink]",
      getAttrs: (dom: HTMLElement | string) =>
        typeof dom === "string" ? {} : { target: dom.getAttribute("data-wikilink") ?? "", embed: dom.hasAttribute("data-embed") },
    },
  ],
  toDOM: (node: ProseNode) => [
    "span",
    {
      "data-wikilink": node.attrs.target,
      ...(node.attrs.embed ? { "data-embed": "" } : {}),
      class: "lb-wikilink",
      title: node.attrs.target,
    },
    wikilinkLabel(node.attrs.target),
  ],
  parseMarkdown: {
    match: (n: { type: string }) => n.type === "wikilink",
    runner: (state, n, type) => {
      state.addNode(type, { target: n.value as string, embed: Boolean(n.embed) })
    },
  },
  toMarkdown: {
    match: (n: ProseNode) => n.type.name === "wikilink",
    runner: (state, n) => {
      state.addNode("wikilink", undefined, n.attrs.target, { embed: n.attrs.embed })
    },
  },
}))

export const CALLOUT_KINDS = ["note", "tip", "info", "warning", "danger", "success", "question", "quote"] as const

export const calloutSchema = $nodeSchema("callout", () => ({
  group: "block",
  content: "block+",
  defining: true,
  attrs: { kind: { default: "note" }, title: { default: "" }, fold: { default: "" } },
  parseDOM: [
    {
      tag: "div[data-callout]",
      getAttrs: (dom: HTMLElement | string) => (typeof dom === "string" ? {} : { kind: dom.getAttribute("data-callout") ?? "note" }),
    },
  ],
  toDOM: (node: ProseNode) => [
    "div",
    { "data-callout": node.attrs.kind, "data-title": node.attrs.title || node.attrs.kind.charAt(0).toUpperCase() + node.attrs.kind.slice(1), class: "lb-callout" },
    ["div", { class: "lb-callout-body" }, 0],
  ],
  parseMarkdown: {
    match: (n: { type: string }) => n.type === "callout",
    runner: (state, n, type) => {
      state.openNode(type, { kind: n.kind, title: n.title, fold: n.fold })
      const kids = (n.children ?? []) as never[]
      if (kids.length > 0) state.next(kids)
      else state.openNode(state.schema.nodes.paragraph).closeNode()
      state.closeNode()
    },
  },
  toMarkdown: {
    match: (n: ProseNode) => n.type.name === "callout",
    runner: (state, n) => {
      state.openNode("callout", undefined, { kind: n.attrs.kind, title: n.attrs.title, fold: n.attrs.fold })
      // A callout holding one empty paragraph has no body to write.
      const empty = n.childCount === 1 && n.firstChild?.type.name === "paragraph" && n.firstChild.content.size === 0
      if (!empty) state.next(n.content)
      state.closeNode()
    },
  },
}))

/* The to-markdown state is mdast-util-to-markdown's; typed loosely here. */
interface ToMdState {
  enter: (name: string) => () => void
  createTracker: (info: unknown) => { move: (s: string) => string; shift: (n: number) => void; current: () => unknown }
  containerFlow: (node: unknown, info: unknown) => string
  indentLines: (value: string, map: (line: string, index: number, blank: boolean) => string) => string
}

const quote = (line: string, _: number, blank: boolean) => ">" + (blank ? "" : " ") + line

/** Stringify options: Obsidian's bullets and rules, plus the two node writers. */
export function configureObsidian(ctx: Ctx) {
  ctx.update(remarkStringifyOptionsCtx, (prev) => ({
    ...prev,
    bullet: "-" as const,
    rule: "-" as const,
    handlers: {
      ...prev.handlers,
      wikilink: (node: MdNode) => `${node.embed ? "!" : ""}[[${node.value}]]`,
      callout: (node: MdNode, _: unknown, state: ToMdState, info: unknown) => {
        const head = `> [!${node.kind}]${node.fold ?? ""}${node.title ? ` ${node.title}` : ""}`
        const exit = state.enter("blockquote")
        const tracker = state.createTracker(info)
        tracker.move("> ")
        tracker.shift(2)
        const body = state.containerFlow(node, tracker.current())
        exit()
        return body.trim() === "" ? head : `${head}\n${state.indentLines(body, quote)}`
      },
    } as never,
  }))
}

export const obsidian = [obsidianRemark, wikilinkSchema, calloutSchema].flat()
