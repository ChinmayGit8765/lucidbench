import { useState } from "react"
import { Gauge, ShieldCheck } from "lucide-react"

import { ProviderTile } from "@/components/ProviderMark"
import { PageHeader, RefreshButton } from "@/components/Shell"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card } from "@/components/ui/card"
import { EmptyState, ErrorState, Skeleton } from "@/components/ui/states"
import { usePoll } from "@/lib/api"
import { usePrefs } from "@/lib/prefs"
import { absoluteTime, relativeTime, useNow } from "@/lib/time"
import {
  fmtTokens,
  livePercent,
  resetsIn,
  usageURL,
  USAGE_POLL_MS,
  USAGE_WARN_PERCENT,
  type UsageBucket,
  type UsageDay,
  type UsageOwn,
  type UsageProvider,
  type UsageSummary,
  type UsageWindow,
} from "@/lib/usage"
import { cn } from "@/lib/utils"

/** The stacked series, bottom to top. Colours come from the theme's status tokens. */
const SERIES = [
  { key: "input", label: "Input", color: "var(--info)" },
  { key: "output", label: "Output", color: "var(--success)" },
  { key: "cache_write", label: "Cache write", color: "var(--warning)" },
  { key: "cache_read", label: "Cache read", color: "var(--neutral)" },
] as const

type SeriesKey = (typeof SERIES)[number]["key"]

const barTone = (pct: number) => (pct >= USAGE_WARN_PERCENT ? "bg-danger" : pct >= 60 ? "bg-warning" : "bg-brand")

/* ---------- limit windows ---------- */

function Gauge1({ w, now }: { w: UsageWindow; now: number }) {
  const pct = livePercent(w)
  const reset = resetsIn(w.resets_at, now)
  return (
    <div>
      <div className="flex items-baseline justify-between gap-3">
        <span className="text-sm font-medium capitalize">{w.label}</span>
        <span className={cn("text-sm tabular-nums", pct !== null && pct >= USAGE_WARN_PERCENT ? "font-semibold text-danger-fg" : "text-muted-foreground")}>
          {pct !== null ? `${Math.round(pct)}% used` : w.stale ? "reset since last seen" : "unknown"}
        </span>
      </div>
      <div
        role="progressbar"
        aria-label={`${w.label} window used`}
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={pct === null ? undefined : Math.round(pct)}
        className="mt-1.5 h-2 overflow-hidden rounded-full bg-muted"
      >
        <div className={cn("h-full rounded-full transition-[width]", barTone(pct ?? 0))} style={{ width: `${Math.min(100, pct ?? 0)}%` }} />
      </div>
      <div className="mt-1 text-xs text-subtle-foreground">
        {w.stale && w.used_percent !== null ? `Was ${Math.round(w.used_percent)}% before the reset. ` : ""}
        {reset ? (reset === "now" ? "Resets now." : `Resets in ${reset}.`) : "Reset time unknown."}
        <span title={absoluteTime(w.observed_at)}> Last seen {relativeTime(w.observed_at, now)}.</span>
      </div>
    </div>
  )
}

function WindowsCard({ providers, title, now }: { providers: UsageProvider[]; title: string; now: number }) {
  return (
    <Card className="overflow-hidden">
      <div className="px-5 pb-3 pt-4">
        <h2 className="text-sm font-semibold">{title}</h2>
        <p className="mt-0.5 text-sm text-muted-foreground">How much of each limit window you have used, as the CLI last reported it.</p>
      </div>
      <div className="grid gap-px border-t bg-border @3xl:grid-cols-2">
        {providers.map((p) => (
          <div key={p.id} className="bg-card p-5">
            <div className="mb-3 flex items-center gap-2.5">
              <ProviderTile provider={p.id} size="sm" muted={p.windows.length === 0} />
              <span className="text-sm font-semibold">{p.label}</span>
              {p.status === "unavailable" && <Badge>not available yet</Badge>}
            </div>
            {p.windows.length > 0 ? (
              <div className="space-y-4">
                {p.windows.map((w) => (
                  <Gauge1 key={w.name} w={w} now={now} />
                ))}
              </div>
            ) : (
              <p className="text-sm text-muted-foreground">
                {p.status === "unavailable"
                  ? "Lucidbench has no local source for this yet."
                  : (p.window_note ?? "No window reported.")}
              </p>
            )}
          </div>
        ))}
      </div>
    </Card>
  )
}

