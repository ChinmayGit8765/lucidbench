import { Component, type ReactNode } from "react"
import { Ellipsis, ExternalLink, Trash2 } from "lucide-react"

import { Badge } from "@/components/ui/badge"
import { Card } from "@/components/ui/card"
import { Menu } from "@/components/ui/menu"
import { Skeleton } from "@/components/ui/states"
import { usePoll } from "@/lib/api"
import {
  CATALOG_PATH,
  evaluate,
  fieldLabel,
  getPath,
  safeLink,
  sectionURL,
  statValue,
  type Catalog,
  type Section,
  type SectionField,
} from "@/lib/sections"
import { absoluteTime, relativeTime, useNow } from "@/lib/time"
import { cn } from "@/lib/utils"

/** One value in a field's format, always as text (never as markup). */
export function FieldValue({ v, f, now }: { v: unknown; f: SectionField; now: number }) {
  if (v === undefined || v === null || v === "") return <span className="text-subtle-foreground">—</span>
  switch (f.format) {
    case "usd":
      return <span className="tabular-nums">{typeof v === "number" ? `$${v < 10 ? v.toFixed(2) : v.toFixed(0)}` : String(v)}</span>
    case "number":
      return <span className="tabular-nums">{typeof v === "number" ? v.toLocaleString(undefined, { maximumFractionDigits: 2 }) : String(v)}</span>
    case "percent":
      return <span className="tabular-nums">{typeof v === "number" ? `${Math.round(v)}%` : String(v)}</span>
    case "date": {
      const s = String(v)
      const t = Date.parse(s)
      return <span title={s}>{Number.isNaN(t) ? s : /^\d{4}-\d{2}-\d{2}$/.test(s) ? new Date(`${s}T12:00:00`).toLocaleDateString(undefined, { day: "numeric", month: "short" }) : new Date(t).toLocaleDateString(undefined, { day: "numeric", month: "short" })}</span>
    }
    case "relative":
      return typeof v === "string" && !Number.isNaN(Date.parse(v)) ? (
        <time dateTime={v} title={absoluteTime(v)}>
          {relativeTime(v, now)}
        </time>
      ) : (
        <span>{String(v)}</span>
      )
    case "link": {
      const href = safeLink(v)
      return href ? (
        <a href={href} target="_blank" rel="noreferrer noopener" className="inline-flex items-center gap-1 text-brand-fg hover:underline">
          {f.label || "open"} <ExternalLink className="size-3" />
        </a>
      ) : (
        <span className="text-subtle-foreground">—</span>
      )
    }
    case "badge":
      return <Badge>{String(v)}</Badge>
  }
  return <span>{typeof v === "object" ? JSON.stringify(v) : String(v)}</span>
}

