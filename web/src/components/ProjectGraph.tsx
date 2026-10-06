import { useMemo, useState } from "react"

import { categoryColor, categoryInfo, NEED, STATUS, type NeedStatus, type Project } from "@/lib/projects"
import { cn, plural } from "@/lib/utils"

/*
 * "What needs what": a layered left-to-right graph. A project sits one
 * column to the right of everything that builds into it or supplies one of
 * its needs (longest path), so private tools land on the left and the
 * products and portfolio pieces they feed land on the right. Rows within a
 * column are ordered by two barycentre sweeps to keep edges short. No graph
 * library: the layout is deterministic and small.
 */

const NODE_W = 196
const NODE_H = 46
const COL_GAP = 92
const ROW_GAP = 14
const PAD = 16
const HEAD = 26

export interface GraphEdge {
  from: string
  to: string
  kind: "builds" | "need"
  status?: NeedStatus
  what?: string
}

export function graphEdges(ps: Project[]): GraphEdge[] {
  const ids = new Set(ps.map((p) => p.id))
  const out: GraphEdge[] = []
  for (const p of ps) {
    for (const t of p.builds_into) if (ids.has(t) && t !== p.id) out.push({ from: p.id, to: t, kind: "builds" })
    for (const n of p.needs)
      if (n.from && ids.has(n.from) && n.from !== p.id)
        out.push({ from: n.from, to: p.id, kind: "need", status: n.status, what: n.what })
  }
  return out
}

interface Placed {
  p: Project
  col: number
  x: number
  y: number
}

function layout(ps: Project[], edges: GraphEdge[]) {
  const preds = new Map<string, string[]>()
  const succs = new Map<string, string[]>()
  for (const p of ps) {
    preds.set(p.id, [])
    succs.set(p.id, [])
  }
  for (const e of edges) {
    if (!preds.get(e.to)!.includes(e.from)) preds.get(e.to)!.push(e.from)
    if (!succs.get(e.from)!.includes(e.to)) succs.get(e.from)!.push(e.to)
  }
  // Longest path from the sources; a cycle edge is ignored where it closes.
  const layer = new Map<string, number>()
  const visiting = new Set<string>()
  const depth = (id: string): number => {
    if (layer.has(id)) return layer.get(id)!
    if (visiting.has(id)) return 0
    visiting.add(id)
    let d = 0
    for (const f of preds.get(id)!) if (!visiting.has(f)) d = Math.max(d, depth(f) + 1)
    visiting.delete(id)
    layer.set(id, d)
    return d
  }
  const catOrder = (p: Project) => {
    const i = ["tool", "experiment", "coursework", "product", "portfolio"].indexOf(p.category)
    return i < 0 ? 9 : i
  }
  const sorted = [...ps].sort((a, b) => catOrder(a) - catOrder(b) || a.name.localeCompare(b.name))
  sorted.forEach((p) => depth(p.id))
  const cols: Project[][] = []
  for (const p of sorted) (cols[layer.get(p.id)!] ??= []).push(p)

  // Barycentre sweeps: forward by predecessors, then back by successors.
  const pos = new Map<string, number>()
  const index = () => cols.forEach((c) => c.forEach((p, i) => pos.set(p.id, i)))
  index()
  const bary = (ids: string[], self: number) =>
    ids.length ? ids.reduce((s, id) => s + (pos.get(id) ?? 0), 0) / ids.length : self
  for (let sweep = 0; sweep < 2; sweep++) {
    for (let c = 1; c < cols.length; c++) {
      cols[c] = cols[c]
        .map((p, i) => ({ p, k: bary(preds.get(p.id)!, i) }))
        .sort((a, b) => a.k - b.k)
        .map((x) => x.p)
      index()
    }
    for (let c = cols.length - 2; c >= 0; c--) {
      cols[c] = cols[c]
        .map((p, i) => ({ p, k: bary(succs.get(p.id)!, i) }))
        .sort((a, b) => a.k - b.k)
        .map((x) => x.p)
      index()
    }
  }

  const tallest = Math.max(1, ...cols.map((c) => c.length))
  const height = tallest * NODE_H + (tallest - 1) * ROW_GAP
  const placed = new Map<string, Placed>()
  cols.forEach((c, ci) => {
    const h = c.length * NODE_H + (c.length - 1) * ROW_GAP
    const top = PAD + HEAD + (height - h) / 2
    c.forEach((p, ri) => placed.set(p.id, { p, col: ci, x: PAD + ci * (NODE_W + COL_GAP), y: top + ri * (NODE_H + ROW_GAP) }))
  })
  return {
    cols,
    placed,
    width: PAD * 2 + cols.length * NODE_W + Math.max(0, cols.length - 1) * COL_GAP,
    height: PAD * 2 + HEAD + height,
  }
}

