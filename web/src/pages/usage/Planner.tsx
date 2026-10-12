import { useState } from "react"
import { CalendarClock, ListPlus, Plus, Trash2, TriangleAlert } from "lucide-react"
import { toast } from "sonner"

import { ProviderTile } from "@/components/ProviderMark"
import { StatusPill, type Tone } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card } from "@/components/ui/card"
import { ErrorState, Skeleton } from "@/components/ui/states"
import { errorMessage, getJSON, usePoll } from "@/lib/api"
import { NEXTUP_PATH, type NextView } from "@/lib/nextup"
import { absoluteTime } from "@/lib/time"
import {
  fmtTokens,
  gaugeTone,
  PLAN_KINDS,
  PLAN_PROVIDERS,
  planURL,
  savePlan,
  USAGE_POLL_MS,
  type Pace,
  type PlanItem,
  type PlanKind,
  type PlannedItem,
  type PlanView,
} from "@/lib/usage"
import { cn } from "@/lib/utils"

const field =
  "h-8 rounded-md border bg-background/60 px-2 text-sm outline-none transition-colors hover:border-border-strong focus-visible:border-border-strong focus-visible:ring-2 focus-visible:ring-ring/30"

const HOURS = [1, 2, 6]

const VERDICT: Record<PlannedItem["verdict"], { tone: Tone; label: string }> = {
  fits: { tone: "success", label: "fits" },
  after_reset: { tone: "warning", label: "after reset" },
  too_big: { tone: "danger", label: "too big" },
  unknown: { tone: "neutral", label: "unknown" },
}

/** Next up actions that spend, and the plan kind each one runs. */
const NEXTUP_KIND: Record<string, PlanKind> = { start_work: "work", fix_ci: "work", resume_work: "work", run_council: "council" }

/* ---------- pace ---------- */

function PaceGauge({ p }: { p: Pace }) {
  const pct = p.used_percent
  const label = `${p.provider}${p.account && p.account !== "default" ? ` (${p.account})` : ""} · ${p.window}`
  return (
    <div data-testid="pace" className="bg-card p-5">
      <div className="flex items-baseline justify-between gap-3">
        <span className="flex items-center gap-2 text-sm font-medium">
          <ProviderTile provider={p.provider} size="sm" />
          {label}
          {p.basis === "estimated" && <span className="text-2xs font-normal text-subtle-foreground">estimated from your logs</span>}
        </span>
        <span className="text-sm tabular-nums text-muted-foreground">{pct !== null ? `${Math.round(pct)}% used` : `${fmtTokens(p.used_tokens)} used`}</span>
      </div>
      <div
        role="progressbar"
        aria-label={`${label} used`}
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={pct === null ? undefined : Math.round(pct)}
        className="mt-1.5 h-2 overflow-hidden rounded-full bg-muted"
      >
        <div className={cn("h-full rounded-full transition-[width]", gaugeTone(pct ?? 0))} style={{ width: `${Math.min(100, pct ?? 0)}%` }} />
      </div>
      <p className={cn("mt-2 text-sm", p.verdict === "runs_out" ? "text-warning-fg" : "text-muted-foreground")} data-testid="pace-summary">
        {p.summary}
      </p>
      <p className="mt-0.5 text-xs text-subtle-foreground">
        {p.burn_per_hour > 0 ? `About ${fmtTokens(p.burn_per_hour)} tokens an hour over the last ${p.hours} h.` : `Nothing spent in the last ${p.hours} h.`}
        {p.resets_at ? <span title={absoluteTime(p.resets_at)}> Resets {new Date(p.resets_at).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}.</span> : null}
      </p>
    </div>
  )
}

/* ---------- plan ---------- */