/* ---------- chart ---------- */

function dayLabel(date: string): string {
  const d = new Date(`${date}T12:00:00`)
  return d.toLocaleDateString(undefined, { month: "short", day: "numeric" })
}

/** Stacked daily bars. Plain divs, so they follow the theme and need no chart library. */
function DailyBars({ daily, keys, name }: { daily: UsageDay[]; keys: SeriesKey[]; name: string }) {
  const series = SERIES.filter((s) => keys.includes(s.key))
  const sum = (d: UsageDay) => series.reduce((n, s) => n + d[s.key], 0)
  const max = Math.max(1, ...daily.map(sum))
  const every = daily.length > 14 ? 5 : 1
  return (
    <div role="img" aria-label={`${name}: tokens per day over the last ${daily.length} days`}>
      <div className="flex h-36 items-end gap-[3px]">
        {daily.map((d) => {
          const total = sum(d)
          return (
            <div
              key={d.date}
              className="group relative flex h-full min-w-0 flex-1 flex-col justify-end"
              title={`${dayLabel(d.date)}: ${fmtTokens(total)} tokens\n${series.map((s) => `${s.label} ${fmtTokens(d[s.key])}`).join(" · ")}`}
            >
              <div className="flex flex-col-reverse overflow-hidden rounded-t-[3px]" style={{ height: `${(total / max) * 100}%`, minHeight: total > 0 ? 2 : 0 }}>
                {series.map((s) => (
                  <div key={s.key} style={{ height: total ? `${(d[s.key] / total) * 100}%` : 0, backgroundColor: s.color }} />
                ))}
              </div>
              <div aria-hidden className="absolute inset-x-0 bottom-0 top-0 rounded-sm bg-foreground/0 transition-colors group-hover:bg-foreground/5" />
            </div>
          )
        })}
      </div>
      <div className="mt-1.5 flex gap-[3px] text-2xs text-subtle-foreground">
        {daily.map((d, i) => (
          <span key={d.date} className="min-w-0 flex-1 overflow-visible whitespace-nowrap text-center">
            {i % every === 0 || i === daily.length - 1 ? dayLabel(d.date) : ""}
          </span>
        ))}
      </div>
    </div>
  )
}

function Legend({ keys }: { keys: readonly SeriesKey[] }) {
  return (
    <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground">
      {SERIES.filter((s) => keys.includes(s.key)).map((s) => (
        <span key={s.key} className="flex items-center gap-1.5">
          <span className="size-2 rounded-sm" style={{ backgroundColor: s.color }} />
          {s.label}
        </span>
      ))}
    </div>
  )
}

function Stat({ label, value, sub }: { label: string; value: string; sub?: string }) {
  return (
    <div>
      <div className="text-2xs font-medium uppercase tracking-[0.08em] text-subtle-foreground">{label}</div>
      <div className="mt-0.5 text-lg font-semibold tabular-nums tracking-tight">{value}</div>
      {sub && <div className="text-xs text-subtle-foreground">{sub}</div>}
    </div>
  )
}

