import { useState } from "react"
import { Check, Copy, Lightbulb, ListChecks, Quote, Sparkles, StickyNote, Vote } from "lucide-react"
import { toast } from "sonner"

import { useApplyProposals, type Outcome } from "@/components/assistant/Proposals"
import { StateSprite } from "@/components/StateSprite"
import { Badge, StatusPill } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { errorMessage } from "@/lib/api"
import {
  ASSISTANT_PROVIDERS,
  defaultAs,
  itemAction,
  parseBraindump,
  type BraindumpItem,
  type BraindumpResult,
  type Proposal,
} from "@/lib/assistant"
import { cn } from "@/lib/utils"
import { formatCost } from "@/lib/work"

type As = "card" | "idea" | "council"

const AS: { id: As; label: string; icon: typeof StickyNote }[] = [
  { id: "card", label: "Card", icon: StickyNote },
  { id: "idea", label: "Idea", icon: Lightbulb },
  { id: "council", label: "Council", icon: Vote },
]

const NEXT_TONE = { council: "warning", card: "info", idea: "neutral", park: "neutral" } as const

/** A proposal for an item, so it goes through the same check and Apply as the chat's. */
function proposalFor(it: BraindumpItem, as: As): Proposal {
  const a = itemAction(it, as)
  return { action: a.action, args: a.args, summary: it.restatement, valid: true, problems: [], status: "pending", spends: as === "council" }
}

/**
 * Parse a braindump: paste a messy dump, the user's CLI splits it into
 * items (no tools), and each one can become a card, an idea or a council
 * braindump. Nothing is applied until the user clicks.
 */