function ItemRow({ it, onChange, onRemove }: { it: PlannedItem; onChange: (p: Partial<PlanItem>) => void; onRemove: () => void }) {
  const v = VERDICT[it.verdict]
  const est = it.estimate.basis === "none" ? "no estimate yet" : `~${fmtTokens(it.estimate.tokens)} tokens${it.estimate.cost_usd ? ` · ~$${it.estimate.cost_usd.toFixed(2)}` : ""}`
  return (
    <li data-testid="plan-item" className="space-y-1.5 border-t px-5 py-3">
      <div className="flex flex-wrap items-center gap-2">
        <span className="min-w-0 flex-1 truncate text-sm font-medium" data-testid="plan-item-title">
          {it.title}
        </span>
        {it.source === "nextup" && <StatusPill tone="neutral">from Next up</StatusPill>}
        <StatusPill tone={v.tone}>
          <span data-testid="plan-verdict">{v.label}</span>
        </StatusPill>
        <button aria-label={`Remove ${it.title}`} onClick={onRemove} className="rounded p-1 text-subtle-foreground hover:bg-accent hover:text-foreground">
          <Trash2 className="size-3.5" />
        </button>
      </div>
      <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
        <select aria-label="Kind" className={field} value={it.kind} onChange={(e) => onChange({ kind: e.target.value as PlanKind })}>
          {PLAN_KINDS.map((k) => (
            <option key={k.id} value={k.id}>
              {k.label}
            </option>
          ))}
        </select>
        <select aria-label="Provider" className={field} value={it.provider ?? ""} onChange={(e) => onChange({ provider: e.target.value || undefined })}>
          <option value="">Suggest ({it.use || "none"})</option>
          {PLAN_PROVIDERS.map((p) => (
            <option key={p} value={p}>
              {p}
            </option>
          ))}
        </select>
        <span className="tabular-nums" title={`From ${it.estimate.runs} recent ${it.estimate.runs === 1 ? "run" : "runs"} (${it.estimate.basis})`}>
          {est}
        </span>
      </div>
      <p className="text-xs text-muted-foreground" data-testid="plan-fit">
        {it.suggested && it.use ? (
          <>
            Use <span className="font-medium text-foreground" data-testid="plan-use">{it.use}</span>
            {it.reason ? ` (${it.reason})` : ""}.{" "}
          </>
        ) : null}
        {it.fit}
      </p>
      {it.warnings.map((w) => (
        <p key={w} className="flex items-center gap-1.5 text-xs text-warning-fg" data-testid="plan-warning">
          <TriangleAlert className="size-3.5 shrink-0" /> {w}
        </p>
      ))}
    </li>
  )
}

