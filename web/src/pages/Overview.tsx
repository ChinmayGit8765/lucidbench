import { Component, type ReactNode } from "react"
import { CircleCheck, PenLine, SquareTerminal } from "lucide-react"

import { SectionGrid } from "@/components/SectionGrid"
import { PageHeader } from "@/components/Shell"
import { SpriteBoard } from "@/components/ThemeArt"
import { Button } from "@/components/ui/button"
import { Card } from "@/components/ui/card"
import { Skeleton } from "@/components/ui/states"
import { useApp } from "@/lib/app"
import { newBraindump } from "@/lib/council"
import { usePrefs } from "@/lib/prefs"
import { useNow } from "@/lib/time"
import { cn, isMac } from "@/lib/utils"
import { MODULES } from "@/modules"
import { isLive, useAttention } from "@/modules/registry"
import type { AttentionItem, ModulePageProps } from "@/modules/types"
import { NextUpSection } from "@/pages/NextUp"

/** Home shows at most this many tiles: the rest is one click away in the sidebar. */
const MAX_TILES = 4

function greeting(d: Date): string {
  const h = d.getHours()
  if (h < 5) return "Working late"
  if (h < 12) return "Good morning"
  if (h < 18) return "Good afternoon"
  return "Good evening"
}

const SEVERITY_BAR: Record<AttentionItem["severity"], string> = {
  danger: "bg-danger",
  warning: "bg-warning",
  info: "bg-info",
}

function AttentionRow({ a, i = 0 }: { a: AttentionItem; i?: number }) {
  const infra = a.kind === "infra"
  return (
    <li
      className="lb-rise relative flex items-center gap-3 px-5 py-2.5 transition-colors hover:bg-accent/30"
      style={{ "--i": i } as React.CSSProperties}
      data-testid="attention-row"
      data-kind={a.kind ?? "move"}
    >
      <span aria-hidden className={cn("absolute inset-y-2 left-0 w-0.5 rounded-full", SEVERITY_BAR[a.severity], infra && "opacity-50")} />
      <span className="flex size-7 shrink-0 items-center justify-center rounded-md border bg-background/60">{a.icon}</span>
      <div className="min-w-0 flex-1">
        <div className={cn("truncate text-sm", infra ? "text-muted-foreground" : "font-medium")}>{a.title}</div>
        <div className="truncate text-xs text-muted-foreground">{a.meta}</div>
      </div>
      {a.action && <div className="flex shrink-0 items-center gap-1">{a.action}</div>}
    </li>
  )
}

/** "Needs your move": the user's own moves first, then what the machine needs, under its own label. */
function NeedsYourMove({ items, loading, pending }: { items: AttentionItem[]; loading: boolean; pending: boolean }) {
  const { open } = useApp()
  const { label } = usePrefs()
  const moves = items.filter((a) => a.kind !== "infra")
  const infra = items.filter((a) => a.kind === "infra")
  const top = moves[0] ?? infra[0]
  return (
    <Card className="overflow-hidden" data-testid="needs-your-move">
      <div className="flex items-center justify-between px-5 pb-3 pt-4">
        <h2 className="flex items-center gap-2 text-sm font-semibold">
          {label("attention", "Needs your move")}
          {moves.length > 0 && top && (
            <span
              className={cn(
                "rounded-full px-1.5 text-2xs tabular-nums",
                top.severity === "danger" ? "bg-danger-soft text-danger-fg" : top.severity === "warning" ? "bg-warning-soft text-warning-fg" : "bg-info-soft text-info-fg",
              )}
            >
              {moves.length}
            </span>
          )}
        </h2>
      </div>
      {loading || (items.length === 0 && pending) ? (
        <div className="space-y-2 border-t p-5">
          <Skeleton className="h-9" />
          <Skeleton className="h-9" />
        </div>
      ) : items.length === 0 ? (
        <div className="flex items-center gap-3 border-t px-5 py-4">
          <span className="flex size-8 items-center justify-center rounded-full bg-success-soft text-success">
            <CircleCheck className="size-4" />
          </span>
          <div className="min-w-0 flex-1">
            <div className="text-sm font-medium">{label("all_clear", "All clear")}</div>
            <div className="text-xs text-muted-foreground">No briefs to approve, diffs to review, agents waiting, usage limits or blocked needs.</div>
          </div>
          <Button size="sm" variant="secondary" onClick={() => newBraindump(open)} title="New braindump (n)">
            <PenLine /> Braindump
          </Button>
        </div>
      ) : (
        <ul className="divide-y border-t">
          {moves.map((a, i) => (
            <AttentionRow key={a.key} a={a} i={i} />
          ))}
          {infra.length > 0 && (
            <li className="bg-muted/30 px-5 pb-1 pt-2 text-2xs font-medium uppercase tracking-[0.08em] text-subtle-foreground" aria-hidden>
              This machine
            </li>
          )}
          {infra.map((a, i) => (
            <AttentionRow key={a.key} a={a} i={moves.length + i} />
          ))}
        </ul>
      )}
    </Card>
  )
}