/** The category that most nodes in a column share, for the column label. */
function columnLabel(c: Project[]): string {
  const n = new Map<string, number>()
  for (const p of c) n.set(p.category, (n.get(p.category) ?? 0) + 1)
  const cats = [...n.entries()].sort((a, b) => b[1] - a[1]).map(([k]) => categoryInfo(k)?.label ?? k)
  return cats.slice(0, 2).join(" · ")
}

function clip(s: string, max: number) {
  return s.length > max ? `${s.slice(0, max - 1)}…` : s
}

export function ProjectGraph({ projects, onOpen }: { projects: Project[]; onOpen: (id: string) => void }) {
  const [hover, setHover] = useState<string | null>(null)
  const edges = useMemo(() => graphEdges(projects), [projects])
  const linked = useMemo(() => {
    const s = new Set<string>()
    for (const e of edges) s.add(e.from).add(e.to)
    return projects.filter((p) => s.has(p.id))
  }, [projects, edges])
  const g = useMemo(() => layout(linked, edges), [linked, edges])

  if (linked.length === 0) return null

  const near = (id: string) =>
    hover === null || id === hover || edges.some((e) => (e.from === hover && e.to === id) || (e.to === hover && e.from === id))
  const lit = (e: GraphEdge) => hover === null || e.from === hover || e.to === hover

  // Edge order: builds first, then needs by severity, so blocked draws on top.
  const rank = (e: GraphEdge) => (e.kind === "builds" ? 0 : { done: 1, todo: 2, doing: 3, blocked: 4 }[e.status ?? "todo"])
  const drawn = [...edges].sort((a, b) => rank(a) - rank(b))
  // Spread each node's ports along its side, ordered by the other end's
  // height, so arrows into one node never pile onto a single point.
  const port = new Map<GraphEdge, { out: number; in: number }>()
  const spread = (list: GraphEdge[], key: "out" | "in") => {
    const step = Math.min(8, (NODE_H - 18) / Math.max(1, list.length - 1))
    list.forEach((e, i) => {
      const p = port.get(e) ?? { out: 0, in: 0 }
      p[key] = (i - (list.length - 1) / 2) * step
      port.set(e, p)
    })
  }
  const yOf = (id: string) => g.placed.get(id)?.y ?? 0
  for (const id of g.placed.keys()) {
    spread(edges.filter((e) => e.from === id).sort((a, b) => yOf(a.to) - yOf(b.to)), "out")
    spread(edges.filter((e) => e.to === id).sort((a, b) => yOf(a.from) - yOf(b.from)), "in")
  }

  return (
    <div className="overflow-x-auto">
      <svg
        width={g.width}
        height={g.height}
        viewBox={`0 0 ${g.width} ${g.height}`}
        role="img"
        aria-label={`Dependency graph of ${plural(linked.length, "project")}`}
        style={{ maxWidth: "100%", height: "auto" }}
        className="block font-sans"
      >
        <defs>
          {(["builds", "todo", "doing", "done", "blocked"] as const).map((k) => (
            <marker
              key={k}
              id={`arrow-${k}`}
              viewBox="0 0 8 8"
              refX="7"
              refY="4"
              markerWidth="5"
              markerHeight="5"
              orient="auto-start-reverse"
            >
              <path d="M0.5 1 L7 4 L0.5 7" fill="none" stroke={k === "builds" ? "var(--subtle-foreground)" : NEED[k].color} strokeWidth="1.4" strokeLinecap="round" strokeLinejoin="round" />
            </marker>
          ))}
        </defs>

        {g.cols.map((c, ci) => (ci > 0 && columnLabel(c) === columnLabel(g.cols[ci - 1]) ? null :
          <text
            key={ci}
            x={PAD + ci * (NODE_W + COL_GAP) + 2}
            y={PAD + 10}
            className="fill-[var(--subtle-foreground)] text-[10px] font-medium uppercase tracking-[0.08em]"
          >
            {columnLabel(c)}
          </text>
        ))}

        {drawn.map((e, i) => {
          const a = g.placed.get(e.from)!
          const b = g.placed.get(e.to)!
          const pt = port.get(e) ?? { out: 0, in: 0 }
          const x1 = a.x + NODE_W
          const y1 = a.y + NODE_H / 2 + pt.out
          const x2 = b.x - 2
          const y2 = b.y + NODE_H / 2 + pt.in
          const back = x2 <= x1
          const dx = back ? 60 : Math.max(36, (x2 - x1) / 2)
          const d = back
            ? `M${x1} ${y1} C${x1 + dx} ${y1}, ${b.x - dx} ${y2}, ${x2} ${y2}`
            : `M${x1} ${y1} C${x1 + dx} ${y1}, ${x2 - dx} ${y2}, ${x2} ${y2}`
          const color = e.kind === "builds" ? "var(--border-strong)" : NEED[e.status ?? "todo"].color
          const marker = e.kind === "builds" ? "builds" : (e.status ?? "todo")
          return (
            <path
              key={i}
              d={d}
              fill="none"
              stroke={hover !== null && lit(e) && e.kind === "builds" ? "var(--subtle-foreground)" : color}
              strokeWidth={e.kind === "need" && e.status === "blocked" ? 2 : 1.5}
              strokeDasharray={e.kind === "need" && e.status === "todo" ? "4 4" : undefined}
              markerEnd={`url(#arrow-${marker})`}
              opacity={lit(e) ? 1 : 0.15}
              className="transition-opacity duration-150"
            >
              <title>
                {e.kind === "builds"
                  ? `${a.p.name} builds into ${b.p.name}`
                  : `${b.p.name} needs "${e.what}" from ${a.p.name} (${NEED[e.status ?? "todo"].label.toLowerCase()})`}
              </title>
            </path>
          )
        })}

        {[...g.placed.values()].map(({ p, x, y }) => {
          const blocked = p.needs.filter((n) => n.status === "blocked").length
          const st = STATUS[p.status]
          return (
            <g
              key={p.id}
              transform={`translate(${x} ${y})`}
              opacity={near(p.id) ? 1 : 0.35}
              onMouseEnter={() => setHover(p.id)}
              onMouseLeave={() => setHover(null)}
              onClick={() => onOpen(p.id)}
              onKeyDown={(e) => (e.key === "Enter" || e.key === " ") && onOpen(p.id)}
              tabIndex={0}
              role="button"
              aria-label={`${p.name}, ${categoryInfo(p.category)?.one ?? p.category}${blocked ? `, ${blocked} blocked` : ""}. Open card`}
              className="cursor-pointer outline-none transition-opacity duration-150 [&:focus-visible>rect:first-child]:stroke-[var(--ring)]"
            >
              <rect
                width={NODE_W}
                height={NODE_H}
                rx={9}
                fill="var(--card)"
                stroke={hover === p.id ? "var(--subtle-foreground)" : "var(--border-strong)"}
                strokeWidth={hover === p.id ? 1.5 : 1}
              />
              <rect x={0.5} y={9} width={3} height={NODE_H - 18} rx={1.5} fill={categoryColor(p.category)} />
              <text x={14} y={19} className="fill-[var(--foreground)] text-[12px] font-medium">
                {clip(p.name, blocked ? 22 : 25)}
              </text>
              <text x={14} y={34} className="fill-[var(--subtle-foreground)] text-[10.5px]">
                {clip(`${categoryInfo(p.category)?.one ?? p.category} · ${st?.label.toLowerCase() ?? p.status}`, 30)}
                {p.progress.total > 0 && ` · ${p.progress.done}/${p.progress.total}`}
              </text>
              {blocked > 0 && (
                <g transform={`translate(${NODE_W - 24} 8)`}>
                  <rect width={16} height={14} rx={7} fill="var(--warning-soft)" />
                  <text x={8} y={10.5} textAnchor="middle" className="fill-[var(--warning-fg)] text-[9.5px] font-semibold">
                    {blocked}
                  </text>
                </g>
              )}
              <title>{p.summary ? `${p.name}: ${p.summary}` : p.name}</title>
            </g>
          )
        })}
      </svg>
    </div>
  )
}

/** The graph's key: builds-into links and need edges by status. */
export function GraphLegend({ className }: { className?: string }) {
  const item = "flex items-center gap-1.5"
  return (
    <div className={cn("flex flex-wrap items-center gap-x-4 gap-y-1.5 text-xs text-muted-foreground", className)}>
      <span className={item}>
        <svg width="22" height="8" aria-hidden>
          <path d="M1 4 H20" stroke="var(--border-strong)" strokeWidth="1.5" />
        </svg>
        builds into
      </span>
      {(["todo", "doing", "done", "blocked"] as const).map((s) => (
        <span key={s} className={item}>
          <svg width="22" height="8" aria-hidden>
            <path d="M1 4 H20" stroke={NEED[s].color} strokeWidth={s === "blocked" ? 2 : 1.5} strokeDasharray={s === "todo" ? "4 4" : undefined} />
          </svg>
          need · {NEED[s].label.toLowerCase()}
        </span>
      ))}
    </div>
  )
}