function BucketList({ title, items }: { title: string; items: UsageBucket[] }) {
  const max = Math.max(1, ...items.map((b) => b.total))
  return (
    <div className="min-w-0">
      <h3 className="mb-2 text-xs font-medium text-muted-foreground">{title}</h3>
      {items.length === 0 ? (
        <p className="text-xs text-subtle-foreground">Nothing in range.</p>
      ) : (
        <ul className="space-y-1.5">
          {items.slice(0, 6).map((b) => (
            <li key={b.name} className="flex items-center gap-2" title={`${b.name}: ${fmtTokens(b.total)} tokens`}>
              <span className="w-32 shrink-0 truncate font-mono text-xs @3xl:w-40">{b.name}</span>
              <span className="h-1.5 min-w-0 flex-1 overflow-hidden rounded-full bg-muted">
                <span className="block h-full rounded-full bg-brand/70" style={{ width: `${(b.total / max) * 100}%` }} />
              </span>
              <span className="w-12 shrink-0 text-right text-xs tabular-nums text-muted-foreground">{fmtTokens(b.total)}</span>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}

function ProviderCard({ p, withCache }: { p: UsageProvider; withCache: boolean }) {
  const keys: SeriesKey[] = withCache ? ["input", "output", "cache_write", "cache_read"] : ["input", "output", "cache_write"]
  const shown = p.totals.total - (withCache ? 0 : p.totals.cache_read)
  const none = p.status === "missing" || p.status === "disabled"
  return (
    <Card className="overflow-hidden">
      <div className="flex flex-wrap items-center justify-between gap-3 px-5 pb-3 pt-4">
        <div className="flex items-center gap-2.5">
          <ProviderTile provider={p.id} size="sm" />
          <div>
            <h2 className="text-sm font-semibold">{p.label}</h2>
            <p className="text-xs text-subtle-foreground">
              {p.sources} {p.sources === 1 ? "log file" : "log files"} read on this machine
            </p>
          </div>
        </div>
        {!none && <Legend keys={keys} />}
      </div>
      {none ? (
        <p className="border-t px-5 py-6 text-sm text-muted-foreground">{p.note ?? "Disabled in your config."}</p>
      ) : (
        <div className="space-y-5 border-t px-5 py-4">
          <div className="flex flex-wrap gap-x-8 gap-y-3">
            <Stat label="Tokens" value={fmtTokens(shown)} sub={withCache ? "including cache reads" : "without cache reads"} />
            <Stat label="Input" value={fmtTokens(p.totals.input)} />
            <Stat label="Output" value={fmtTokens(p.totals.output)} />
            <Stat label="Cache write" value={fmtTokens(p.totals.cache_write)} />
            <Stat label="Cache read" value={fmtTokens(p.totals.cache_read)} />
            {p.totals.cost_usd ? <Stat label="Reported cost" value={`$${p.totals.cost_usd.toFixed(2)}`} /> : null}
          </div>
          <DailyBars daily={p.daily} keys={keys} name={p.label} />
          <div className="grid gap-6 @3xl:grid-cols-2">
            <BucketList title="Top projects" items={p.projects} />
            <BucketList title="Models" items={p.models} />
          </div>
        </div>
      )}
    </Card>
  )
}

function OwnCard({ o, withCache }: { o: UsageOwn; withCache: boolean }) {
  const keys: SeriesKey[] = withCache ? ["input", "output", "cache_write", "cache_read"] : ["input", "output", "cache_write"]
  const cost = o.totals.cost_usd ?? 0
  return (
    <Card className="overflow-hidden">
      <div className="flex flex-wrap items-center justify-between gap-3 px-5 pb-3 pt-4">
        <div className="flex items-center gap-2.5">
          <span className="flex size-7 items-center justify-center rounded-lg border bg-brand-soft text-brand">
            <Gauge className="size-3.5" />
          </span>
          <div>
            <h2 className="text-sm font-semibold">Lucidbench runs</h2>
            <p className="text-xs text-subtle-foreground">What council and work sessions started here spent.</p>
          </div>
        </div>
        {o.runs > 0 && <Legend keys={keys} />}
      </div>
      {o.runs === 0 ? (
        <p className="border-t px-5 py-6 text-sm text-muted-foreground">No council or work runs in this range yet.</p>
      ) : (
        <div className="space-y-5 border-t px-5 py-4">
          <div className="flex flex-wrap gap-x-8 gap-y-3">
            <Stat label="Runs" value={String(o.runs)} />
            <Stat label="Tokens" value={fmtTokens(o.totals.total - (withCache ? 0 : o.totals.cache_read))} />
            <Stat label="Input" value={fmtTokens(o.totals.input)} />
            <Stat label="Output" value={fmtTokens(o.totals.output)} />
            {cost > 0 ? <Stat label="Reported cost" value={`$${cost.toFixed(2)}`} sub="as the CLIs state it" /> : null}
          </div>
          <DailyBars daily={o.daily} keys={keys} name="Lucidbench runs" />
          <div className="grid gap-6 @3xl:grid-cols-2">
            <BucketList title="By source" items={o.sources} />
            <BucketList title="By provider" items={o.providers} />
          </div>
        </div>
      )}
    </Card>
  )
}

/* ---------- page ---------- */

export default function Usage() {
  const { label } = usePrefs()
  const [days, setDays] = useState(7)
  const [withCache, setWithCache] = useState(false)
  const poll = usePoll<UsageSummary>(usageURL(days), USAGE_POLL_MS)
  const now = useNow(30000)
  const s = poll.data
  const title = label("usage_meter", "Usage")
  const gauges = (s?.providers ?? []).filter((p) => p.id === "claude" || p.id === "codex" || p.windows.length > 0 || p.status === "unavailable")
  const detailed = (s?.providers ?? []).filter((p) => p.status !== "unavailable")

  return (
    <div className="space-y-6">
      <PageHeader
        icon={<Gauge />}
        title={title}
        description="Tokens and limit windows for each subscription, read from the logs your CLIs already keep on this machine."
        actions={
          <>
            <div role="group" aria-label="Range" className="flex items-center gap-1">
              {[7, 30].map((n) => (
                <Button key={n} size="sm" variant={days === n ? "secondary" : "ghost"} aria-pressed={days === n} onClick={() => setDays(n)}>
                  {n} days
                </Button>
              ))}
            </div>
            <RefreshButton refreshing={poll.refreshing} updatedAt={poll.updatedAt} />
          </>
        }
      />

      <div className="flex items-start gap-3 rounded-lg border bg-muted/30 px-4 py-3">
        <ShieldCheck className="mt-0.5 size-4 shrink-0 text-success" />
        <p className="text-sm text-muted-foreground">
          <span className="font-medium text-foreground">Numbers and folder names only.</span> Lucidbench adds up token counts from your Claude Code
          and Codex logs and from its own runs. Prompts and replies are never read out or stored, and cost is shown only where a CLI states it.
        </p>
      </div>

      {poll.loading && !s && (
        <div className="space-y-4" aria-busy="true" aria-label="Reading usage logs">
          <Skeleton className="h-48 rounded-xl" />
          <Skeleton className="h-72 rounded-xl" />
          <p className="text-center text-xs text-subtle-foreground">The first read of a large log folder can take a few seconds.</p>
        </div>
      )}
      {poll.error && !s && <ErrorState title="Could not read usage" message={poll.error.message} onRetry={poll.refresh} />}

      {s && (
        <>
          <WindowsCard providers={gauges} title={`${title} windows`} now={now} />

          <div className="flex flex-wrap items-center justify-between gap-3">
            <h2 className="text-sm font-semibold">Tokens, last {s.days} days</h2>
            <label className="flex cursor-pointer items-center gap-2 text-xs text-muted-foreground">
              <input type="checkbox" className="size-3.5 accent-[var(--brand)]" checked={withCache} onChange={(e) => setWithCache(e.target.checked)} />
              Include cache reads
            </label>
          </div>
          {detailed.every((p) => p.status === "missing" || p.status === "disabled") && s.lucidbench.runs === 0 && (
            <Card className="border-dashed">
              <EmptyState
                icon={<Gauge />}
                title="No usage found yet"
                description="Use Claude Code or Codex on this machine and their token counts appear here."
              />
            </Card>
          )}
          <div className="space-y-4">
            {detailed.map((p) => (
              <ProviderCard key={p.id} p={p} withCache={withCache} />
            ))}
            <OwnCard o={s.lucidbench} withCache={withCache} />
          </div>
        </>
      )}
    </div>
  )
}