function PlanCard({ view, hours, onView }: { view: PlanView; hours: number; onView: (v: PlanView) => void }) {
  const [title, setTitle] = useState("")
  const [kind, setKind] = useState<PlanKind>("work")
  const [busy, setBusy] = useState(false)
  const save = async (items: PlanItem[]) => {
    setBusy(true)
    try {
      onView(await savePlan(items, hours))
    } catch (e) {
      toast.error(errorMessage(e))
    } finally {
      setBusy(false)
    }
  }
  const items: PlanItem[] = view.items
  const add = () => {
    const t = title.trim()
    if (!t) return
    setTitle("")
    void save([...items, { title: t, kind }])
  }
  const fromNextUp = async () => {
    try {
      const nv = await getJSON<NextView>(NEXTUP_PATH)
      const have = new Set(items.map((i) => i.nextup_id).filter(Boolean))
      const add: PlanItem[] = []
      for (const n of nv.items) {
        const k = NEXTUP_KIND[n.action.kind]
        if (!k || have.has(n.id) || !n.action.spends) continue
        add.push({ title: n.title.slice(0, 200), kind: k, project: n.project, source: "nextup", nextup_id: n.id })
        if (add.length === 5) break
      }
      if (!add.length) {
        toast.message("Next up has nothing new that would spend.")
        return
      }
      await save([...items, ...add])
    } catch (e) {
      toast.error(errorMessage(e))
    }
  }
  return (
    <Card className="overflow-hidden" data-testid="plan-card">
      <div className="flex flex-wrap items-start justify-between gap-3 px-5 pb-3 pt-4">
        <div>
          <h2 className="flex items-center gap-2 text-sm font-semibold">
            <CalendarClock className="size-4 text-brand" /> Plan
          </h2>
          <p className="mt-0.5 text-sm text-muted-foreground">
            What you mean to do today. Each item is estimated from your recent runs of the same kind, and checked against the window it would spend from.
          </p>
        </div>
        <Button size="sm" variant="secondary" onClick={() => void fromNextUp()} disabled={busy} data-testid="plan-from-nextup">
          <ListPlus /> Add from Next up
        </Button>
      </div>
      {view.windows.length > 0 && (
        <ul className="space-y-1 border-t bg-muted/20 px-5 py-3 text-sm" data-testid="plan-windows">
          {view.windows.map((w) => (
            <li key={w.provider} className="flex items-center gap-2">
              <ProviderTile provider={w.provider} size="sm" />
              <span className="text-muted-foreground">{w.summary}</span>
            </li>
          ))}
        </ul>
      )}
      {view.items.length === 0 ? (
        <p className="border-t px-5 py-5 text-sm text-muted-foreground">Nothing planned yet. Add what you mean to run today, or take the top of Next up.</p>
      ) : (
        <ul>
          {view.items.map((it, i) => (
            <ItemRow
              key={it.id}
              it={it}
              onChange={(p) => void save(items.map((x, j) => (j === i ? { ...x, ...p } : x)))}
              onRemove={() => void save(items.filter((_, j) => j !== i))}
            />
          ))}
        </ul>
      )}
      <form
        className="flex flex-wrap items-center gap-2 border-t px-5 py-3"
        onSubmit={(e) => {
          e.preventDefault()
          add()
        }}
      >
        <input
          aria-label="What do you plan to do?"
          placeholder="What do you plan to do?"
          className={cn(field, "min-w-48 flex-1")}
          maxLength={200}
          value={title}
          onChange={(e) => setTitle(e.target.value)}
          data-testid="plan-title"
        />
        <select aria-label="New item kind" className={field} value={kind} onChange={(e) => setKind(e.target.value as PlanKind)} data-testid="plan-kind">
          {PLAN_KINDS.map((k) => (
            <option key={k.id} value={k.id}>
              {k.label}
            </option>
          ))}
        </select>
        <Button size="sm" type="submit" disabled={busy || !title.trim()} data-testid="plan-add">
          <Plus /> Add
        </Button>
      </form>
    </Card>
  )
}

/** Pace per account and the day's plan. Everything here is worked out locally; no provider is asked. */
export function Planner() {
  const [hours, setHours] = useState(2)
  const poll = usePoll<PlanView>(planURL(hours), USAGE_POLL_MS)
  const [saved, setSaved] = useState<PlanView | null>(null)
  // A save answers with the new view; it stands until the next poll lands.
  const view = saved && (!poll.data || saved.generated_at >= poll.data.generated_at) && saved.hours === hours ? saved : poll.data
  return (
    <section className="space-y-4" aria-label="Planning">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h2 className="text-sm font-semibold">Pace and plan</h2>
        <div role="group" aria-label="Pace over" className="flex items-center gap-1 text-xs text-muted-foreground">
          Pace over
          {HOURS.map((h) => (
            <Button key={h} size="sm" variant={hours === h ? "secondary" : "ghost"} aria-pressed={hours === h} onClick={() => setHours(h)}>
              {h} h
            </Button>
          ))}
        </div>
      </div>
      {poll.error && !view && <ErrorState title="Could not work out the plan" message={poll.error.message} onRetry={poll.refresh} />}
      {!view && !poll.error && <Skeleton className="h-40 rounded-xl" />}
      {view && (
        <>
          {view.paces.length > 0 ? (
            <Card className="overflow-hidden">
              <div className="grid gap-px bg-border @3xl:grid-cols-2">
                {view.paces.map((p) => (
                  <PaceGauge key={`${p.provider}/${p.account}/${p.window}`} p={p} />
                ))}
              </div>
            </Card>
          ) : (
            <p className="text-sm text-muted-foreground">No limit window is running right now, so there is no pace to project.</p>
          )}
          <PlanCard view={view} hours={hours} onView={setSaved} />
        </>
      )}
    </section>
  )
}