/**
 * Home: what needs your move, then Next up, then a few tiles. It replaces
 * the old Overview and Next up pages; /nextup and /nextup/start/<id> land
 * here.
 */
export default function Overview({ subpath }: ModulePageProps) {
  const { open, dockerUp } = useApp()
  const { prefs, label } = usePrefs()
  const now = useNow(60000)
  const { items, loading, pending } = useAttention(prefs, dockerUp)
  const tiles = MODULES.filter((m) => m.overviewTile && m.tileRank !== undefined && isLive(m, prefs, dockerUp))
    .sort((a, b) => (a.tileRank ?? 0) - (b.tileRank ?? 0))
    .slice(0, MAX_TILES)
  const moves = items.filter((a) => a.kind !== "infra").length
  const startId = subpath[0] === "start" ? subpath.slice(1).join("/") : ""
  const today = new Date(now)

  return (
    <div className="space-y-8">
      <PageHeader
        eyebrow={today.toLocaleDateString(undefined, { weekday: "long", day: "numeric", month: "long" })}
        title={label("overview_title", greeting(today))}
        description={
          loading || (moves === 0 && pending)
            ? "Checking your workspace…"
            : moves === 0
              ? "Nothing needs your move right now."
              : `${moves} ${moves === 1 ? "thing needs" : "things need"} your move.`
        }
        actions={
          <>
            <Button size="sm" variant="secondary" onClick={() => open("work", ["new"])}>
              <SquareTerminal /> Start work
            </Button>
            <Button size="sm" onClick={() => newBraindump(open)} title={`New braindump (n)`}>
              <PenLine /> New braindump
            </Button>
          </>
        }
      />

      <NeedsYourMove items={items} loading={loading} pending={pending} />

      <NextUpSection startId={startId} />

      {tiles.length > 0 && (
        <section aria-label="At a glance" className="grid grid-cols-2 gap-3 @4xl:grid-cols-4">
          {tiles.map((m, i) => {
            const Tile = m.overviewTile!
            // Each tile loads, and fails, on its own.
            return (
              <div key={m.id} className="lb-rise [&>*]:h-full" style={{ "--i": i } as React.CSSProperties}>
                <TileBoundary title={m.title}>
                  <Tile />
                </TileBoundary>
              </div>
            )
          })}
        </section>
      )}

      <SectionGrid placement="overview" />

      <SpriteBoard />

      <p className="flex items-center gap-1.5 text-xs text-subtle-foreground">
        Everything else is one search away:
        <kbd className="rounded border bg-muted px-1 font-sans text-2xs">{isMac() ? "⌘" : "Ctrl"}</kbd>
        <kbd className="rounded border bg-muted px-1 font-sans text-2xs">K</kbd>
      </p>
    </div>
  )
}

/** Keeps one broken tile from taking Home down with it. */
class TileBoundary extends Component<{ title: string; children: ReactNode }, { failed: boolean }> {
  state = { failed: false }
  static getDerivedStateFromError() {
    return { failed: true }
  }
  render() {
    if (!this.state.failed) return this.props.children
    return (
      <Card className="flex h-full items-center p-4 text-xs text-muted-foreground">
        {this.props.title} could not be shown. Reload the page to try again.
      </Card>
    )
  }
}