export function BraindumpParse({ className }: { className?: string }) {
  const [text, setText] = useState("")
  const [provider, setProvider] = useState("")
  const [busy, setBusy] = useState(false)
  const [res, setRes] = useState<BraindumpResult | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [picked, setPicked] = useState<Record<number, boolean>>({})
  const [as, setAs] = useState<Record<number, As>>({})
  const [done, setDone] = useState<Record<number, Outcome>>({})
  const { apply, applyMany, dialogs } = useApplyProposals()

  const parse = async () => {
    setBusy(true)
    setError(null)
    try {
      const r = await parseBraindump(text, provider || undefined)
      setRes(r)
      setPicked({})
      setDone({})
      setAs(Object.fromEntries(r.items.map((it, i) => [i, defaultAs(it.next)])))
    } catch (e) {
      setError(errorMessage(e))
    } finally {
      setBusy(false)
    }
  }

  const items = res?.items ?? []
  const selected = items.map((_, i) => i).filter((i) => picked[i] && !done[i])
  const mark = (i: number) => (o: Outcome) => {
    setDone((d) => ({ ...d, [i]: o }))
    if (o.status === "applied") toast.success(`Applied: ${items[i].restatement}`)
  }

  return (
    <div className={cn("space-y-4", className)} data-testid="braindump-parse">
      <div className="space-y-2">
        <label htmlFor="braindump-text" className="text-sm font-medium">
          Paste a braindump
        </label>
        <textarea
          id="braindump-text"
          value={text}
          onChange={(e) => setText(e.target.value)}
          rows={7}
          placeholder="Everything on your mind, as messy as it is: half-ideas, bugs, chores, questions…"
          className="w-full resize-y rounded-md border bg-background/50 p-3 text-sm outline-none focus:border-ring"
        />
        <div className="flex flex-wrap items-center gap-2">
          <select
            aria-label="Provider"
            value={provider}
            onChange={(e) => setProvider(e.target.value)}
            className="h-8 rounded-md border bg-background/50 px-2 text-sm"
          >
            <option value="">First installed CLI</option>
            {ASSISTANT_PROVIDERS.map((p) => (
              <option key={p} value={p}>
                {p}
              </option>
            ))}
          </select>
          <Button onClick={() => void parse()} disabled={busy || text.trim().length < 3} data-testid="braindump-parse-run">
            <Sparkles /> {busy ? "Splitting…" : "Split into items"}
          </Button>
          <span className="text-xs text-subtle-foreground">One call with no tools, on your own account. Nothing is applied until you click.</span>
        </div>
      </div>

      {busy && (
        <div className="flex items-center gap-3 rounded-lg border bg-elevated/60 p-3 text-sm text-muted-foreground">
          <StateSprite state="thinking" className="size-10" /> Reading the braindump…
        </div>
      )}
      {error && <p role="alert" className="rounded-md border border-danger/30 bg-danger-soft p-3 text-sm">{error}</p>}

      {res && (
        <div className="space-y-2">
          <div className="flex flex-wrap items-center gap-2">
            <h3 className="text-sm font-semibold">{items.length} items</h3>
            <span className="text-xs text-subtle-foreground">
              {res.provider}
              {res.model ? ` · ${res.model}` : ""} · {formatCost(res.usage.cost_usd)}
            </span>
            <div className="ml-auto flex items-center gap-2">
              <Button
                size="sm"
                variant="secondary"
                onClick={() => setPicked(Object.fromEntries(items.map((_, i) => [i, !done[i]])))}
              >
                <ListChecks /> Select all
              </Button>
              <Button
                size="sm"
                disabled={selected.length === 0}
                data-testid="braindump-apply-selected"
                onClick={() =>
                  void applyMany(
                    selected.map((i) => proposalFor(items[i], as[i] ?? defaultAs(items[i].next))),
                    (k, o) => mark(selected[k])(o),
                  )
                }
              >
                <Check /> Apply {selected.length || ""} selected
              </Button>
            </div>
          </div>
          {res.notes.map((n, i) => (
            <p key={i} className="text-xs text-warning-fg">
              {n}
            </p>
          ))}
          <ul className="space-y-2">
            {items.map((it, i) => {
              const o = done[i]
              const mode = as[i] ?? defaultAs(it.next)
              return (
                <li key={i} data-testid="braindump-item" data-status={o?.status ?? "pending"} className={cn("rounded-lg border bg-elevated/60 p-3", o?.status === "applied" && "opacity-70")}>
                  <div className="flex items-start gap-3">
                    <input
                      type="checkbox"
                      aria-label={`Select ${it.restatement}`}
                      checked={!!picked[i]}
                      disabled={!!o}
                      onChange={(e) => setPicked((p) => ({ ...p, [i]: e.target.checked }))}
                      className="mt-1"
                    />
                    <div className="min-w-0 flex-1 space-y-1">
                      <div className="text-sm font-medium">{it.restatement}</div>
                      {it.quote && (
                        <p className="flex items-start gap-1 text-xs italic text-muted-foreground">
                          <Quote className="mt-0.5 size-3 shrink-0" />
                          <span>
                            {it.quote}
                            {!it.quote_found && <span className="not-italic text-warning-fg"> (not found word for word in the dump)</span>}
                          </span>
                        </p>
                      )}
                      <div className="flex flex-wrap items-center gap-1.5">
                        <Badge>{it.type}</Badge>
                        {it.project ? <Badge>{it.project}</Badge> : it.new_project ? <StatusPill tone="info">new project?</StatusPill> : null}
                        <StatusPill tone={NEXT_TONE[it.next]}>next: {it.next}</StatusPill>
                        {it.similar && (
                          <StatusPill tone="warning">
                            <Copy className="size-3" /> looks like an existing {it.similar.kind}: {it.similar.title}
                          </StatusPill>
                        )}
                        {o && <StatusPill tone={o.status === "applied" ? "success" : "danger"}>{o.status}{o.note ? ` · ${o.note}` : ""}</StatusPill>}
                      </div>
                    </div>
                    {!o && (
                      <div className="flex shrink-0 flex-col items-end gap-1.5">
                        <div role="radiogroup" aria-label="Apply as" className="flex rounded-md border p-0.5">
                          {AS.map((x) => (
                            <button
                              key={x.id}
                              role="radio"
                              aria-checked={mode === x.id}
                              onClick={() => setAs((m) => ({ ...m, [i]: x.id }))}
                              className={cn("flex h-6 items-center gap-1 rounded px-1.5 text-2xs", mode === x.id ? "bg-brand-soft text-brand-fg" : "text-muted-foreground hover:text-foreground")}
                            >
                              <x.icon className="size-3" /> {x.label}
                            </button>
                          ))}
                        </div>
                        <Button size="sm" variant="secondary" onClick={() => void apply(proposalFor(it, mode), mark(i))} data-testid="braindump-item-apply">
                          Apply as {mode}
                        </Button>
                      </div>
                    )}
                  </div>
                </li>
              )
            })}
          </ul>
        </div>
      )}
      {dialogs}
    </div>
  )
}
