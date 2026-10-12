import type { ReactNode } from "react"
import { Check, FlaskConical } from "lucide-react"

import { cn } from "@/lib/utils"

/*
 * A council brief (or any card brief in the same shape) rendered as a page:
 * its sections, bullets, and done criteria as checkboxes with their proof.
 * Council, Ideas and the Work timeline all show briefs with it.
 */

export interface ParsedBrief {
  title: string
  sections: { heading: string; lines: string[] }[]
}

export function parseBrief(md: string): ParsedBrief {
  const out: ParsedBrief = { title: "", sections: [] }
  let cur: { heading: string; lines: string[] } | null = null
  for (const raw of md.replace(/\r\n/g, "\n").split("\n")) {
    const line = raw.trimEnd()
    if (line.startsWith("# ") && !out.title) {
      out.title = line.slice(2).trim()
      continue
    }
    const h = /^#{2,3} +(.+)$/.exec(line)
    if (h) {
      cur = { heading: h[1].trim(), lines: [] }
      out.sections.push(cur)
      continue
    }
    if (!cur) {
      if (line.trim()) {
        cur = { heading: "", lines: [] }
        out.sections.push(cur)
      } else continue
    }
    cur.lines.push(line)
  }
  return out
}

/** **bold**, `code` and [[links]] inside a line. */
export function Inline({ text }: { text: string }) {
  const parts = text.split(/(\*\*[^*]+\*\*|`[^`]+`|\[\[[^\]]+\]\])/g)
  return (
    <>
      {parts.map((p, i) => {
        if (p.startsWith("**") && p.endsWith("**") && p.length > 4) return <strong key={i} className="font-semibold">{p.slice(2, -2)}</strong>
        if (p.startsWith("`") && p.endsWith("`") && p.length > 2)
          return (
            <code key={i} className="rounded bg-muted px-1 py-px font-mono text-[0.85em]">
              {p.slice(1, -1)}
            </code>
          )
        if (p.startsWith("[[") && p.endsWith("]]")) return <span key={i} className="text-brand-fg">{p.slice(2, -2).split("|").pop()}</span>
        return <span key={i}>{p}</span>
      })}
    </>
  )
}

const CRITERION_RE = /^[-*] \[( |x|X)\] *(.*)$/
const BULLET_RE = /^[-*] +(.*)$/
const ORDERED_RE = /^(\d+)[.)] +(.*)$/

function Criterion({ text, done }: { text: string; done: boolean }) {
  // "D1 — outcome — proof: how"
  const m = /^(D\d+)\s*[—–-]+\s*(.*)$/.exec(text)
  const tag = m?.[1]
  let rest = m ? m[2] : text
  let proof = ""
  const pi = rest.toLowerCase().lastIndexOf("proof:")
  if (pi >= 0) {
    proof = rest.slice(pi + 6).trim()
    rest = rest.slice(0, pi).replace(/\s*[—–-]+\s*$/, "").trim()
  }
  return (
    <li className="flex items-start gap-3 py-2.5">
      <span
        className={cn(
          "mt-0.5 flex size-4 shrink-0 items-center justify-center rounded border",
          done ? "border-success/50 bg-success-soft text-success" : "border-border-strong bg-background",
        )}
      >
        {done && <Check className="size-3" />}
      </span>
      <div className="min-w-0 flex-1">
        <div className="text-sm leading-snug">
          {tag && <span className="mr-2 font-mono text-2xs font-semibold text-brand-fg">{tag}</span>}
          <Inline text={rest} />
        </div>
        {proof ? (
          <div className="mt-1 flex items-start gap-1.5 text-xs text-muted-foreground">
            <FlaskConical className="mt-px size-3.5 shrink-0 text-subtle-foreground" />
            <span>
              <span className="text-subtle-foreground">Proof: </span>
              <Inline text={proof} />
            </span>
          </div>
        ) : (
          <div className="mt-1 text-xs text-warning-fg">No proof given</div>
        )}
      </div>
    </li>
  )
}

function Lines({ lines }: { lines: string[] }) {
  const blocks: ReactNode[] = []
  let list: { kind: "ul" | "ol" | "crit"; items: ReactNode[] } | null = null
  let para: string[] = []
  const flushPara = () => {
    if (para.length) blocks.push(<p key={`p${blocks.length}`} className="text-sm leading-relaxed"><Inline text={para.join(" ")} /></p>)
    para = []
  }
  const flushList = () => {
    if (!list) return
    const k = `l${blocks.length}`
    if (list.kind === "crit") blocks.push(<ul key={k} className="divide-y">{list.items}</ul>)
    else if (list.kind === "ol") blocks.push(<ol key={k} className="list-decimal space-y-1 pl-5 text-sm leading-relaxed marker:text-subtle-foreground">{list.items}</ol>)
    else blocks.push(<ul key={k} className="list-disc space-y-1 pl-5 text-sm leading-relaxed marker:text-subtle-foreground">{list.items}</ul>)
    list = null
  }
  const push = (kind: "ul" | "ol" | "crit", node: ReactNode) => {
    flushPara()
    if (list && list.kind !== kind) flushList()
    list ??= { kind, items: [] }
    list.items.push(node)
  }
  lines.forEach((raw, i) => {
    const line = raw.trim()
    let m: RegExpExecArray | null
    if (!line) {
      flushPara()
      flushList()
    } else if ((m = CRITERION_RE.exec(line))) push("crit", <Criterion key={i} text={m[2]} done={m[1] !== " "} />)
    else if ((m = BULLET_RE.exec(line))) push("ul", <li key={i}><Inline text={m[1]} /></li>)
    else if ((m = ORDERED_RE.exec(line))) push("ol", <li key={i}><Inline text={m[2]} /></li>)
    else if (list && /^\s{2,}/.test(raw)) {
      // a wrapped list item: append to the previous one
      list.items.push(<div key={`c${i}`} className="text-sm text-muted-foreground"><Inline text={line} /></div>)
    } else {
      flushList()
      para.push(line)
    }
  })
  flushPara()
  flushList()
  return <div className="space-y-2">{blocks}</div>
}

const PAIRS: [string, string][] = [
  ["problem", "outcome"],
  ["risks", "first steps"],
]

function Section({ heading, lines, compact }: { heading: string; lines: string[]; compact?: boolean }) {
  return (
    <section className="min-w-0">
      {heading && <h3 className={cn("mb-1.5 font-semibold uppercase tracking-wider text-subtle-foreground", compact ? "text-2xs" : "text-2xs")}>{heading}</h3>}
      <Lines lines={lines} />
    </section>
  )
}

export function BriefBody({ b, compact }: { b: ParsedBrief; compact?: boolean }) {
  const byName = new Map(b.sections.map((s) => [s.heading.toLowerCase(), s]))
  const used = new Set<string>()
  const blocks: ReactNode[] = []
  for (const s of b.sections) {
    const key = s.heading.toLowerCase()
    if (used.has(key)) continue
    const pair = PAIRS.find((p) => p[0] === key)
    const partner = pair ? byName.get(pair[1]) : undefined
    if (pair && partner) {
      used.add(key)
      used.add(pair[1])
      blocks.push(
        <div key={key} className={cn("grid gap-x-8 gap-y-5", !compact && "@2xl:grid-cols-2")}>
          <Section heading={s.heading} lines={s.lines} compact={compact} />
          <Section heading={partner.heading} lines={partner.lines} compact={compact} />
        </div>,
      )
      continue
    }
    used.add(key)
    blocks.push(
      key === "done criteria" && !compact ? (
        <div key={key} className="rounded-lg border bg-background/40 px-4 pb-1 pt-3">
          <Section heading={s.heading} lines={s.lines} />
        </div>
      ) : (
        <Section key={key || `s${blocks.length}`} heading={s.heading} lines={s.lines} compact={compact} />
      ),
    )
  }
  return <div className={cn(compact ? "space-y-4" : "space-y-6")}>{blocks}</div>
}