function Body({ s, data, now, projectId }: { s: Section; data: unknown; now: number; projectId?: string }) {
  if (s.view === "stat") {
    const v = statValue(s, data, now, projectId)
    const f = s.fields[0]
    return (
      <div className="px-4 pb-4">
        <div className="text-3xl font-semibold tabular-nums tracking-tight" data-testid="section-stat">
          <FieldValue v={v} f={f} now={now} />
        </div>
        <div className="text-xs text-muted-foreground">{fieldLabel(f)}</div>
      </div>
    )
  }
  if (s.view === "markdown") {
    const f = s.fields[0]
    const v = getPath(s.rows ? evaluate(s, data, now, projectId)[0] : data, f.path)
    return (
      <div className="whitespace-pre-wrap break-words px-4 pb-4 text-sm leading-relaxed text-muted-foreground">
        {v === undefined || v === null ? "Nothing to show." : String(v)}
      </div>
    )
  }
  const rows = evaluate(s, data, now, projectId)
  if (rows.length === 0) return <p className="px-4 pb-4 text-sm text-muted-foreground">Nothing here right now.</p>
  if (s.view === "bars") {
    const [lf, vf] = s.fields
    const vals = rows.map((r) => {
      const n = getPath(r, vf.path)
      return typeof n === "number" ? n : 0
    })
    const max = Math.max(...vals, 0) || 1
    return (
      <ul className="space-y-1.5 px-4 pb-4">
        {rows.map((r, i) => (
          <li key={i} className="grid grid-cols-[minmax(0,7rem)_1fr_auto] items-center gap-2 text-xs">
            <span className="truncate text-muted-foreground">
              <FieldValue v={getPath(r, lf.path)} f={lf} now={now} />
            </span>
            <span className="h-2 overflow-hidden rounded-full bg-muted">
              <span className="block h-full rounded-full bg-brand" style={{ width: `${(vals[i] / max) * 100}%` }} />
            </span>
            <span className="font-medium">
              <FieldValue v={getPath(r, vf.path)} f={vf} now={now} />
            </span>
          </li>
        ))}
      </ul>
    )
  }
  if (s.view === "table") {
    return (
      <div className="overflow-x-auto px-2 pb-3">
        <table className="w-full text-left text-xs">
          <thead>
            <tr className="text-2xs uppercase tracking-[0.06em] text-subtle-foreground">
              {s.fields.map((f, i) => (
                <th key={i} className="px-2 py-1 font-medium">
                  {fieldLabel(f)}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {rows.map((r, i) => (
              <tr key={i} className="border-t">
                {s.fields.map((f, j) => (
                  <td key={j} className={cn("max-w-56 truncate px-2 py-1.5", j === 0 && "font-medium")}>
                    <FieldValue v={getPath(r, f.path)} f={f} now={now} />
                  </td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    )
  }
  const [first, ...rest] = s.fields
  return (
    <ul className="divide-y">
      {rows.map((r, i) => (
        <li key={i} className="flex items-center gap-2 px-4 py-2 text-sm">
          <span className="min-w-0 flex-1 truncate">
            <FieldValue v={getPath(r, first.path)} f={first} now={now} />
          </span>
          {rest.map((f, j) => (
            <span key={j} className="shrink-0 text-xs text-muted-foreground">
              <FieldValue v={getPath(r, f.path)} f={f} now={now} />
            </span>
          ))}
        </li>
      ))}
    </ul>
  )
}

/**
 * One section, live: it reads its allowlisted route every refresh_s seconds
 * and shows the answer in its view. projectId fills "{project}" on a
 * project's page. onRemove adds a remove item to its menu.
 */
export function SectionRenderer({
  section: s,
  projectId,
  onRemove,
  className,
}: {
  section: Section
  projectId?: string
  onRemove?: () => void
  className?: string
}) {
  const catalog = usePoll<Catalog>(CATALOG_PATH, 600_000)
  const url = catalog.data ? sectionURL(s, catalog.data.apis) : null
  const poll = usePoll<unknown>(url, Math.max(15, s.refresh_s ?? 60) * 1000)
  const now = useNow(30_000)
  const refused = catalog.data && !url
  return (
    <Card className={cn("flex flex-col overflow-hidden", className)} data-testid="section" aria-label={`Section: ${s.title}`}>
      <div className="flex items-start gap-2 px-4 pb-2 pt-3.5">
        <div className="min-w-0 flex-1">
          <h3 className="truncate text-sm font-semibold">{s.title}</h3>
          {s.description && <p className="truncate text-xs text-subtle-foreground">{s.description}</p>}
        </div>
        {onRemove && (
          <Menu
            label={`${s.title} menu`}
            trigger={<Ellipsis />}
            items={[{ label: "Remove section", icon: Trash2, danger: true, onSelect: onRemove }]}
          />
        )}
      </div>
      <div className="min-h-0 flex-1">
        {refused ? (
          <p className="px-4 pb-4 text-sm text-danger-fg">This section reads a route that is not allowed, so it is not shown.</p>
        ) : poll.error && !poll.data ? (
          <p className="px-4 pb-4 text-sm text-muted-foreground">Could not read {s.source.api}: {poll.error.message}</p>
        ) : poll.data === null ? (
          <div className="space-y-2 px-4 pb-4">
            <Skeleton className="h-5" />
            <Skeleton className="h-5 w-2/3" />
          </div>
        ) : (
          <SectionBoundary>
            <Body s={s} data={poll.data} now={now} projectId={projectId} />
          </SectionBoundary>
        )}
      </div>
    </Card>
  )
}

/** Renders a section against data already in hand (the generator's preview). */
export function SectionPreview({ section: s, data, projectId }: { section: Section; data: unknown; projectId?: string }) {
  const now = useNow(30_000)
  return (
    <SectionBoundary>
      <Body s={s} data={data} now={now} projectId={projectId} />
    </SectionBoundary>
  )
}

class SectionBoundary extends Component<{ children: ReactNode }, { failed: boolean }> {
  state = { failed: false }
  static getDerivedStateFromError() {
    return { failed: true }
  }
  render() {
    if (!this.state.failed) return this.props.children
    return <p className="px-4 pb-4 text-sm text-muted-foreground">This section could not be drawn from the answer it got.</p>
  }
}
